package services

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/deleyva/recall/internal/models"
)

type ReviewService struct {
	db *sql.DB
}

func NewReviewService(db *sql.DB) *ReviewService {
	return &ReviewService{db: db}
}

// LearnAheadWindow is how far ahead the queue will reach for a card that is
// mid-loop. A card failed two minutes ago is due in five; without this the
// session would end and the re-retrieval that makes the failure worth anything
// would never happen. Anki calls the same idea the learn-ahead limit.
const LearnAheadWindow = 20 * time.Minute

// queuePredicate selects what the session may serve: anything already due, plus
// learning and relearning cards close enough to be worth finishing now. Review
// cards are never pulled forward — studying ahead of the schedule is exactly
// what the spacing is there to prevent.
const queuePredicate = `
	deck_id = ?
	AND (
		due <= ?
		OR (state IN (1, 3) AND due <= ?)
	)`

// queueOrder ranks by three terms. A card that is actually due always beats one
// merely reached for by learn-ahead, so studying ahead stays a last resort
// rather than a shortcut past the schedule. Within each of those groups, cards
// mid-loop come first — they are time-sensitive, and a failed card left behind
// the new-card backlog never resurfaces before the session ends — then new
// cards, then reviews, each by due date.
const queueOrder = `
	ORDER BY
		CASE WHEN due <= ? THEN 0 ELSE 1 END ASC,
		CASE WHEN state IN (1, 3) THEN 0 WHEN state = 0 THEN 1 ELSE 2 END ASC,
		due ASC`

func (s *ReviewService) GetNextDue(deckID string) (*models.Card, int, error) {
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339)
	ahead := nowTime.Add(LearnAheadWindow).Format(time.RFC3339)

	var dueCount int
	s.db.QueryRow("SELECT COUNT(*) FROM cards WHERE"+queuePredicate, deckID, now, ahead).Scan(&dueCount)

	if dueCount == 0 {
		return nil, 0, nil
	}

	row := s.db.QueryRow(`
		SELECT id, deck_id, front, back, due, stability, difficulty, elapsed_days, scheduled_days,
			reps, lapses, state, last_review, created_at, updated_at, article_id, kind
		FROM cards
		WHERE`+queuePredicate+queueOrder+`
		LIMIT 1
	`, deckID, now, ahead, now)

	card, err := scanCardRow(row)
	if err != nil {
		return nil, 0, fmt.Errorf("get next due: %w", err)
	}
	return card, dueCount, nil
}

func (s *ReviewService) CreateLog(cardID string, rating, scheduledDays, elapsedDays, state int) error {
	id := generateID()
	now := time.Now().UTC()

	_, err := s.db.Exec(`
		INSERT INTO review_logs (id, card_id, rating, scheduled_days, elapsed_days, review_time, state)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, cardID, rating, scheduledDays, elapsedDays, now.Format(time.RFC3339), state)
	return err
}

func (s *ReviewService) GetStats(userID string) (*models.Stats, error) {
	var stats models.Stats
	now := time.Now().UTC().Format(time.RFC3339)

	// Total cards
	s.db.QueryRow(`
		SELECT COUNT(*) FROM cards c
		JOIN decks d ON c.deck_id = d.id
		WHERE d.user_id = ?
	`, userID).Scan(&stats.TotalCards)

	// Due today
	s.db.QueryRow(`
		SELECT COUNT(*) FROM cards c
		JOIN decks d ON c.deck_id = d.id
		WHERE d.user_id = ? AND c.due <= ?
	`, userID, now).Scan(&stats.DueToday)

	// Streak (consecutive days with reviews)
	stats.Streak = s.calculateStreak(userID)

	return &stats, nil
}

func (s *ReviewService) calculateStreak(userID string) int {
	rows, err := s.db.Query(`
		SELECT DISTINCT date(r.review_time) as review_date
		FROM review_logs r
		JOIN cards c ON r.card_id = c.id
		JOIN decks d ON c.deck_id = d.id
		WHERE d.user_id = ?
		ORDER BY review_date DESC
	`, userID)
	if err != nil {
		return 0
	}
	defer rows.Close()

	streak := 0
	expected := time.Now().UTC().Truncate(24 * time.Hour)

	for rows.Next() {
		var dateStr string
		if err := rows.Scan(&dateStr); err != nil {
			break
		}
		reviewDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			break
		}

		if reviewDate.Equal(expected) {
			streak++
			expected = expected.AddDate(0, 0, -1)
		} else if reviewDate.Before(expected) {
			break
		}
	}
	return streak
}

func (s *ReviewService) GetHistory(userID string) ([]models.DailyReviewCount, error) {
	rows, err := s.db.Query(`
		SELECT date(r.review_time) as review_date, COUNT(*) as count
		FROM review_logs r
		JOIN cards c ON r.card_id = c.id
		JOIN decks d ON c.deck_id = d.id
		WHERE d.user_id = ?
		GROUP BY review_date
		ORDER BY review_date DESC
		LIMIT 30
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}
	defer rows.Close()

	var history []models.DailyReviewCount
	for rows.Next() {
		var h models.DailyReviewCount
		if err := rows.Scan(&h.Date, &h.Reviews); err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}
		history = append(history, h)
	}
	return history, nil
}
