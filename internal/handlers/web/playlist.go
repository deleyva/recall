package web

import (
	"net/http"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/labstack/echo/v4"
)

type PlaylistHandler struct {
	playlists *services.PlaylistService
	articles  *services.ArticleService
	decks     *services.DeckService
	tmpl      *templates.Registry
}

func NewPlaylistHandler(playlists *services.PlaylistService, articles *services.ArticleService, decks *services.DeckService, tmpl *templates.Registry) *PlaylistHandler {
	return &PlaylistHandler{playlists: playlists, articles: articles, decks: decks, tmpl: tmpl}
}

func (h *PlaylistHandler) ListPage(c echo.Context) error {
	userID := middleware.GetUserID(c)

	playlists, err := h.playlists.List(userID)
	if err != nil {
		playlists = nil
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "playlists.html", map[string]interface{}{
		"Playlists": playlists,
		"Email":     c.Get(middleware.EmailKey),
		"IsAdmin":   middleware.IsAdmin(c),
		"Error":     c.QueryParam("error"),
		"Success":   c.QueryParam("success"),
	})
}

func (h *PlaylistHandler) DetailPage(c echo.Context) error {
	userID := middleware.GetUserID(c)
	playlistID := c.Param("id")

	playlist, err := h.playlists.Get(userID, playlistID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/playlists?error=Playlist+not+found")
	}

	// Load all articles and decks for linking dropdowns
	articles, _ := h.articles.List(userID)
	decks, _ := h.decks.List(userID)

	return h.tmpl.ExecuteTemplate(c.Response(), "playlist_detail.html", map[string]interface{}{
		"Playlist":     playlist,
		"AllArticles":  articles,
		"AllDecks":     decks,
		"Email":        c.Get(middleware.EmailKey),
		"IsAdmin":      middleware.IsAdmin(c),
		"Error":        c.QueryParam("error"),
		"Success":      c.QueryParam("success"),
	})
}

func (h *PlaylistHandler) Create(c echo.Context) error {
	userID := middleware.GetUserID(c)

	rawURL := c.FormValue("url")
	title := c.FormValue("title")
	description := c.FormValue("description")

	if rawURL == "" {
		return c.Redirect(http.StatusSeeOther, "/playlists?error=URL+is+required")
	}

	_, err := h.playlists.Create(userID, rawURL, title, description)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/playlists?error="+err.Error())
	}

	return c.Redirect(http.StatusSeeOther, "/playlists?success=Playlist+added")
}

func (h *PlaylistHandler) Delete(c echo.Context) error {
	userID := middleware.GetUserID(c)
	playlistID := c.Param("id")

	if err := h.playlists.Delete(userID, playlistID); err != nil {
		return c.Redirect(http.StatusSeeOther, "/playlists?error=Failed+to+delete+playlist")
	}

	return c.Redirect(http.StatusSeeOther, "/playlists?success=Playlist+deleted")
}

func (h *PlaylistHandler) LinkArticle(c echo.Context) error {
	userID := middleware.GetUserID(c)
	playlistID := c.Param("id")
	articleID := c.FormValue("article_id")

	if articleID == "" {
		return c.String(http.StatusBadRequest, "article_id required")
	}

	if err := h.playlists.LinkArticle(userID, playlistID, articleID); err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	return h.renderLinkedItems(c, userID, playlistID)
}

func (h *PlaylistHandler) UnlinkArticle(c echo.Context) error {
	userID := middleware.GetUserID(c)
	playlistID := c.Param("id")
	articleID := c.Param("articleID")

	if err := h.playlists.UnlinkArticle(userID, playlistID, articleID); err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	return h.renderLinkedItems(c, userID, playlistID)
}

func (h *PlaylistHandler) LinkDeck(c echo.Context) error {
	userID := middleware.GetUserID(c)
	playlistID := c.Param("id")
	deckID := c.FormValue("deck_id")

	if deckID == "" {
		return c.String(http.StatusBadRequest, "deck_id required")
	}

	if err := h.playlists.LinkDeck(userID, playlistID, deckID); err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	return h.renderLinkedItems(c, userID, playlistID)
}

func (h *PlaylistHandler) UnlinkDeck(c echo.Context) error {
	userID := middleware.GetUserID(c)
	playlistID := c.Param("id")
	deckID := c.Param("deckID")

	if err := h.playlists.UnlinkDeck(userID, playlistID, deckID); err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	return h.renderLinkedItems(c, userID, playlistID)
}

func (h *PlaylistHandler) renderLinkedItems(c echo.Context, userID, playlistID string) error {
	playlist, err := h.playlists.Get(userID, playlistID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to reload playlist")
	}

	articles, _ := h.articles.List(userID)
	decks, _ := h.decks.List(userID)

	return h.tmpl.ExecuteTemplate(c.Response(), "playlist_linked_partial.html", map[string]interface{}{
		"Playlist":    playlist,
		"AllArticles": articles,
		"AllDecks":    decks,
	})
}
