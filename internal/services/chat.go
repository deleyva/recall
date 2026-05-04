package services

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/deleyva/recall/internal/models"
)

type ChatService struct {
	db *sql.DB
}

func NewChatService(db *sql.DB) *ChatService {
	return &ChatService{db: db}
}

func (s *ChatService) Create(articleID, userID, role, content string) (*models.ChatMessage, error) {
	id := generateID()
	now := time.Now().UTC()

	_, err := s.db.Exec(`
		INSERT INTO chat_messages (id, article_id, user_id, role, content, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, articleID, userID, role, content, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("create chat message: %w", err)
	}

	return &models.ChatMessage{
		ID:        id,
		ArticleID: articleID,
		UserID:    userID,
		Role:      role,
		Content:   content,
		CreatedAt: now,
	}, nil
}

func (s *ChatService) ListByArticle(articleID, userID string) ([]models.ChatMessage, error) {
	rows, err := s.db.Query(`
		SELECT id, article_id, user_id, role, content, created_at
		FROM chat_messages
		WHERE article_id = ? AND user_id = ?
		ORDER BY created_at ASC
	`, articleID, userID)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	defer rows.Close()

	var messages []models.ChatMessage
	for rows.Next() {
		var m models.ChatMessage
		var createdAt string
		if err := rows.Scan(&m.ID, &m.ArticleID, &m.UserID, &m.Role, &m.Content, &createdAt); err != nil {
			return nil, fmt.Errorf("scan chat message: %w", err)
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		messages = append(messages, m)
	}
	return messages, nil
}

func (s *ChatService) DeleteByArticle(articleID, userID string) error {
	_, err := s.db.Exec(`
		DELETE FROM chat_messages WHERE article_id = ? AND user_id = ?
	`, articleID, userID)
	if err != nil {
		return fmt.Errorf("delete chat messages: %w", err)
	}
	return nil
}
