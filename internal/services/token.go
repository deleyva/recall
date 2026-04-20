package services

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/deleyva/recall/internal/models"
)

type TokenService struct {
	db *sql.DB
}

func NewTokenService(db *sql.DB) *TokenService {
	return &TokenService{db: db}
}

// Create generates a new API token and returns it. The raw token is only returned once.
func (s *TokenService) Create(userID, name string) (string, *models.APIToken, error) {
	// Generate 32-byte random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	rawToken := "rcl_" + hex.EncodeToString(tokenBytes)

	// Store hash only
	hash := hashToken(rawToken)
	id := generateID()
	now := time.Now().UTC()

	_, err := s.db.Exec(
		"INSERT INTO api_tokens (id, user_id, token_hash, name, created_at) VALUES (?, ?, ?, ?, ?)",
		id, userID, hash, name, now.Format(time.RFC3339),
	)
	if err != nil {
		return "", nil, fmt.Errorf("create token: %w", err)
	}

	return rawToken, &models.APIToken{
		ID:        id,
		UserID:    userID,
		Name:      name,
		CreatedAt: now,
	}, nil
}

func (s *TokenService) List(userID string) ([]models.APIToken, error) {
	rows, err := s.db.Query(
		"SELECT id, user_id, name, created_at FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()

	var tokens []models.APIToken
	for rows.Next() {
		var t models.APIToken
		var createdAt string
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &createdAt); err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		tokens = append(tokens, t)
	}
	return tokens, nil
}

func (s *TokenService) Delete(userID, tokenID string) error {
	result, err := s.db.Exec(
		"DELETE FROM api_tokens WHERE id = ? AND user_id = ?",
		tokenID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("token not found")
	}
	return nil
}

// ValidateToken checks a raw token against stored hashes. Returns user_id if valid.
func (s *TokenService) ValidateToken(rawToken string) (string, error) {
	hash := hashToken(rawToken)
	var userID string
	err := s.db.QueryRow(
		"SELECT user_id FROM api_tokens WHERE token_hash = ?",
		hash,
	).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("invalid token")
	}
	return userID, nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
