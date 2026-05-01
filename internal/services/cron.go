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
}

func NewCronService(db *sql.DB, articles *ArticleService, cards *CardService, gemini *GeminiService) *CronService {
	return &CronService{
		db:       db,
		articles: articles,
		cards:    cards,
		gemini:   gemini,
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
	maxCards := 8
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

	log.Printf("[cron] User %s: total %d cards generated", userID, totalGenerated)
}
