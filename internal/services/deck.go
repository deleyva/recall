package services

import (
	"database/sql"
	"fmt"
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

func (s *DeckService) List(userID string) ([]models.Deck, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := s.db.Query(`
		SELECT d.id, d.user_id, d.name, d.description, d.created_at,
			COALESCE((SELECT COUNT(*) FROM cards c WHERE c.deck_id = d.id AND c.due <= ?), 0) as due_count
		FROM decks d
		WHERE d.user_id = ?
		ORDER BY due_count DESC, d.created_at DESC
	`, now, userID)
	if err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	defer rows.Close()

	var decks []models.Deck
	for rows.Next() {
		var d models.Deck
		var createdAt string
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.Description, &createdAt, &d.DueCount); err != nil {
			return nil, fmt.Errorf("scan deck: %w", err)
		}
		d.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		decks = append(decks, d)
	}
	return decks, nil
}

func (s *DeckService) Get(userID, deckID string) (*models.Deck, error) {
	var d models.Deck
	var createdAt string

	now := time.Now().UTC().Format(time.RFC3339)
	err := s.db.QueryRow(`
		SELECT d.id, d.user_id, d.name, d.description, d.created_at,
			COALESCE((SELECT COUNT(*) FROM cards c WHERE c.deck_id = d.id AND c.due <= ?), 0) as due_count
		FROM decks d
		WHERE d.id = ? AND d.user_id = ?
	`, now, deckID, userID).Scan(&d.ID, &d.UserID, &d.Name, &d.Description, &createdAt, &d.DueCount)
	if err != nil {
		return nil, fmt.Errorf("get deck: %w", err)
	}
	d.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &d, nil
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
