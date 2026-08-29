package web

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
)

// newCardHandler reuses the review harness for its database and fixtures, so a
// card edited here is the same card the study routes see.
func newCardHandler(t *testing.T) (*CardHandler, *sql.DB) {
	t.Helper()

	_, db := newReviewHandler(t)
	tmpl := templates.NewRegistry()
	if err := tmpl.Load("../../../templates"); err != nil {
		t.Fatalf("load templates: %v", err)
	}
	return NewCardHandler(services.NewCardService(db), services.NewDeckService(db), tmpl), db
}

// The deck's own edit page carries the kind control, with the card's current
// kind selected — the study session is no longer the only place to switch one.
func TestEditCardPageOffersTheKindControl(t *testing.T) {
	h, _ := newCardHandler(t)

	c, rec := call(t, http.MethodGet, "/decks/"+testDeckID+"/cards/"+productionID+"/edit", nil, nil)
	if err := h.EditCardPage(withCardParams(c, productionID)); err != nil {
		t.Fatalf("edit page: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `name="kind"`) {
		t.Error("edit page has no kind control")
	}
	if !strings.Contains(body, `value="production" selected`) {
		t.Errorf("production card does not pre-select production; body: %s", body)
	}
}

// Saving from the deck's edit page switches the card over and leaves every FSRS
// column where it was.
func TestUpdateCardSwitchesKindWithoutTouchingTheSchedule(t *testing.T) {
	h, db := newCardHandler(t)

	var dueBefore string
	var repsBefore, stateBefore int
	var stabilityBefore float64
	row := db.QueryRow("SELECT due, reps, state, stability FROM cards WHERE id = ?", recognitionI)
	if err := row.Scan(&dueBefore, &repsBefore, &stateBefore, &stabilityBefore); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	c, _ := call(t, http.MethodPost, "/decks/"+testDeckID+"/cards/"+recognitionI,
		url.Values{"front": {testFront}, "back": {testBack}, "kind": {"production"}}, nil)
	if err := h.UpdateCard(withCardParams(c, recognitionI)); err != nil {
		t.Fatalf("update card: %v", err)
	}

	var kind, dueAfter string
	var repsAfter, stateAfter int
	var stabilityAfter float64
	row = db.QueryRow("SELECT kind, due, reps, state, stability FROM cards WHERE id = ?", recognitionI)
	if err := row.Scan(&kind, &dueAfter, &repsAfter, &stateAfter, &stabilityAfter); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if kind != services.KindProduction {
		t.Errorf("kind = %q, want production", kind)
	}
	if dueAfter != dueBefore || repsAfter != repsBefore || stateAfter != stateBefore || stabilityAfter != stabilityBefore {
		t.Errorf("schedule moved: %s/%d/%d/%v → %s/%d/%d/%v",
			dueBefore, repsBefore, stateBefore, stabilityBefore,
			dueAfter, repsAfter, stateAfter, stabilityAfter)
	}
}

// A save that carries no kind — an older form, or a client that only edits the
// text — leaves the card asked exactly as it was.
func TestUpdateCardWithoutKindLeavesItAlone(t *testing.T) {
	h, db := newCardHandler(t)

	c, _ := call(t, http.MethodPost, "/decks/"+testDeckID+"/cards/"+productionID,
		url.Values{"front": {testFront}, "back": {"edited"}}, nil)
	if err := h.UpdateCard(withCardParams(c, productionID)); err != nil {
		t.Fatalf("update card: %v", err)
	}

	var kind, back string
	if err := db.QueryRow("SELECT kind, back FROM cards WHERE id = ?", productionID).Scan(&kind, &back); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if kind != services.KindProduction {
		t.Errorf("kind = %q, want it unchanged at production", kind)
	}
	if back != "edited" {
		t.Errorf("back = %q, want the edit to have landed", back)
	}
}

// An unknown kind posted by hand is ignored rather than written.
func TestUpdateCardRejectsAnUnknownKind(t *testing.T) {
	h, db := newCardHandler(t)

	c, _ := call(t, http.MethodPost, "/decks/"+testDeckID+"/cards/"+recognitionI,
		url.Values{"front": {testFront}, "back": {testBack}, "kind": {"cloze"}}, nil)
	if err := h.UpdateCard(withCardParams(c, recognitionI)); err != nil {
		t.Fatalf("update card: %v", err)
	}

	var kind string
	if err := db.QueryRow("SELECT kind FROM cards WHERE id = ?", recognitionI).Scan(&kind); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if kind != services.KindRecognition {
		t.Errorf("kind = %q, want it unchanged at recognition", kind)
	}
}

// A card added by hand is created with the kind the form asked for, rather than
// always landing as recognition and needing a second edit.
func TestCreateCardHonoursTheChosenKind(t *testing.T) {
	h, db := newCardHandler(t)

	c, _ := call(t, http.MethodPost, "/decks/"+testDeckID+"/cards",
		url.Values{"front": {"¿Quién escribió el Traité?"}, "back": {"Lavoisier"}, "kind": {"production"}}, nil)
	c.SetParamNames("id")
	c.SetParamValues(testDeckID)
	if err := h.CreateCard(c); err != nil {
		t.Fatalf("create card: %v", err)
	}

	var kind string
	if err := db.QueryRow("SELECT kind FROM cards WHERE front = ?", "¿Quién escribió el Traité?").Scan(&kind); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if kind != services.KindProduction {
		t.Errorf("kind = %q, want production", kind)
	}
}
