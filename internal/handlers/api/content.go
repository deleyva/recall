package api

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/models"
	"github.com/labstack/echo/v4"
)

// Health is the one unauthenticated read: enough to tell a monitor the process
// is up and the database answers.
func (h *Handler) Health(c echo.Context) error {
	status := "ok"
	if h.db != nil {
		if err := h.db.Ping(); err != nil {
			status = "degraded"
		}
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"status": status,
		"api":    "v1",
	})
}

// Search queries the full-text index across articles, flashcards and chats.
func (h *Handler) Search(c echo.Context) error {
	userID := middleware.GetUserID(c)
	query := c.QueryParam("q")

	var kinds []string
	if k := c.QueryParam("kind"); k != "" {
		kinds = strings.Split(k, ",")
	}

	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 20
	}
	offset, _ := strconv.Atoi(c.QueryParam("offset"))

	results, total, err := h.search.Search(userID, query, kinds, limit, offset)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, models.SearchResponse{
		Query:   query,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		Results: results,
	})
}

// Reindex rebuilds the search index from the source tables. Admin-only: it is a
// whole-database operation, not a per-user one.
func (h *Handler) Reindex(c echo.Context) error {
	userID := middleware.GetUserID(c)
	var isAdmin bool
	h.db.QueryRow("SELECT is_admin FROM users WHERE id = ?", userID).Scan(&isAdmin)
	if !isAdmin {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "admin only"})
	}

	count, err := h.search.Reindex()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"indexed": count})
}

// GetArticle returns one article including the stored text.
func (h *Handler) GetArticle(c echo.Context) error {
	userID := middleware.GetUserID(c)
	article, err := h.articles.Get(userID, c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "article not found"})
	}
	return c.JSON(http.StatusOK, models.ArticleDetail{Article: *article, Content: article.Content})
}

// GetArticleContent returns just the stored text, as plain text.
func (h *Handler) GetArticleContent(c echo.Context) error {
	userID := middleware.GetUserID(c)
	article, err := h.articles.Get(userID, c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "article not found"})
	}
	return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(article.Content))
}

func (h *Handler) UpdateArticle(c echo.Context) error {
	userID := middleware.GetUserID(c)
	var req struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if req.Title == "" && req.Content == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "title or content required"})
	}

	if err := h.articles.Update(userID, c.Param("id"), req.Title, req.Content); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "article not found"})
	}

	article, err := h.articles.Get(userID, c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "article not found"})
	}
	return c.JSON(http.StatusOK, models.ArticleDetail{Article: *article, Content: article.Content})
}

// ListArticleCards returns the flashcards generated from one article.
func (h *Handler) ListArticleCards(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articleID := c.Param("id")

	if _, err := h.articles.Get(userID, articleID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "article not found"})
	}

	cards, err := h.articles.GetCardsForArticle(articleID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if cards == nil {
		cards = []models.Card{}
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"cards": cards, "total": len(cards)})
}

// ListAllCards returns cards across every deck the user owns.
func (h *Handler) ListAllCards(c echo.Context) error {
	userID := middleware.GetUserID(c)
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	offset, _ := strconv.Atoi(c.QueryParam("offset"))

	cards, total, err := h.cards.ListForUser(
		userID,
		c.QueryParam("deck_id"),
		c.QueryParam("article_id"),
		c.QueryParam("due") == "true",
		limit, offset,
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"cards": cards, "total": total})
}

// Chat

func (h *Handler) ListChat(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articleID := c.Param("id")

	if _, err := h.articles.Get(userID, articleID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "article not found"})
	}

	messages, err := h.chat.ListByArticle(articleID, userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if messages == nil {
		messages = []models.ChatMessage{}
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"messages": messages, "total": len(messages)})
}

func (h *Handler) SendChat(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articleID := c.Param("id")

	if !h.llm.IsConfigured() {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "LLM not configured"})
	}

	var req struct {
		Message string `json:"message"`
	}
	if err := c.Bind(&req); err != nil || strings.TrimSpace(req.Message) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "message required"})
	}

	article, err := h.articles.Get(userID, articleID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "article not found"})
	}

	history, err := h.chat.ListByArticle(articleID, userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	userMsg, err := h.chat.Create(articleID, userID, models.RoleUser, req.Message)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	answer, err := h.llm.ChatWithArticle(article.Content, history, req.Message)
	if err != nil {
		log.Printf("api chat error for article %s: %v", articleID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "llm request failed"})
	}

	assistantMsg, err := h.chat.Create(articleID, userID, models.RoleAssistant, answer)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"question": userMsg,
		"answer":   assistantMsg,
	})
}

func (h *Handler) ClearChat(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articleID := c.Param("id")

	if _, err := h.articles.Get(userID, articleID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "article not found"})
	}
	if err := h.chat.DeleteByArticle(articleID, userID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "cleared"})
}
