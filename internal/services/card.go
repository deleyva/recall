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

func NewCardService(db *sql.DB) *CardService {
	return &CardService{db: db}
}

func (s *CardService) Create(deckID, front, back string) (*models.Card, error) {
	id := generateID()
	now := time.Now().UTC()

	_, err := s.db.Exec(`
		INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days, scheduled_days, reps, lapses, state, last_review, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, 0, 0, 0, 0, 0, 0, '0001-01-01T00:00:00Z', ?, ?)
	`, id, deckID, front, back, now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("create card: %w", err)
	}

	return &models.Card{
		ID:        id,
		DeckID:    deckID,
		Front:     front,
		Back:      back,
		Due:       now,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
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
			reps, lapses, state, last_review, created_at, updated_at
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

func (s *CardService) Get(cardID string) (*models.Card, error) {
	row := s.db.QueryRow(`
		SELECT id, deck_id, front, back, due, stability, difficulty, elapsed_days, scheduled_days,
			reps, lapses, state, last_review, created_at, updated_at
		FROM cards WHERE id = ?
	`, cardID)
	return scanCardRow(row)
}

// GetForUser returns a card only if it belongs to a deck owned by userID.
func (s *CardService) GetForUser(cardID, userID string) (*models.Card, error) {
	row := s.db.QueryRow(`
		SELECT c.id, c.deck_id, c.front, c.back, c.due, c.stability, c.difficulty, c.elapsed_days, c.scheduled_days,
			c.reps, c.lapses, c.state, c.last_review, c.created_at, c.updated_at
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
	err := s.Scan(&c.ID, &c.DeckID, &c.Front, &c.Back, &due, &c.Stability, &c.Difficulty,
		&c.ElapsedDays, &c.ScheduledDays, &c.Reps, &c.Lapses, &c.State, &lastReview, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan card: %w", err)
	}
	c.Due, _ = time.Parse(time.RFC3339, due)
	c.LastReview, _ = time.Parse(time.RFC3339, lastReview)
	c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &c, nil
}

func scanCard(rows *sql.Rows) (*models.Card, error) {
	return scanCardFromRow(rows)
}

func scanCardRow(row *sql.Row) (*models.Card, error) {
	return scanCardFromRow(row)
}
