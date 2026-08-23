package services

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/deleyva/recall/internal/models"
)

type CardService struct {
	db *sql.DB
}

// Card kinds. A recognition card reveals its answer when the learner asks for
// it; a production card requires the learner to type the answer first, so the
// system observes whether it was produced instead of trusting a self-report.
const (
	KindRecognition = "recognition"
	KindProduction  = "production"
)

// ValidCardKind guards the column SQLite cannot guard for us — ALTER TABLE
// cannot add a CHECK constraint, so the check lives here.
func ValidCardKind(kind string) bool {
	return kind == KindRecognition || kind == KindProduction
}

func NewCardService(db *sql.DB) *CardService {
	return &CardService{db: db}
}

func (s *CardService) Create(deckID, front, back string, articleID *string) (*models.Card, error) {
	id := generateID()
	now := time.Now().UTC()

	_, err := s.db.Exec(`
		INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days, scheduled_days, reps, lapses, state, last_review, created_at, updated_at, article_id)
		VALUES (?, ?, ?, ?, ?, 0, 0, 0, 0, 0, 0, 0, '0001-01-01T00:00:00Z', ?, ?, ?)
	`, id, deckID, front, back, now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339), articleID)
	if err != nil {
		return nil, fmt.Errorf("create card: %w", err)
	}

	return &models.Card{
		ID:        id,
		DeckID:    deckID,
		ArticleID: articleID,
		Front:     front,
		Back:      back,
		Due:       now,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

type FlashcardPair struct {
	Front string `json:"front"`
	Back  string `json:"back"`
}

func (s *CardService) CreateBatch(deckID string, articleID *string, pairs []FlashcardPair) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	count := 0
	for _, p := range pairs {
		if p.Front == "" || p.Back == "" {
			continue
		}
		id := generateID()
		now := time.Now().UTC()
		_, err = tx.Exec(`
			INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days, scheduled_days, reps, lapses, state, last_review, created_at, updated_at, article_id)
			VALUES (?, ?, ?, ?, ?, 0, 0, 0, 0, 0, 0, 0, '0001-01-01T00:00:00Z', ?, ?, ?)
		`, id, deckID, p.Front, p.Back, now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339), articleID)
		if err != nil {
			return count, fmt.Errorf("insert card: %w", err)
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}
	return count, nil
}

