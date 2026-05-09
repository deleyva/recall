package web

import (
	"net/http"
	"strconv"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/labstack/echo/v4"
)

type CardHandler struct {
	cards *services.CardService
	decks *services.DeckService
	tmpl  *templates.Registry
}

func NewCardHandler(cards *services.CardService, decks *services.DeckService, tmpl *templates.Registry) *CardHandler {
	return &CardHandler{cards: cards, decks: decks, tmpl: tmpl}
}

func (h *CardHandler) ListCards(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")

	deck, err := h.decks.Get(userID, deckID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}

	cards, total, err := h.cards.List(deckID, page, 50)
	if err != nil {
		return err
	}

	totalPages := (total + 49) / 50

	return h.tmpl.ExecuteTemplate(c.Response(), "cards_list.html", map[string]interface{}{
		"Deck":       deck,
		"Cards":      cards,
		"Page":       page,
		"TotalPages": totalPages,
		"Total":      total,
		"Email":         c.Get(middleware.EmailKey),
		"IsAdmin":       middleware.IsAdmin(c),
	})
}

func (h *CardHandler) NewCardPage(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")

	deck, err := h.decks.Get(userID, deckID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "card_new.html", map[string]interface{}{
		"Deck": deck,
	})
}

func (h *CardHandler) CreateCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")

	// Verify deck ownership
	if _, err := h.decks.Get(userID, deckID); err != nil {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	front := c.FormValue("front")
	back := c.FormValue("back")

	if front == "" || back == "" {
		return c.Redirect(http.StatusSeeOther, "/decks/"+deckID+"/cards/new?error=Front+and+back+required")
	}

	h.cards.Create(deckID, front, back, nil)

	// If addAnother flag is set, stay on the new card page
	if c.FormValue("add_another") == "true" {
		return c.Redirect(http.StatusSeeOther, "/decks/"+deckID+"/cards/new?success=Card+added")
	}

	return c.Redirect(http.StatusSeeOther, "/decks/"+deckID+"/cards")
}

func (h *CardHandler) EditCardPage(c echo.Context) error {
	userID := middleware.GetUserID(c)
	cardID := c.Param("cardID")

	card, err := h.cards.GetForUser(cardID, userID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "card_edit.html", map[string]interface{}{
		"Card":   card,
		"DeckID": c.Param("id"),
	})
}

func (h *CardHandler) UpdateCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	cardID := c.Param("cardID")
	deckID := c.Param("id")
	front := c.FormValue("front")
	back := c.FormValue("back")

	h.cards.UpdateForUser(cardID, userID, front, back)
	return c.Redirect(http.StatusSeeOther, "/decks/"+deckID+"/cards")
}

func (h *CardHandler) DeleteCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	cardID := c.Param("cardID")
	deckID := c.Param("id")

	h.cards.DeleteForUser(cardID, userID)
	return c.Redirect(http.StatusSeeOther, "/decks/"+deckID+"/cards")
}

func (h *CardHandler) ImportCSV(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")

	// Verify deck ownership
	if _, err := h.decks.Get(userID, deckID); err != nil {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/decks/"+deckID+"/cards?error=No+file+uploaded")
	}

	src, err := file.Open()
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/decks/"+deckID+"/cards?error=Could+not+read+file")
	}
	defer src.Close()

	count, err := h.cards.ImportCSV(deckID, src)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/decks/"+deckID+"/cards?error=Import+failed")
	}

	return c.Redirect(http.StatusSeeOther, "/decks/"+deckID+"/cards?success=Imported+"+strconv.Itoa(count)+"+cards")
}
