package services

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/deleyva/recall/internal/models"
)

type DeckService struct {
	db *sql.DB
}

func NewDeckService(db *sql.DB) *DeckService {
	return &DeckService{db: db}
}

func (s *DeckService) Create(userID, name, description string) (*models.Deck, error) {
	id := generateID()
	now := time.Now().UTC()

	_, err := s.db.Exec(
		"INSERT INTO decks (id, user_id, name, description, created_at) VALUES (?, ?, ?, ?, ?)",
		id, userID, name, description, now.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("create deck: %w", err)
	}

	return &models.Deck{
		ID:          id,
		UserID:      userID,
		Name:        name,
		Description: description,
		CreatedAt:   now,
	}, nil
}

// deckCounts fills in the two computed fields on a deck: how many cards a
// session would actually serve from it today, and how many are held back as
// siblings. The due count is capped by the day's study budget — a "Study (5)"
// that opens onto two cards is the same defect burying and suspension each had.
func (s *DeckService) deckCounts(userID string, decks []models.Deck) {
	reviews := NewReviewService(s.db)
	newLeft, reviewLeft, err := reviews.remainingBudget(userID, time.Local)
	if err != nil {
		newLeft, reviewLeft = 0, 0
	}
	now := time.Now().UTC().Format(time.RFC3339)

	for i := range decks {
		decks[i].DueCount = reviews.CappedDueCount(
			"c.deck_id = ? AND c.suspended = 0 AND (c.buried_until IS NULL OR c.buried_until <= ?)",
			[]interface{}{decks[i].ID, now}, newLeft, reviewLeft)
		s.db.QueryRow(`SELECT COUNT(*) FROM cards
			WHERE deck_id = ? AND suspended = 0 AND buried_until > ?`,
			decks[i].ID, now).Scan(&decks[i].BuriedCount)
	}
}

func (s *DeckService) List(userID string) ([]models.Deck, error) {
	rows, err := s.db.Query(`
		SELECT d.id, d.user_id, d.name, d.description, d.created_at
		FROM decks d WHERE d.user_id = ?
		ORDER BY d.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	defer rows.Close()

	var decks []models.Deck
	for rows.Next() {
		var d models.Deck
		var createdAt string
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.Description, &createdAt); err != nil {
			return nil, fmt.Errorf("scan deck: %w", err)
		}
		d.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		decks = append(decks, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	s.deckCounts(userID, decks)
	// Decks with work to do come first, as they did when the count was computed
	// in SQL.
	sort.SliceStable(decks, func(i, j int) bool { return decks[i].DueCount > decks[j].DueCount })
	return decks, nil
}

func (s *DeckService) Get(userID, deckID string) (*models.Deck, error) {
	var d models.Deck
	var createdAt string

	err := s.db.QueryRow(`
		SELECT d.id, d.user_id, d.name, d.description, d.created_at
		FROM decks d WHERE d.id = ? AND d.user_id = ?
	`, deckID, userID).Scan(&d.ID, &d.UserID, &d.Name, &d.Description, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("get deck: %w", err)
	}
	d.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	one := []models.Deck{d}
	s.deckCounts(userID, one)
	return &one[0], nil
}

func (s *DeckService) Update(userID, deckID, name, description string) error {
	result, err := s.db.Exec(
		"UPDATE decks SET name = ?, description = ? WHERE id = ? AND user_id = ?",
		name, description, deckID, userID,
	)
	if err != nil {
		return fmt.Errorf("update deck: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("deck not found")
	}
	return nil
}

func (s *DeckService) Delete(userID, deckID string) error {
	result, err := s.db.Exec(
		"DELETE FROM decks WHERE id = ? AND user_id = ?",
		deckID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete deck: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("deck not found")
	}
	return nil
}