func (s *CardService) List(deckID string, page, perPage int) ([]models.Card, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}
	offset := (page - 1) * perPage

	var total int
	s.db.QueryRow("SELECT COUNT(*) FROM cards WHERE deck_id = ?", deckID).Scan(&total)

	rows, err := s.db.Query(`
		SELECT id, deck_id, front, back, due, stability, difficulty, elapsed_days, scheduled_days,
			reps, lapses, state, last_review, created_at, updated_at, article_id, kind, buried_until, suspended
		FROM cards WHERE deck_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, deckID, perPage, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list cards: %w", err)
	}
	defer rows.Close()

	var cards []models.Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, 0, err
		}
		cards = append(cards, *c)
	}
	return cards, total, nil
}

// ListForUser returns every card the user owns across all decks, optionally
// filtered by deck or by the article that generated it, and optionally only
// those currently due.
func (s *CardService) ListForUser(userID, deckID, articleID string, dueOnly bool, limit, offset int) ([]models.Card, int, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	where := "d.user_id = ?"
	args := []interface{}{userID}
	if deckID != "" {
		where += " AND c.deck_id = ?"
		args = append(args, deckID)
	}
	if articleID != "" {
		where += " AND c.article_id = ?"
		args = append(args, articleID)
	}
	if dueOnly {
		now := time.Now().UTC().Format(time.RFC3339)
		where += " AND c.due <= ? AND c.suspended = 0 AND (c.buried_until IS NULL OR c.buried_until <= ?)"
		args = append(args, now, now)
	}

	var total int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM cards c JOIN decks d ON c.deck_id = d.id WHERE "+where, args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count cards: %w", err)
	}

	rows, err := s.db.Query(`
		SELECT c.id, c.deck_id, c.front, c.back, c.due, c.stability, c.difficulty, c.elapsed_days,
			c.scheduled_days, c.reps, c.lapses, c.state, c.last_review, c.created_at, c.updated_at, c.article_id, c.kind, c.buried_until, c.suspended
		FROM cards c JOIN decks d ON c.deck_id = d.id
		WHERE `+where+`
		ORDER BY c.created_at DESC
		LIMIT ? OFFSET ?
	`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list user cards: %w", err)
	}
	defer rows.Close()

	cards := []models.Card{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, 0, err
		}
		cards = append(cards, *c)
	}
	return cards, total, rows.Err()
}

func (s *CardService) Get(cardID string) (*models.Card, error) {
	row := s.db.QueryRow(`
		SELECT id, deck_id, front, back, due, stability, difficulty, elapsed_days, scheduled_days,
			reps, lapses, state, last_review, created_at, updated_at, article_id, kind, buried_until, suspended
		FROM cards WHERE id = ?
	`, cardID)
	return scanCardRow(row)
}

// GetForUser returns a card only if it belongs to a deck owned by userID.
func (s *CardService) GetForUser(cardID, userID string) (*models.Card, error) {
	row := s.db.QueryRow(`
		SELECT c.id, c.deck_id, c.front, c.back, c.due, c.stability, c.difficulty, c.elapsed_days, c.scheduled_days,
			c.reps, c.lapses, c.state, c.last_review, c.created_at, c.updated_at, c.article_id, c.kind, c.buried_until, c.suspended
		FROM cards c
		JOIN decks d ON c.deck_id = d.id
		WHERE c.id = ? AND d.user_id = ?
	`, cardID, userID)
	return scanCardRow(row)
}

func (s *CardService) Update(cardID, front, back string) error {
	now := time.Now().UTC()
	result, err := s.db.Exec(
		"UPDATE cards SET front = ?, back = ?, updated_at = ? WHERE id = ?",
		front, back, now.Format(time.RFC3339), cardID,
	)
	if err != nil {
		return fmt.Errorf("update card: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("card not found")
	}
	return nil
}

// UpdateForUser updates a card only if it belongs to a deck owned by userID.
func (s *CardService) UpdateForUser(cardID, userID, front, back string) error {
	now := time.Now().UTC()
	result, err := s.db.Exec(`
		UPDATE cards SET front = ?, back = ?, updated_at = ?
		WHERE id = ? AND deck_id IN (SELECT id FROM decks WHERE user_id = ?)`,
		front, back, now.Format(time.RFC3339), cardID, userID,
	)
	if err != nil {
		return fmt.Errorf("update card: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("card not found")
	}
	return nil
}

// SetKindForUser switches a card between recognition and production. It touches
// no FSRS column: a card that changes how it is asked keeps the schedule it has
// earned.
func (s *CardService) SetKindForUser(cardID, userID, kind string) error {
	if !ValidCardKind(kind) {
		return fmt.Errorf("invalid card kind: %s", kind)
	}
	result, err := s.db.Exec(`
		UPDATE cards SET kind = ?, updated_at = ?
		WHERE id = ? AND deck_id IN (SELECT id FROM decks WHERE user_id = ?)
	`, kind, time.Now().UTC().Format(time.RFC3339), cardID, userID)
	if err != nil {
		return fmt.Errorf("set card kind: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("set card kind: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("card not found")
	}
	return nil
}

// nextDayBoundary is the next local midnight after now. Burying is scoped to
// "not again today", and the day the learner means is the one on their own
// clock — the same definition `recall metrics` uses for its day boundaries.
// time.Date normalizes the day overflow and resolves the offset in loc, so a
// DST transition moves the boundary rather than breaking it.
func nextDayBoundary(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, loc)
}

// BurySiblings hides the other cards generated from the same article until the
// next local day boundary, and returns how many it hid. Siblings share
// retrieval cues: answering the first one primes the rest, so a batch studied
// back to back measures priming rather than memory.
//
// Three restrictions matter more than the mechanism:
//
//   - Learning and relearning cards (state 1 and 3) are never buried. A card
//     failed minutes ago is due in five, and burying it would delete the
//     same-session re-retrieval that ISC-51 and ISC-74 exist to restore. Anki
//     draws the same line: interday learning siblings are left alone by
//     default.
//   - Only cards already due are buried. A card due next week is not competing
//     for this session and does not need hiding.
//   - Only cards in decks owned by userID are touched. A write path is a place
//     to leak just as much as a read path is.
//
// It writes buried_until and nothing else — not even updated_at. Burying is
// presentation, not a change to the card.
func (s *CardService) BurySiblings(cardID, userID string, now time.Time, loc *time.Location) (int, error) {
	boundary := nextDayBoundary(now, loc).UTC().Format(time.RFC3339)
	nowStr := now.UTC().Format(time.RFC3339)

	result, err := s.db.Exec(`
		UPDATE cards SET buried_until = ?
		WHERE id != ?
		  AND article_id IS NOT NULL
		  AND article_id = (SELECT article_id FROM cards WHERE id = ?)
		  AND deck_id IN (SELECT id FROM decks WHERE user_id = ?)
		  AND state NOT IN (1, 3)
		  AND suspended = 0
		  AND due <= ?
		  AND (buried_until IS NULL OR buried_until < ?)
	`, boundary, cardID, cardID, userID, nowStr, boundary)
	if err != nil {
		return 0, fmt.Errorf("bury siblings: %w", err)
	}
	buried, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("bury siblings: %w", err)
	}
	return int(buried), nil
}

// ListLeeches returns every card the user owns that has reached the leech
// threshold, worst first, across every deck. Suspended leeches are included:
// the list is where a learner goes to decide what to do about them, and a card
// already taken out of rotation is still a card that needs rewriting or
// deleting.
func (s *CardService) ListLeeches(userID string) ([]models.Card, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.deck_id, c.front, c.back, c.due, c.stability, c.difficulty, c.elapsed_days,
			c.scheduled_days, c.reps, c.lapses, c.state, c.last_review, c.created_at, c.updated_at,
			c.article_id, c.kind, c.buried_until, c.suspended
		FROM cards c JOIN decks d ON c.deck_id = d.id
		WHERE d.user_id = ? AND c.lapses >= ?
		ORDER BY c.lapses DESC, c.updated_at DESC
	`, userID, models.LeechThreshold)
	if err != nil {
		return nil, fmt.Errorf("list leeches: %w", err)
	}
	defer rows.Close()

	cards := []models.Card{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		cards = append(cards, *c)
	}
	return cards, rows.Err()
}

