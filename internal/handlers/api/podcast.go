package api

import (
	"net/http"
	"strings"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/models"
	"github.com/labstack/echo/v4"
)

func (h *Handler) ListPodcasts(c echo.Context) error {
	userID := middleware.GetUserID(c)
	podcasts, err := h.podcasts.List(userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if podcasts == nil {
		podcasts = []models.Podcast{}
	}
	return c.JSON(http.StatusOK, podcasts)
}

func (h *Handler) CreatePodcast(c echo.Context) error {
	userID := middleware.GetUserID(c)
	var req struct {
		Title      string   `json:"title"`
		ArticleIDs []string `json:"article_ids"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if len(req.ArticleIDs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "article_ids required"})
	}
	if strings.TrimSpace(req.Title) == "" {
		req.Title = "Recall podcast"
	}

	// Only the caller's own articles may go into a podcast.
	for _, id := range req.ArticleIDs {
		if _, err := h.articles.Get(userID, id); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown article: " + id})
		}
	}

	podcast, err := h.podcasts.Create(userID, req.Title, req.ArticleIDs)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, podcast)
}

func (h *Handler) GetPodcast(c echo.Context) error {
	userID := middleware.GetUserID(c)
	podcast, err := h.podcasts.Get(userID, c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "podcast not found"})
	}
	return c.JSON(http.StatusOK, podcast)
}

func (h *Handler) DeletePodcast(c echo.Context) error {
	userID := middleware.GetUserID(c)
	if err := h.podcasts.Delete(userID, c.Param("id")); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "podcast not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "deleted"})
}

// GetPodcastContent returns the concatenated article text behind a podcast —
// what the NotebookLM worker feeds in to produce the audio.
func (h *Handler) GetPodcastContent(c echo.Context) error {
	userID := middleware.GetUserID(c)
	podcastID := c.Param("id")

	if _, err := h.podcasts.Get(userID, podcastID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "podcast not found"})
	}

	content, err := h.podcasts.GetArticleContent(podcastID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(content))
}
