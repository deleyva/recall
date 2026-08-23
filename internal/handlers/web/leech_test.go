package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/labstack/echo/v4"
)

func getPage(t *testing.T, target string, render func(echo.Context) error) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.Set(middleware.UserIDKey, testUserID)
	if err := render(c); err != nil {
		t.Fatalf("render %s: %v", target, err)
	}
	return rec.Body.String()
}

// ISC-61 — the list offers all three documented remedies per card, and says
// which deck the card lives in.
func TestLeechListOffersTheThreeRemedies(t *testing.T) {
	h, db := newReviewHandler(t)
	if _, err := db.Exec("UPDATE cards SET lapses = 9 WHERE id = ?", recognitionI); err != nil {
		t.Fatalf("seed leech: %v", err)
	}

	body := getPage(t, "/leeches", h.LeechesPage)

	for _, want := range []string{
		"/decks/" + testDeckID + "/cards/" + recognitionI + "/edit", // edit
		"/leeches/" + recognitionI + "/suspend",                     // suspend
		"/leeches/" + recognitionI + "/delete",                      // delete
		"9 lapses",
		"Deck", // the deck name
	} {
		if !strings.Contains(body, want) {
			t.Errorf("leech list is missing %q:\n%s", want, body)
		}
	}
	// The card below the threshold is not on the list.
	if strings.Contains(body, "/leeches/"+productionID+"/suspend") {
		t.Error("a card under the threshold is listed as a leech")
	}
}

// ISC-61 — with nothing over the threshold the page still works and says so.
// This is the expected reading for a while: lapses only accumulate once failure
// is registered at all.
func TestLeechListIsHonestWhenEmpty(t *testing.T) {
	h, _ := newReviewHandler(t)
	body := getPage(t, "/leeches", h.LeechesPage)
	if !strings.Contains(body, "No card has reached") {
		t.Errorf("empty leech list does not explain itself:\n%s", body)
	}
}

// ISC-61 — the list is reachable from the dashboard whether or not there is
// anything on it, and says so loudly when there is.
func TestDashboardRoutesToTheLeechList(t *testing.T) {
	h, db := newReviewHandler(t)
	tmpl := templates.NewRegistry()
	if err := tmpl.Load("../../../templates"); err != nil {
		t.Fatalf("load templates: %v", err)
	}
	dh := NewDeckHandler(services.NewDeckService(db), services.NewReviewService(db), tmpl)
	_ = h

	body := getPage(t, "/", dh.Dashboard)
	if !strings.Contains(body, `href="/leeches"`) {
		t.Errorf("no route to the leech list on the dashboard:\n%s", body)
	}
	if strings.Contains(body, "keep failing —") {
		t.Error("the dashboard shouts about leeches when there are none")
	}

	if _, err := db.Exec("UPDATE cards SET lapses = 12 WHERE id = ?", recognitionI); err != nil {
		t.Fatalf("seed leech: %v", err)
	}
	body = getPage(t, "/", dh.Dashboard)
	if !strings.Contains(body, "1") || !strings.Contains(body, "card keeps failing") {
		t.Errorf("the dashboard does not surface the leech count:\n%s", body)
	}
}

// ISC-60 — the flag is visible on the card during study, both before the answer
// is revealed and after.
func TestLeechFlagIsVisibleDuringStudy(t *testing.T) {
	h, db := newReviewHandler(t)
	if _, err := db.Exec(
		"UPDATE cards SET lapses = 9 WHERE id = ?; ", recognitionI); err != nil {
		t.Fatalf("seed leech: %v", err)
	}
	if _, err := db.Exec("UPDATE cards SET due = '2030-01-01T00:00:00Z' WHERE id = ?", productionID); err != nil {
		t.Fatalf("park the other card: %v", err)
	}

	c, rec := call(t, http.MethodGet, "/decks/"+testDeckID+"/study", nil, nil)
	c.SetParamNames("id")
	c.SetParamValues(testDeckID)
	if err := h.StudyPage(c); err != nil {
		t.Fatalf("study page: %v", err)
	}
	if !strings.Contains(rec.Body.String(), "failed 9 times") {
		t.Errorf("no leech flag on the card being studied:\n%s", rec.Body.String())
	}

	c2, rec2 := call(t, http.MethodGet, "/decks/"+testDeckID+"/study/"+recognitionI+"/answer", nil, nil)
	if err := h.ShowAnswer(withCardParams(c2, recognitionI)); err != nil {
		t.Fatalf("show answer: %v", err)
	}
	if !strings.Contains(rec2.Body.String(), "failed 9 times") {
		t.Errorf("no leech flag on the answer view:\n%s", rec2.Body.String())
	}
}

// ISC-62 — the suspend and delete routes stop at the owner.
func TestLeechRoutesStopAtTheOwner(t *testing.T) {
	h, db := newReviewHandler(t)
	if _, err := db.Exec(`
		INSERT INTO users (id, email, password_hash) VALUES ('u2', 'u2@example.com', 'x');
		INSERT INTO decks (id, user_id, name, description) VALUES ('d2', 'u2', 'Theirs', '');
		INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days,
			scheduled_days, reps, lapses, state, last_review, created_at, updated_at)
		VALUES ('foreign', 'd2', 'f', 'b', '2026-01-01T00:00:00Z', 0, 0, 0, 0, 0, 9, 2,
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');`); err != nil {
		t.Fatalf("seed other user: %v", err)
	}

	c, _ := call(t, http.MethodPost, "/leeches/foreign/suspend", nil, nil)
	c.SetParamNames("cardID")
	c.SetParamValues("foreign")
	if err := h.SuspendLeech(c); err == nil {
		t.Error("suspended another user's card")
	}

	c2, _ := call(t, http.MethodPost, "/leeches/foreign/delete", nil, nil)
	c2.SetParamNames("cardID")
	c2.SetParamValues("foreign")
	if err := h.DeleteLeech(c2); err == nil {
		t.Error("deleted another user's card")
	}

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM cards WHERE id = 'foreign' AND suspended = 0").Scan(&n); err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if n != 1 {
		t.Error("another user's card was modified or removed")
	}
}
