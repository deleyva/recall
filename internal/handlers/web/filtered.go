package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/models"
	"github.com/deleyva/recall/internal/services"
	"github.com/labstack/echo/v4"
)

// The filter of the session in progress. It lives in the session rather than in
// the URL because the study partials are shared with deck sessions and build
// their URLs by path; threading a query string through every one of them would
// change the markup a deck session serves, which ISC-42 forbids.
const (
	filterTagKey       = "filter_tag"
	filterMinLapsesKey = "filter_min_lapses"
	filterNoReschedKey = "filter_no_resched"
	filterCursorKey    = "filter_cursor"
)

// filteredStudyBase is where a cross-deck session posts. Deck sessions leave
// StudyBase empty and keep their own routes.
const filteredStudyBase = "/study"

func (h *ReviewHandler) saveFilter(c echo.Context, f services.StudyFilter) {
	sess, err := h.store.Get(c.Request(), middleware.SessionName)
	if err != nil {
		return
	}
	sess.Values[filterTagKey] = f.TagKey
	sess.Values[filterMinLapsesKey] = f.MinLapses
	sess.Values[filterNoReschedKey] = f.NoReschedule
	sess.Values[filterCursorKey] = "" // a new session starts at the beginning
	_ = sess.Save(c.Request(), c.Response())
}

func (h *ReviewHandler) loadFilter(c echo.Context) services.StudyFilter {
	var f services.StudyFilter
	sess, err := h.store.Get(c.Request(), middleware.SessionName)
	if err != nil {
		return f
	}
	if v, ok := sess.Values[filterTagKey].(string); ok {
		f.TagKey = v
	}
	if v, ok := sess.Values[filterMinLapsesKey].(int); ok {
		f.MinLapses = v
	}
	if v, ok := sess.Values[filterNoReschedKey].(bool); ok {
		f.NoReschedule = v
	}
	if v, ok := sess.Values[filterCursorKey].(string); ok {
		f.AfterID = v
	}
	return f
}

// advanceCursor moves the no-reschedule pass past a card. It is the only state
// that mode keeps, and it is deliberately not a growing set of ids: a cookie
// that grows with the session is a session that breaks at some length nobody
// tested.
func (h *ReviewHandler) advanceCursor(c echo.Context, cardID string) {
	sess, err := h.store.Get(c.Request(), middleware.SessionName)
	if err != nil {
		return
	}
	sess.Values[filterCursorKey] = cardID
	_ = sess.Save(c.Request(), c.Response())
}

func (h *ReviewHandler) filteredData(card interface{}, dueCount int, f services.StudyFilter) map[string]interface{} {
	return map[string]interface{}{
		"Card":         card,
		"DueCount":     dueCount,
		"StudyBase":    filteredStudyBase,
		"DeckID":       "",
		"Filter":       f,
		"NoReschedule": f.NoReschedule,
	}
}

// FilteredStudyPage builds a session over every deck the user owns. With no
// filter in the query it renders the picker instead, which is also where the
// no-reschedule choice is made.
func (h *ReviewHandler) FilteredStudyPage(c echo.Context) error {
	userID := middleware.GetUserID(c)

	tagKey := c.QueryParam("tag")
	minLapses, _ := strconv.Atoi(c.QueryParam("min_lapses"))
	f := services.StudyFilter{
		TagKey:       tagKey,
		MinLapses:    minLapses,
		NoReschedule: c.QueryParam("no_reschedule") == "1",
	}

	// The vocabulary is optional wiring; without it a session can still be
	// built on a lapse floor, it just cannot offer topics.
	var tags []models.Tag
	if h.tags != nil {
		var err error
		if tags, err = h.tags.ListForUser(userID); err != nil {
			return err
		}
	}

	if !f.Active() {
		return h.tmpl.ExecuteTemplate(c.Response(), "study_filtered.html", map[string]interface{}{
			"Tags":    tags,
			"Picker":  true,
			"Email":   c.Get(middleware.EmailKey),
			"IsAdmin": middleware.IsAdmin(c),
		})
	}

	h.saveFilter(c, f)
	card, dueCount, err := h.reviews.GetNextFiltered(userID, f)
	if err != nil {
		return err
	}

	data := map[string]interface{}{
		"Tags":         tags,
		"Filter":       f,
		"DueCount":     dueCount,
		"StudyBase":    filteredStudyBase,
		"NoReschedule": f.NoReschedule,
		"Email":        c.Get(middleware.EmailKey),
		"IsAdmin":      middleware.IsAdmin(c),
	}
	if card == nil {
		data["Done"] = true
	} else {
		data["Card"] = card
	}

	if c.Request().Header.Get("HX-Request") == "true" {
		if card == nil {
			return h.tmpl.ExecuteTemplate(c.Response(), "study_done_partial.html", data)
		}
		return h.tmpl.ExecuteTemplate(c.Response(), "study_card_partial.html", data)
	}
	return h.tmpl.ExecuteTemplate(c.Response(), "study_filtered.html", data)
}

