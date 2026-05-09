package services

import (
	"database/sql"
	"log"
	"time"


)

type CronService struct {
	db       *sql.DB
	articles *ArticleService
	cards    *CardService
	gemini   *GeminiService
	podcasts *PodcastService
}

func NewCronService(db *sql.DB, articles *ArticleService, cards *CardService, gemini *GeminiService, podcasts *PodcastService) *CronService {
	return &CronService{
		db:       db,
		articles: articles,
		cards:    cards,
		gemini:   gemini,
		podcasts: podcasts,
	}
}

// GenerateDailyCards generates up to 8 flashcards per user from their reading list.
// Prioritizes articles with 0 flashcards, then longer articles.
// Caps at 2 cards per article per run to spread across articles.
func (s *CronService) GenerateDailyCards() {
	if !s.gemini.IsConfigured() {
		log.Println("[cron] Gemini not configured, skipping daily card generation")
		return
	}

	log.Println("[cron] Starting daily card generation")

	// Get all users who have articles
	rows, err := s.db.Query(`
		SELECT DISTINCT user_id FROM articles
	`)
	if err != nil {
		log.Printf("[cron] Failed to get users: %v", err)
		return
	}
	defer rows.Close()

	var userIDs []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			continue
		}
		userIDs = append(userIDs, uid)
	}

	for _, userID := range userIDs {
		s.generateForUser(userID)
	}

	log.Println("[cron] Daily card generation complete")
}

func (s *CronService) generateForUser(userID string) {
	// Get articles prioritized: fewest cards first, then newest first
	articles, err := s.articles.ListForCardGeneration(userID)
	if err != nil {
		log.Printf("[cron] Failed to list articles for user %s: %v", userID, err)
		return
	}

	if len(articles) == 0 {
		return
	}

	allArticles := articles

	// Ensure reading deck exists
	deckID, err := s.articles.EnsureReadingDeck(userID)
	if err != nil {
		log.Printf("[cron] Failed to ensure deck for user %s: %v", userID, err)
		return
	}

	totalGenerated := 0
	maxCards := s.getUserDailyLimit(userID)
	maxCardsPerArticle := 2

	for _, article := range allArticles {
		if totalGenerated >= maxCards {
			break
		}

		remaining := maxCards - totalGenerated
		if remaining > maxCardsPerArticle {
			remaining = maxCardsPerArticle
		}

		// Get article content
		full, err := s.articles.Get(userID, article.ID)
		if err != nil {
			continue
		}

		if full.Content == "" {
			continue
		}

		// Get existing cards
		existing, _ := s.articles.GetCardsForArticle(article.ID)

		// Generate flashcards
		pairs, err := s.gemini.GenerateFlashcards(full.Content, existing, remaining)
		if err != nil {
			log.Printf("[cron] Failed to generate for article %s: %v", article.ID, err)
			continue
		}

		// Save
		articleID := article.ID
		count, err := s.cards.CreateBatch(deckID, &articleID, pairs)
		if err != nil {
			log.Printf("[cron] Failed to save cards for article %s: %v", article.ID, err)
			continue
		}

		totalGenerated += count
		log.Printf("[cron] Generated %d cards for article '%s' (user %s)", count, article.Title, userID)

		// Small delay between API calls
		time.Sleep(2 * time.Second)
	}

	log.Printf("[cron] User %s: total %d cards generated (limit: %d)", userID, totalGenerated, maxCards)
}

func (s *CronService) getUserDailyLimit(userID string) int {
	var limit int
	err := s.db.QueryRow("SELECT daily_card_limit FROM users WHERE id = ?", userID).Scan(&limit)
	if err != nil || limit <= 0 {
		return 5 // default
	}
	return limit
}

// GenerateDailyPodcasts creates podcast requests for users with podcast_enabled.
// Only includes articles added in the last 24 hours. Skips if no new articles.
func (s *CronService) GenerateDailyPodcasts() {
	log.Println("[cron] Starting daily podcast generation")

	rows, err := s.db.Query(`
		SELECT id FROM users WHERE podcast_enabled = 1 AND is_admin = 1
	`)
	if err != nil {
		log.Printf("[cron] Failed to get podcast-enabled users: %v", err)
		return
	}
	defer rows.Close()

	var userIDs []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			continue
		}
		userIDs = append(userIDs, uid)
	}

	yesterday := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)

	for _, userID := range userIDs {
		// Get articles from last 24h
		artRows, err := s.db.Query(`
			SELECT id, title FROM articles
			WHERE user_id = ? AND created_at > ?
			ORDER BY created_at DESC
			LIMIT 10
		`, userID, yesterday)
		if err != nil {
			log.Printf("[cron] Failed to get recent articles for user %s: %v", userID, err)
			continue
		}

		var articleIDs []string
		for artRows.Next() {
			var id, title string
			if err := artRows.Scan(&id, &title); err == nil {
				articleIDs = append(articleIDs, id)
			}
		}
		artRows.Close()

		if len(articleIDs) == 0 {
			continue
		}

		podTitle := time.Now().Format("2006-01-02") + " Daily Podcast"
		_, err = s.podcasts.Create(userID, podTitle, articleIDs)
		if err != nil {
			log.Printf("[cron] Failed to create podcast for user %s: %v", userID, err)
			continue
		}

		log.Printf("[cron] Created podcast for user %s with %d articles", userID, len(articleIDs))
	}

	log.Println("[cron] Daily podcast generation complete")
}
