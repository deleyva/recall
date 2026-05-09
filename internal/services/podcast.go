package services

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/deleyva/recall/internal/models"
)

type PodcastService struct {
	db *sql.DB
}

func NewPodcastService(db *sql.DB) *PodcastService {
	return &PodcastService{db: db}
}

func (s *PodcastService) Create(userID, title string, articleIDs []string) (*models.Podcast, error) {
	if len(articleIDs) == 0 {
		return nil, fmt.Errorf("at least one article required")
	}
	if len(articleIDs) > 10 {
		articleIDs = articleIDs[:10]
	}

	id := generateID()
	now := time.Now().UTC()

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO podcasts (id, user_id, title, status, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, id, userID, title, models.PodcastStatusPending, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("create podcast: %w", err)
	}

	for _, articleID := range articleIDs {
		_, err = tx.Exec(`
			INSERT INTO podcast_articles (podcast_id, article_id) VALUES (?, ?)
		`, id, articleID)
		if err != nil {
			return nil, fmt.Errorf("link article %s: %w", articleID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &models.Podcast{
		ID:           id,
		UserID:       userID,
		Title:        title,
		Status:       models.PodcastStatusPending,
		CreatedAt:    now,
		ArticleCount: len(articleIDs),
	}, nil
}

func (s *PodcastService) List(userID string) ([]models.Podcast, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.user_id, p.title, p.status, p.audio_url, p.notebook_id, p.created_at, p.completed_at,
			(SELECT COUNT(*) FROM podcast_articles pa WHERE pa.podcast_id = p.id) as article_count
		FROM podcasts p
		WHERE p.user_id = ?
		ORDER BY p.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list podcasts: %w", err)
	}
	defer rows.Close()

	var podcasts []models.Podcast
	for rows.Next() {
		p, err := scanPodcast(rows)
		if err != nil {
			return nil, err
		}
		podcasts = append(podcasts, *p)
	}
	return podcasts, nil
}

func (s *PodcastService) Get(userID, podcastID string) (*models.Podcast, error) {
	row := s.db.QueryRow(`
		SELECT p.id, p.user_id, p.title, p.status, p.audio_url, p.notebook_id, p.created_at, p.completed_at,
			(SELECT COUNT(*) FROM podcast_articles pa WHERE pa.podcast_id = p.id) as article_count
		FROM podcasts p
		WHERE p.id = ? AND p.user_id = ?
	`, podcastID, userID)

	var p models.Podcast
	var createdAt, completedAt string
	err := row.Scan(&p.ID, &p.UserID, &p.Title, &p.Status, &p.AudioURL, &p.NotebookID, &createdAt, &completedAt)
	if err != nil {
		return nil, fmt.Errorf("get podcast: %w", err)
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if completedAt != "" {
		p.CompletedAt, _ = time.Parse(time.RFC3339, completedAt)
	}

	// Load articles
	artRows, err := s.db.Query(`
		SELECT a.id, a.user_id, a.url, a.title, a.domain, a.created_at
		FROM articles a
		JOIN podcast_articles pa ON pa.article_id = a.id
		WHERE pa.podcast_id = ?
	`, podcastID)
	if err == nil {
		defer artRows.Close()
		for artRows.Next() {
			var a models.Article
			var cat string
			if err := artRows.Scan(&a.ID, &a.UserID, &a.URL, &a.Title, &a.Domain, &cat); err == nil {
				a.CreatedAt, _ = time.Parse(time.RFC3339, cat)
				p.Articles = append(p.Articles, a)
			}
		}
	}
	p.ArticleCount = len(p.Articles)

	return &p, nil
}

func (s *PodcastService) UpdateStatus(podcastID, status, audioURL, notebookID string) error {
	completedAt := ""
	if status == models.PodcastStatusCompleted || status == models.PodcastStatusFailed {
		completedAt = time.Now().UTC().Format(time.RFC3339)
	}

	_, err := s.db.Exec(`
		UPDATE podcasts SET status = ?, audio_url = ?, notebook_id = ?, completed_at = ?
		WHERE id = ?
	`, status, audioURL, notebookID, completedAt, podcastID)
	if err != nil {
		return fmt.Errorf("update podcast status: %w", err)
	}
	return nil
}

func (s *PodcastService) Delete(userID, podcastID string) error {
	result, err := s.db.Exec("DELETE FROM podcasts WHERE id = ? AND user_id = ?", podcastID, userID)
	if err != nil {
		return fmt.Errorf("delete podcast: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("podcast not found")
	}
	return nil
}

func (s *PodcastService) ListPending() ([]models.Podcast, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.user_id, p.title, p.status, p.audio_url, p.notebook_id, p.created_at, p.completed_at,
			(SELECT COUNT(*) FROM podcast_articles pa WHERE pa.podcast_id = p.id) as article_count
		FROM podcasts p
		WHERE p.status = ?
		ORDER BY p.created_at ASC
	`, models.PodcastStatusPending)
	if err != nil {
		return nil, fmt.Errorf("list pending podcasts: %w", err)
	}
	defer rows.Close()

	// Collect podcasts first, then load articles (avoid nested queries on SQLite)
	var podcasts []models.Podcast
	for rows.Next() {
		p, err := scanPodcast(rows)
		if err != nil {
			return nil, err
		}
		podcasts = append(podcasts, *p)
	}

	// Now load article content for each podcast
	for i := range podcasts {
		artRows, err := s.db.Query(`
			SELECT a.id, a.title, a.url, a.content
			FROM articles a
			JOIN podcast_articles pa ON pa.article_id = a.id
			WHERE pa.podcast_id = ?
		`, podcasts[i].ID)
		if err != nil {
			continue
		}
		for artRows.Next() {
			var a models.Article
			if err := artRows.Scan(&a.ID, &a.Title, &a.URL, &a.Content); err == nil {
				podcasts[i].Articles = append(podcasts[i].Articles, a)
			}
		}
		artRows.Close()
	}

	return podcasts, nil
}

func (s *PodcastService) GetArticleContent(podcastID string) (string, error) {
	rows, err := s.db.Query(`
		SELECT a.title, a.content
		FROM articles a
		JOIN podcast_articles pa ON pa.article_id = a.id
		WHERE pa.podcast_id = ?
	`, podcastID)
	if err != nil {
		return "", fmt.Errorf("get article content: %w", err)
	}
	defer rows.Close()

	var sb strings.Builder
	for rows.Next() {
		var title, content string
		if err := rows.Scan(&title, &content); err != nil {
			continue
		}
		sb.WriteString("# ")
		sb.WriteString(title)
		sb.WriteString("\n\n")
		sb.WriteString(content)
		sb.WriteString("\n\n---\n\n")
	}
	return sb.String(), nil
}

func scanPodcast(row scannable) (*models.Podcast, error) {
	var p models.Podcast
	var createdAt, completedAt string
	err := row.Scan(&p.ID, &p.UserID, &p.Title, &p.Status, &p.AudioURL, &p.NotebookID, &createdAt, &completedAt, &p.ArticleCount)
	if err != nil {
		return nil, fmt.Errorf("scan podcast: %w", err)
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if completedAt != "" {
		p.CompletedAt, _ = time.Parse(time.RFC3339, completedAt)
	}
	return &p, nil
}