func (h *ReviewHandler) renderNextFiltered(c echo.Context, userID string, f services.StudyFilter) error {
	card, dueCount, err := h.reviews.GetNextFiltered(userID, f)
	if err != nil {
		return err
	}
	data := h.filteredData(card, dueCount, f)
	if card == nil {
		data["Done"] = true
		return h.tmpl.ExecuteTemplate(c.Response(), "study_done_partial.html", data)
	}
	return h.tmpl.ExecuteTemplate(c.Response(), "study_card_partial.html", data)
}

// FilteredShowAnswer and FilteredSubmitAnswer mirror the deck routes, including
// the production-card reveal gate: a filtered session is a different selection
// of cards, not a different set of rules about them.
func (h *ReviewHandler) FilteredShowAnswer(c echo.Context) error {
	userID := middleware.GetUserID(c)
	card, err := h.cards.GetForUser(c.Param("cardID"), userID)
	if err != nil {
		return err
	}
	if blocked, err := h.blockedAsProduction(c, card); blocked {
		return err
	}
	return h.renderFilteredAnswer(c, card, nil)
}

func (h *ReviewHandler) FilteredSubmitAnswer(c echo.Context) error {
	userID := middleware.GetUserID(c)
	card, err := h.cards.GetForUser(c.Param("cardID"), userID)
	if err != nil {
		return err
	}
	check := services.CheckAnswer(c.FormValue("answer"), card.Back)
	h.markRevealed(c, card.ID)
	return h.renderFilteredAnswer(c, card, &check)
}

func (h *ReviewHandler) renderFilteredAnswer(c echo.Context, card *models.Card, check *services.AnswerCheck) error {
	f := h.loadFilter(c)
	return h.tmpl.ExecuteTemplate(c.Response(), "study_answer_partial.html", map[string]interface{}{
		"Card":         card,
		"DeckID":       "",
		"StudyBase":    filteredStudyBase,
		"Intervals":    h.scheduler.PreviewIntervals(*card, time.Now().UTC()),
		"Check":        check,
		"Filter":       f,
		"NoReschedule": f.NoReschedule,
	})
}

// FilteredSubmitReview grades a card inside a cross-deck session. In
// no-reschedule mode it writes nothing at all — not the FSRS columns, and not a
// review log, because a log entry with no scheduling behind it would feed
// `recall metrics` cram reviews and corrupt the instrument the outcome claims
// are measured with.
func (h *ReviewHandler) FilteredSubmitReview(c echo.Context) error {
	userID := middleware.GetUserID(c)
	f := h.loadFilter(c)

	rating, err := strconv.Atoi(c.FormValue("rating"))
	if err != nil || rating < 1 || rating > 4 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid rating"})
	}

	cardID := c.FormValue("card_id")
	card, err := h.cards.GetForUser(cardID, userID)
	if err != nil {
		return err
	}

	if !f.NoReschedule {
		now := time.Now().UTC()
		updated, log := h.scheduler.Schedule(*card, rating, now)
		if err := h.cards.UpdateFSRS(&updated); err != nil {
			return err
		}
		h.reviews.CreateLog(log.CardID, log.Rating, log.ScheduledDays, log.ElapsedDays, log.State)
		if _, err := h.cards.BurySiblings(cardID, userID, now, time.Local); err != nil {
			c.Logger().Errorf("bury siblings for card %s: %v", cardID, err)
		}
	} else {
		// Nothing is written, so the card stays due and would be served again
		// forever. Park it for this pass only, in memory of the session.
		h.advanceCursor(c, cardID)
		f.AfterID = cardID
	}

	h.clearRevealed(c)
	return h.renderNextFiltered(c, userID, f)
}

// FilteredDeleteCard and FilteredEditCard keep the answer view's controls
// working in a cross-deck session.
func (h *ReviewHandler) FilteredDeleteCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	if err := h.cards.DeleteForUser(c.Param("cardID"), userID); err != nil {
		return err
	}
	return h.renderNextFiltered(c, userID, h.loadFilter(c))
}

func (h *ReviewHandler) FilteredEditCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	card, err := h.cards.GetForUser(c.Param("cardID"), userID)
	if err != nil {
		return err
	}
	if blocked, err := h.blockedAsProduction(c, card); blocked {
		return err
	}
	return h.tmpl.ExecuteTemplate(c.Response(), "study_edit_partial.html", map[string]interface{}{
		"Card":      card,
		"DeckID":    "",
		"StudyBase": filteredStudyBase,
	})
}

func (h *ReviewHandler) FilteredUpdateCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	cardID := c.Param("cardID")
	if err := h.cards.UpdateForUser(cardID, userID, c.FormValue("front"), c.FormValue("back")); err != nil {
		return err
	}
	if kind := c.FormValue("kind"); services.ValidCardKind(kind) {
		if err := h.cards.SetKindForUser(cardID, userID, kind); err != nil {
			return err
		}
	}
	card, err := h.cards.GetForUser(cardID, userID)
	if err != nil {
		return err
	}
	return h.renderFilteredAnswer(c, card, nil)
}
