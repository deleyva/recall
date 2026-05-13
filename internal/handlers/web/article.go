package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/labstack/echo/v4"
)

type ArticleHandler struct {
	articles  *services.ArticleService
	cards     *services.CardService
	decks     *services.DeckService
	gemini    *services.GeminiService
	wikipedia *services.WikipediaService
	tmpl      *templates.Registry
	db        *sql.DB
}

func NewArticleHandler(articles *services.ArticleService, cards *services.CardService, decks *services.DeckService, gemini *services.GeminiService, wikipedia *services.WikipediaService, tmpl *templates.Registry, db *sql.DB) *ArticleHandler {
	return &ArticleHandler{
		articles:  articles,
		cards:     cards,
		decks:     decks,
		gemini:    gemini,
		wikipedia: wikipedia,
		tmpl:      tmpl,
		db:        db,
	}
}

func (h *ArticleHandler) ListPage(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articles, err := h.articles.List(userID)
	if err != nil {
		return err
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "to_read.html", map[string]interface{}{
		"Articles":      articles,
		"Email":         c.Get(middleware.EmailKey),
		"IsAdmin":       middleware.IsAdmin(c),
		"Error":         c.QueryParam("error"),
		"Success":       c.QueryParam("success"),
		"GeminiEnabled": h.gemini.IsConfigured(),
	})
}

func (h *ArticleHandler) AddArticle(c echo.Context) error {
	userID := middleware.GetUserID(c)
	url := c.FormValue("url")

	if url == "" {
		return c.Redirect(http.StatusSeeOther, "/to-read?error=URL+required")
	}

	_, err := h.articles.Create(userID, url)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/to-read?error="+err.Error())
	}

	return c.Redirect(http.StatusSeeOther, "/to-read?success=Article+added")
}

func (h *ArticleHandler) DeleteArticle(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articleID := c.Param("id")

	if err := h.articles.Delete(userID, articleID); err != nil {
		return c.Redirect(http.StatusSeeOther, "/to-read?error=Could+not+delete+article")
	}

	return c.Redirect(http.StatusSeeOther, "/to-read?success=Article+deleted")
}

func (h *ArticleHandler) GenerateFlashcards(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articleID := c.Param("id")

	if !h.gemini.IsConfigured() {
		return c.Redirect(http.StatusSeeOther, "/to-read?error=Gemini+API+key+not+configured")
	}

	count, _ := strconv.Atoi(c.FormValue("count"))
	if count < 1 || count > 20 {
		count = 5
	}

	// Check daily limit
	todayCount, _ := h.articles.CountCardsCreatedToday(userID)
	if todayCount >= 5 && c.FormValue("confirmed") != "true" {
		// Return warning partial for HTMX
		if c.Request().Header.Get("HX-Request") == "true" {
			return h.tmpl.ExecuteTemplate(c.Response(), "daily_limit_warning_partial.html", map[string]interface{}{
				"ArticleID":  articleID,
				"Count":      count,
				"TodayCount": todayCount,
			})
		}
		return c.Redirect(http.StatusSeeOther, "/to-read?error=Daily+limit+reached")
	}

	// Get article
	article, err := h.articles.Get(userID, articleID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/to-read?error=Article+not+found")
	}

	// Get existing cards for context
	existing, _ := h.articles.GetCardsForArticle(articleID)

	// Get user's custom prompt
	var customPrompt string
	h.db.QueryRow("SELECT flashcard_prompt FROM users WHERE id = ?", userID).Scan(&customPrompt)

	// Generate flashcards
	pairs, err := h.gemini.GenerateFlashcards(article.Content, existing, count, customPrompt)
	if err != nil {
		if c.Request().Header.Get("HX-Request") == "true" {
			return c.String(http.StatusInternalServerError, fmt.Sprintf("Generation failed: %s", err.Error()))
		}
		return c.Redirect(http.StatusSeeOther, "/to-read?error=Generation+failed")
	}

	// Ensure Reading List deck exists
	deckID, err := h.articles.EnsureReadingDeck(userID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/to-read?error=Could+not+create+deck")
	}

	// Bulk create cards
	created, err := h.cards.CreateBatch(deckID, &articleID, pairs)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/to-read?error=Could+not+save+cards")
	}

	// For HTMX requests, return the updated row
	if c.Request().Header.Get("HX-Request") == "true" {
		// Re-fetch article to get updated flashcard count
		articles, _ := h.articles.List(userID)
		for _, a := range articles {
			if a.ID == articleID {
				return h.tmpl.ExecuteTemplate(c.Response(), "article_row_partial.html", map[string]interface{}{
					"Article":       a,
					"GeminiEnabled": h.gemini.IsConfigured(),
					"Message":       fmt.Sprintf("%d cards created", created),
				})
			}
		}
	}

	return c.Redirect(http.StatusSeeOther, fmt.Sprintf("/to-read?success=Generated+%d+flashcards", created))
}

func (h *ArticleHandler) ArticleImages(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articleID := c.Param("id")

	article, err := h.articles.Get(userID, articleID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "article not found"})
	}

	if !services.IsWikipediaURL(article.URL) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "not a Wikipedia article"})
	}

	images, err := h.wikipedia.GetArticleImages(article.URL)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"article": article,
		"images":  images,
	})
}

func (h *ArticleHandler) ImageViewer(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articleID := c.Param("id")

	article, err := h.articles.Get(userID, articleID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/to-read")
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "image_viewer.html", map[string]interface{}{
		"Article": article,
		"Email":         c.Get(middleware.EmailKey),
		"IsAdmin":       middleware.IsAdmin(c),
	})
}
