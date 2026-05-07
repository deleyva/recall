package services

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/deleyva/recall/internal/models"
	"golang.org/x/net/html"
)

type ArticleService struct {
	db *sql.DB
}

func NewArticleService(db *sql.DB) *ArticleService {
	return &ArticleService{db: db}
}

func (s *ArticleService) Create(userID, rawURL string) (*models.Article, error) {
	// Parse and validate URL
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid URL: must be http or https")
	}
	domain := parsed.Hostname()

	// Fetch content
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch URL: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Recall/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch URL: status %d", resp.StatusCode)
	}

	// Limit reading to 1MB
	limited := io.LimitReader(resp.Body, 1024*1024)
	title, content := extractHTMLContent(limited)

	if title == "" {
		title = domain
	}

	// Truncate content to 50KB
	if len(content) > 50*1024 {
		content = content[:50*1024]
	}

	id := generateID()
	now := time.Now().UTC()

	_, err = s.db.Exec(`
		INSERT INTO articles (id, user_id, url, title, domain, content, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, userID, rawURL, title, domain, content, now.Format(time.RFC3339))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("article already added")
		}
		return nil, fmt.Errorf("create article: %w", err)
	}

	return &models.Article{
		ID:        id,
		UserID:    userID,
		URL:       rawURL,
		Title:     title,
		Domain:    domain,
		Content:   content,
		CreatedAt: now,
	}, nil
}

// CreateDirect creates an article with pre-fetched content (e.g., from Readeck webhook).
func (s *ArticleService) CreateDirect(userID, rawURL, title, content string) (*models.Article, error) {
	domain := ""
	if rawURL != "" {
		parsed, _ := url.Parse(rawURL)
		if parsed != nil {
			domain = parsed.Hostname()
		}
	}
	if title == "" {
		if domain != "" {
			title = domain
		} else {
			title = "Untitled"
		}
	}

	// Truncate content to 50KB
	if len(content) > 50*1024 {
		content = content[:50*1024]
	}

	id := generateID()
	now := time.Now().UTC()

	_, err := s.db.Exec(`
		INSERT INTO articles (id, user_id, url, title, domain, content, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, userID, rawURL, title, domain, content, now.Format(time.RFC3339))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("article already added")
		}
		return nil, fmt.Errorf("create article: %w", err)
	}

	return &models.Article{
		ID:        id,
		UserID:    userID,
		URL:       rawURL,
		Title:     title,
		Domain:    domain,
		Content:   content,
		CreatedAt: now,
	}, nil
}