// SetSuspendedForUser takes a card out of every study path, or puts it back.
// Like burying, it writes one presentation column: a card that comes back
// carries the schedule it left with, so suspending is never a way to lose work.
func (s *CardService) SetSuspendedForUser(cardID, userID string, suspended bool) error {
	result, err := s.db.Exec(`
		UPDATE cards SET suspended = ?
		WHERE id = ? AND deck_id IN (SELECT id FROM decks WHERE user_id = ?)
	`, suspended, cardID, userID)
	if err != nil {
		return fmt.Errorf("set suspended: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("set suspended: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("card not found")
	}
	return nil
}

// UnburyDeck clears every bury in one deck, so a learner who wants the whole
// batch now can have it. Like burying, it touches no FSRS column: the cards
// come back with exactly the schedule they had.
func (s *CardService) UnburyDeck(deckID, userID string) (int, error) {
	result, err := s.db.Exec(`
		UPDATE cards SET buried_until = NULL
		WHERE deck_id = ?
		  AND deck_id IN (SELECT id FROM decks WHERE user_id = ?)
		  AND buried_until IS NOT NULL
	`, deckID, userID)
	if err != nil {
		return 0, fmt.Errorf("unbury deck: %w", err)
	}
	unburied, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("unbury deck: %w", err)
	}
	return int(unburied), nil
}

func (s *CardService) Delete(cardID string) error {
	result, err := s.db.Exec("DELETE FROM cards WHERE id = ?", cardID)
	if err != nil {
		return fmt.Errorf("delete card: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("card not found")
	}
	return nil
}

// DeleteForUser deletes a card only if it belongs to a deck owned by userID.
func (s *CardService) DeleteForUser(cardID, userID string) error {
	result, err := s.db.Exec(`
		DELETE FROM cards WHERE id = ? AND deck_id IN (SELECT id FROM decks WHERE user_id = ?)`,
		cardID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete card: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("card not found")
	}
	return nil
}

func (s *CardService) UpdateFSRS(card *models.Card) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE cards SET due = ?, stability = ?, difficulty = ?, elapsed_days = ?,
			scheduled_days = ?, reps = ?, lapses = ?, state = ?, last_review = ?, updated_at = ?
		WHERE id = ?
	`, card.Due.Format(time.RFC3339), card.Stability, card.Difficulty, card.ElapsedDays,
		card.ScheduledDays, card.Reps, card.Lapses, card.State, card.LastReview.Format(time.RFC3339),
		now.Format(time.RFC3339), card.ID)
	return err
}

func (s *CardService) ImportCSV(deckID string, r io.Reader) (int, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1 // allow variable fields
	reader.LazyQuotes = true

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	count := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, fmt.Errorf("read csv: %w", err)
		}
		if len(record) < 2 {
			continue
		}

		front := strings.TrimSpace(record[0])
		back := strings.TrimSpace(record[1])
		if front == "" || back == "" {
			continue
		}

		id := generateID()
		now := time.Now().UTC()
		_, err = tx.Exec(`
			INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days, scheduled_days, reps, lapses, state, last_review, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 0, 0, 0, 0, 0, 0, 0, '0001-01-01T00:00:00Z', ?, ?)
		`, id, deckID, front, back, now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339))
		if err != nil {
			return count, fmt.Errorf("insert card from csv: %w", err)
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}
	return count, nil
}

type scannable interface {
	Scan(dest ...interface{}) error
}

func scanCardFromRow(s scannable) (*models.Card, error) {
	var c models.Card
	var due, lastReview, createdAt, updatedAt string
	var articleID, buriedUntil *string
	err := s.Scan(&c.ID, &c.DeckID, &c.Front, &c.Back, &due, &c.Stability, &c.Difficulty,
		&c.ElapsedDays, &c.ScheduledDays, &c.Reps, &c.Lapses, &c.State, &lastReview, &createdAt, &updatedAt,
		&articleID, &c.Kind, &buriedUntil, &c.Suspended)
	if err != nil {
		return nil, fmt.Errorf("scan card: %w", err)
	}
	if buriedUntil != nil {
		if t, err := time.Parse(time.RFC3339, *buriedUntil); err == nil {
			c.BuriedUntil = &t
		}
	}
	c.Due, _ = time.Parse(time.RFC3339, due)
	c.LastReview, _ = time.Parse(time.RFC3339, lastReview)
	c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	c.ArticleID = articleID
	return &c, nil
}

func scanCard(rows *sql.Rows) (*models.Card, error) {
	return scanCardFromRow(rows)
}

func scanCardRow(row *sql.Row) (*models.Card, error) {
	return scanCardFromRow(row)
}
