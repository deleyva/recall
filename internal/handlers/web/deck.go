package web

import (
	"net/http"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/labstack/echo/v4"
)

type DeckHandler struct {
	decks   *services.DeckService
	reviews *services.ReviewService
	tmpl    *templates.Registry
}

func NewDeckHandler(decks *services.DeckService, reviews *services.ReviewService, tmpl *templates.Registry) *DeckHandler {
	return &DeckHandler{decks: decks, reviews: reviews, tmpl: tmpl}
}

func (h *DeckHandler) Dashboard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	decks, err := h.decks.List(userID)
	if err != nil {
		return err
	}

	stats, _ := h.reviews.GetStats(userID)

	return h.tmpl.ExecuteTemplate(c.Response(), "dashboard.html", map[string]interface{}{
		"Decks": decks,
		"Stats": stats,
		"Email":         c.Get(middleware.EmailKey),
		"IsAdmin":       middleware.IsAdmin(c),
	})
}

func (h *DeckHandler) NewDeckPage(c echo.Context) error {
	return h.tmpl.ExecuteTemplate(c.Response(), "deck_new.html", nil)
}

func (h *DeckHandler) CreateDeck(c echo.Context) error {
	userID := middleware.GetUserID(c)
	name := c.FormValue("name")
	description := c.FormValue("description")

	if name == "" {
		return c.Redirect(http.StatusSeeOther, "/decks/new?error=Name+required")
	}

	_, err := h.decks.Create(userID, name, description)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/decks/new?error=Could+not+create+deck")
	}

	return c.Redirect(http.StatusSeeOther, "/")
}

func (h *DeckHandler) ViewDeck(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")

	deck, err := h.decks.Get(userID, deckID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "deck_view.html", map[string]interface{}{
		"Deck":  deck,
		"Email":         c.Get(middleware.EmailKey),
		"IsAdmin":       middleware.IsAdmin(c),
	})
}

func (h *DeckHandler) EditDeckPage(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")

	deck, err := h.decks.Get(userID, deckID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "deck_edit.html", map[string]interface{}{
		"Deck": deck,
	})
}

func (h *DeckHandler) UpdateDeck(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")
	name := c.FormValue("name")
	description := c.FormValue("description")

	h.decks.Update(userID, deckID, name, description)
	return c.Redirect(http.StatusSeeOther, "/decks/"+deckID)
}

func (h *DeckHandler) DeleteDeck(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")

	h.decks.Delete(userID, deckID)
	return c.Redirect(http.StatusSeeOther, "/")
}
