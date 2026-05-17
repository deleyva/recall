package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/models"
	"github.com/deleyva/recall/internal/scheduler"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/labstack/echo/v4"
)

type ReviewHandler struct {
	reviews   *services.ReviewService
	cards     *services.CardService
	decks     *services.DeckService
	scheduler *scheduler.Scheduler
	tmpl      *templates.Registry
}

func NewReviewHandler(reviews *services.ReviewService, cards *services.CardService, decks *services.DeckService, sched *scheduler.Scheduler, tmpl *templates.Registry) *ReviewHandler {
	return &ReviewHandler{
		reviews:   reviews,
		cards:     cards,
		decks:     decks,
		scheduler: sched,
		tmpl:      tmpl,
	}
}

func (h *ReviewHandler) StudyPage(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")

	deck, err := h.decks.Get(userID, deckID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	card, dueCount, err := h.reviews.GetNextDue(deckID)
	if err != nil {
		return err
	}

	data := map[string]interface{}{
		"Deck":     deck,
		"DueCount": dueCount,
		"Email":         c.Get(middleware.EmailKey),
		"IsAdmin":       middleware.IsAdmin(c),
	}

	if card == nil {
		data["Done"] = true
	} else {
		data["Card"] = card
	}

	// Check if HTMX request
	if c.Request().Header.Get("HX-Request") == "true" {
		if card == nil {
			return h.tmpl.ExecuteTemplate(c.Response(), "study_done_partial.html", data)
		}
		return h.tmpl.ExecuteTemplate(c.Response(), "study_card_partial.html", data)
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "study.html", data)
}

// renderAnswerPartial renders the answer view for a card with rating interval previews.
func (h *ReviewHandler) renderAnswerPartial(w http.ResponseWriter, card *models.Card, deckID string) error {
	intervals := h.scheduler.PreviewIntervals(*card, time.Now().UTC())
	return h.tmpl.ExecuteTemplate(w, "study_answer_partial.html", map[string]interface{}{
		"Card":      card,
		"DeckID":    deckID,
		"Intervals": intervals,
	})
}

// renderNextCardOrDone fetches the next due card and renders either the card or done partial.
func (h *ReviewHandler) renderNextCardOrDone(w http.ResponseWriter, deckID string) error {
	nextCard, dueCount, _ := h.reviews.GetNextDue(deckID)
	data := map[string]interface{}{
		"Deck":     map[string]string{"ID": deckID},
		"DueCount": dueCount,
	}
	if nextCard == nil {
		data["Done"] = true
		return h.tmpl.ExecuteTemplate(w, "study_done_partial.html", data)
	}
	data["Card"] = nextCard
	return h.tmpl.ExecuteTemplate(w, "study_card_partial.html", data)
}

func (h *ReviewHandler) ShowAnswer(c echo.Context) error {
	userID := middleware.GetUserID(c)
	cardID := c.Param("cardID")
	deckID := c.Param("id")

	card, err := h.cards.GetForUser(cardID, userID)
	if err != nil {
		return err
	}

	return h.renderAnswerPartial(c.Response(), card, deckID)
}

func (h *ReviewHandler) SubmitReview(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")
	cardID := c.FormValue("card_id")
	ratingStr := c.FormValue("rating")

	// Verify deck ownership
	if _, err := h.decks.Get(userID, deckID); err != nil {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	}

	rating, err := strconv.Atoi(ratingStr)
	if err != nil || rating < 1 || rating > 4 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid rating"})
	}

	card, err := h.cards.Get(cardID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	updatedCard, reviewLog := h.scheduler.Schedule(*card, rating, now)

	// Save updated card
	if err := h.cards.UpdateFSRS(&updatedCard); err != nil {
		return err
	}

	// Save review log
	h.reviews.CreateLog(reviewLog.CardID, reviewLog.Rating, reviewLog.ScheduledDays, reviewLog.ElapsedDays, reviewLog.State)

	return h.renderNextCardOrDone(c.Response(), deckID)
}

func (h *ReviewHandler) StudyEditCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	cardID := c.Param("cardID")
	deckID := c.Param("id")

	card, err := h.cards.GetForUser(cardID, userID)
	if err != nil {
		return err
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "study_edit_partial.html", map[string]interface{}{
		"Card":   card,
		"DeckID": deckID,
	})
}

func (h *ReviewHandler) StudyUpdateCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	cardID := c.Param("cardID")
	deckID := c.Param("id")

	front := c.FormValue("front")
	back := c.FormValue("back")

	if err := h.cards.UpdateForUser(cardID, userID, front, back); err != nil {
		return err
	}

	// Re-fetch card and show answer again
	card, err := h.cards.GetForUser(cardID, userID)
	if err != nil {
		return err
	}

	return h.renderAnswerPartial(c.Response(), card, deckID)
}

func (h *ReviewHandler) StudyDeleteCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	cardID := c.Param("cardID")
	deckID := c.Param("id")

	if err := h.cards.DeleteForUser(cardID, userID); err != nil {
		return err
	}

	return h.renderNextCardOrDone(c.Response(), deckID)
}

func (h *ReviewHandler) StatsPage(c echo.Context) error {
	userID := middleware.GetUserID(c)

	stats, _ := h.reviews.GetStats(userID)
	history, _ := h.reviews.GetHistory(userID)

	return h.tmpl.ExecuteTemplate(c.Response(), "stats.html", map[string]interface{}{
		"Stats":   stats,
		"History": history,
		"Email":         c.Get(middleware.EmailKey),
		"IsAdmin":       middleware.IsAdmin(c),
	})
}