func (s *ArticleService) List(userID string) ([]models.Article, error) {
	rows, err := s.db.Query(`
		SELECT a.id, a.user_id, a.url, a.title, a.domain, a.created_at,
			COALESCE((SELECT COUNT(*) FROM cards c WHERE c.article_id = a.id), 0) as flashcard_count
		FROM articles a
		WHERE a.user_id = ?
		ORDER BY a.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list articles: %w", err)
	}
	defer rows.Close()

	var articles []models.Article
	for rows.Next() {
		var a models.Article
		var createdAt string
		if err := rows.Scan(&a.ID, &a.UserID, &a.URL, &a.Title, &a.Domain, &createdAt, &a.FlashcardCount); err != nil {
			return nil, fmt.Errorf("scan article: %w", err)
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		articles = append(articles, a)
	}
	return articles, nil
}

// ListForCardGeneration returns articles ordered for cron card generation:
// fewest flashcards first, then longest content, then newest.
func (s *ArticleService) ListForCardGeneration(userID string) ([]models.Article, error) {
	rows, err := s.db.Query(`
		SELECT a.id, a.user_id, a.url, a.title, a.domain, a.created_at,
			COALESCE((SELECT COUNT(*) FROM cards c WHERE c.article_id = a.id), 0) as flashcard_count
		FROM articles a
		WHERE a.user_id = ?
		GROUP BY a.id
		HAVING flashcard_count < 20
		ORDER BY flashcard_count ASC, LENGTH(COALESCE(a.content, '')) DESC, a.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list articles for card generation: %w", err)
	}
	defer rows.Close()

	var articles []models.Article
	for rows.Next() {
		var a models.Article
		var createdAt string
		if err := rows.Scan(&a.ID, &a.UserID, &a.URL, &a.Title, &a.Domain, &createdAt, &a.FlashcardCount); err != nil {
			return nil, fmt.Errorf("scan article: %w", err)
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		articles = append(articles, a)
	}
	return articles, nil
}

func (s *ArticleService) Get(userID, articleID string) (*models.Article, error) {
	var a models.Article
	var createdAt string
	err := s.db.QueryRow(`
		SELECT id, user_id, url, title, domain, content, created_at
		FROM articles
		WHERE id = ? AND user_id = ?
	`, articleID, userID).Scan(&a.ID, &a.UserID, &a.URL, &a.Title, &a.Domain, &a.Content, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("get article: %w", err)
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &a, nil
}

func (s *ArticleService) Delete(userID, articleID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete cards linked to this article
	_, err = tx.Exec("DELETE FROM cards WHERE article_id = ?", articleID)
	if err != nil {
		return fmt.Errorf("delete article cards: %w", err)
	}

	// Delete the article
	result, err := tx.Exec("DELETE FROM articles WHERE id = ? AND user_id = ?", articleID, userID)
	if err != nil {
		return fmt.Errorf("delete article: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("article not found")
	}

	return tx.Commit()
}

func (s *ArticleService) GetCardsForArticle(articleID string) ([]models.Card, error) {
	rows, err := s.db.Query(`
		SELECT id, deck_id, front, back, due, stability, difficulty, elapsed_days, scheduled_days,
			reps, lapses, state, last_review, created_at, updated_at, article_id
		FROM cards WHERE article_id = ?
		ORDER BY created_at ASC
	`, articleID)
	if err != nil {
		return nil, fmt.Errorf("get cards for article: %w", err)
	}
	defer rows.Close()

	var cards []models.Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		cards = append(cards, *c)
	}
	return cards, nil
}

func (s *ArticleService) CountCardsCreatedToday(userID string) (int, error) {
	today := time.Now().UTC().Format("2006-01-02")
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM cards c
		JOIN decks d ON c.deck_id = d.id
		WHERE d.user_id = ? AND date(c.created_at) = ?
	`, userID, today).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count cards today: %w", err)
	}
	return count, nil
}

func (s *ArticleService) EnsureReadingDeck(userID string) (string, error) {
	var deckID string
	err := s.db.QueryRow(
		"SELECT id FROM decks WHERE user_id = ? AND name = 'Reading List'",
		userID,
	).Scan(&deckID)
	if err == nil {
		return deckID, nil
	}

	// Create the deck
	deckID = generateID()
	now := time.Now().UTC()
	_, err = s.db.Exec(
		"INSERT INTO decks (id, user_id, name, description, created_at) VALUES (?, ?, 'Reading List', 'Auto-generated deck for reading list flashcards', ?)",
		deckID, userID, now.Format(time.RFC3339),
	)
	if err != nil {
		// Maybe another request created it concurrently
		err2 := s.db.QueryRow(
			"SELECT id FROM decks WHERE user_id = ? AND name = 'Reading List'",
			userID,
		).Scan(&deckID)
		if err2 != nil {
			return "", fmt.Errorf("ensure reading deck: %w", err)
		}
	}
	return deckID, nil
}

// extractHTMLContent parses HTML and returns (title, text content)
func extractHTMLContent(r io.Reader) (string, string) {
	tokenizer := html.NewTokenizer(r)
	var title string
	var content strings.Builder
	inTitle := false
	inScript := false
	inStyle := false

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return title, content.String()
		case html.StartTagToken:
			tn, _ := tokenizer.TagName()
			tag := string(tn)
			switch tag {
			case "title":
				inTitle = true
			case "script":
				inScript = true
			case "style":
				inStyle = true
			}
		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			tag := string(tn)
			switch tag {
			case "title":
				inTitle = false
			case "script":
				inScript = false
			case "style":
				inStyle = false
			}
		case html.TextToken:
			text := strings.TrimSpace(string(tokenizer.Text()))
			if text == "" {
				continue
			}
			if inTitle && title == "" {
				title = text
			}
			if !inScript && !inStyle && !inTitle {
				content.WriteString(text)
				content.WriteString(" ")
			}
		}
	}
}
