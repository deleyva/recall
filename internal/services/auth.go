package services

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/deleyva/recall/internal/models"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserExists         = errors.New("user already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type AuthService struct {
	db *sql.DB
}

func NewAuthService(db *sql.DB) *AuthService {
	return &AuthService{db: db}
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *AuthService) Register(email, password string) (*models.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	id := generateID()
	now := time.Now().UTC()

	_, err = s.db.Exec(
		"INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)",
		id, email, string(hash), now.Format(time.RFC3339),
	)
	if err != nil {
		return nil, ErrUserExists
	}

	return &models.User{
		ID:        id,
		Email:     email,
		CreatedAt: now,
	}, nil
}

func (s *AuthService) ResetPassword(email, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	result, err := s.db.Exec(
		"UPDATE users SET password_hash = ? WHERE email = ?",
		string(hash), email,
	)
	if err != nil {
		return fmt.Errorf("reset password: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found: %s", email)
	}
	return nil
}

func (s *AuthService) ListUsers() ([]string, error) {
	rows, err := s.db.Query("SELECT email FROM users ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var emails []string
	for rows.Next() {
		var e string
		rows.Scan(&e)
		emails = append(emails, e)
	}
	return emails, nil
}

func (s *AuthService) Login(email, password string) (*models.User, error) {
	var user models.User
	var hash string
	var createdAt string

	err := s.db.QueryRow(
		"SELECT id, email, password_hash, created_at FROM users WHERE email = ?",
		email,
	).Scan(&user.ID, &user.Email, &hash, &createdAt)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	user.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &user, nil
}
