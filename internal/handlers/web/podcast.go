package web

import (
	"net/http"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/labstack/echo/v4"
)

type PodcastHandler struct {
	podcasts *services.PodcastService
	articles *services.ArticleService
	tmpl     *templates.Registry
}

func NewPodcastHandler(podcasts *services.PodcastService, articles *services.ArticleService, tmpl *templates.Registry) *PodcastHandler {
	return &PodcastHandler{podcasts: podcasts, articles: articles, tmpl: tmpl}
}

func (h *PodcastHandler) ListPage(c echo.Context) error {
	if !middleware.IsAdmin(c) {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	userID := middleware.GetUserID(c)

	podcasts, err := h.podcasts.List(userID)
	if err != nil {
		podcasts = nil
	}

	articles, err := h.articles.List(userID)
	if err != nil {
		articles = nil
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "podcasts.html", map[string]interface{}{
		"Podcasts": podcasts,
		"Articles": articles,
		"Email":    c.Get(middleware.EmailKey),
		"IsAdmin":  middleware.IsAdmin(c),
		"Error":    c.QueryParam("error"),
		"Success":  c.QueryParam("success"),
	})
}

func (h *PodcastHandler) CreatePodcast(c echo.Context) error {
	if !middleware.IsAdmin(c) {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	userID := middleware.GetUserID(c)

	title := c.FormValue("title")
	if title == "" {
		title = "Daily Podcast"
	}

	articleIDs := c.Request().Form["article_ids"]
	if len(articleIDs) == 0 {
		return c.Redirect(http.StatusSeeOther, "/podcasts?error=Select+at+least+one+article")
	}

	_, err := h.podcasts.Create(userID, title, articleIDs)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/podcasts?error=Failed+to+create+podcast")
	}

	return c.Redirect(http.StatusSeeOther, "/podcasts?success=Podcast+queued+for+generation")
}

func (h *PodcastHandler) DeletePodcast(c echo.Context) error {
	if !middleware.IsAdmin(c) {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	userID := middleware.GetUserID(c)
	podcastID := c.Param("id")

	if err := h.podcasts.Delete(userID, podcastID); err != nil {
		return c.Redirect(http.StatusSeeOther, "/podcasts?error=Failed+to+delete+podcast")
	}

	return c.Redirect(http.StatusSeeOther, "/podcasts?success=Podcast+deleted")
}
