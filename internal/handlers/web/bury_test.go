package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/labstack/echo/v4"
)

const (
	siblingA = "c-sibling-a"
	siblingB = "c-sibling-b"
)

// seedSiblings gives the deck two cards generated from one article, both due,
// and parks the fixture's own cards out of the way so the queue is unambiguous.
func seedSiblings(t *testing.T, db *sql.DB) {
	t.Helper()
	exec := func(q string, args ...interface{}) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	exec("UPDATE cards SET due = '2030-01-01T00:00:00Z'")
	exec(`INSERT INTO articles (id, user_id, url, title, domain, content, created_at)
		VALUES ('art1', ?, 'https://example.com/a', 'Article', 'example.com', 'body', '2026-01-01T00:00:00Z')`, testUserID)
	due := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	for _, id := range []string{siblingA, siblingB} {
		exec(`INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days,
			scheduled_days, reps, lapses, state, last_review, created_at, updated_at, kind, article_id)
			VALUES (?, ?, ?, ?, ?, 2, 5, 1, 1, 3, 0, 2,
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', ?, 'art1')`,
			id, testDeckID, testFront, testBack, due, services.KindRecognition)
	}
}

func buriedAt(t *testing.T, db *sql.DB, id string) *string {
	t.Helper()
	var v *string
	if err := db.QueryRow("SELECT buried_until FROM cards WHERE id = ?", id).Scan(&v); err != nil {
		t.Fatalf("read buried_until for %s: %v", id, err)
	}
	return v
}

// ISC-45 — the burying happens when a card is *answered*, not when a service
// method is called. This drives the real study route the learner drives.
func TestSubmitReviewBuriesTheRestOfTheBatch(t *testing.T) {
	h, db := newReviewHandler(t)
	seedSiblings(t, db)

	c, rec := call(t, http.MethodPost, "/decks/"+testDeckID+"/study",
		url.Values{"card_id": {siblingA}, "rating": {"3"}}, nil)
	c.SetParamNames("id")
	c.SetParamValues(testDeckID)
	if err := h.SubmitReview(c); err != nil {
		t.Fatalf("submit review: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}

	if buriedAt(t, db, siblingB) == nil {
		t.Error("the sibling was not buried by answering its batch-mate")
	}
	if buriedAt(t, db, siblingA) != nil {
		t.Error("the answered card buried itself")
	}

	// And the queue agrees: the session is over rather than serving the primed
	// sibling next.
	if !strings.Contains(rec.Body.String(), "Done") && strings.Contains(rec.Body.String(), testFront) {
		t.Errorf("the primed sibling was served immediately after its batch-mate:\n%s", rec.Body.String())
	}
}

// ISC-48 — the unbury route clears the deck and sends the learner back to it.
func TestUnburyRouteClearsTheDeck(t *testing.T) {
	h, db := newReviewHandler(t)
	seedSiblings(t, db)
	future := time.Now().UTC().Add(6 * time.Hour).Format(time.RFC3339)
	if _, err := db.Exec("UPDATE cards SET buried_until = ? WHERE id = ?", future, siblingB); err != nil {
		t.Fatalf("bury: %v", err)
	}

	c, rec := call(t, http.MethodPost, "/decks/"+testDeckID+"/unbury", url.Values{}, nil)
	c.SetParamNames("id")
	c.SetParamValues(testDeckID)
	if err := h.UnburyDeck(c); err != nil {
		t.Fatalf("unbury: %v", err)
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/decks/"+testDeckID {
		t.Errorf("redirected to %q, want the deck page", got)
	}
	if buriedAt(t, db, siblingB) != nil {
		t.Error("the card is still buried after unburying the deck")
	}
}

// ISC-48 — the deck overview shows the control, and only when there is
// something buried to release.
func TestDeckPageRendersTheUnburyControlOnlyWhenNeeded(t *testing.T) {
	_, db := newReviewHandler(t)
	seedSiblings(t, db)

	tmpl := templates.NewRegistry()
	if err := tmpl.Load("../../../templates"); err != nil {
		t.Fatalf("load templates: %v", err)
	}
	decks := services.NewDeckService(db)
	h := NewDeckHandler(decks, services.NewReviewService(db), tmpl)

	render := func() string {
		req := httptest.NewRequest(http.MethodGet, "/decks/"+testDeckID, nil)
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		c.Set(middleware.UserIDKey, testUserID)
		c.SetParamNames("id")
		c.SetParamValues(testDeckID)
		if err := h.ViewDeck(c); err != nil {
			t.Fatalf("view deck: %v", err)
		}
		return rec.Body.String()
	}

	if body := render(); strings.Contains(body, "/unbury") {
		t.Error("the unbury control is offered with nothing buried")
	}

	future := time.Now().UTC().Add(6 * time.Hour).Format(time.RFC3339)
	if _, err := db.Exec("UPDATE cards SET buried_until = ? WHERE id = ?", future, siblingB); err != nil {
		t.Fatalf("bury: %v", err)
	}

	body := render()
	if !strings.Contains(body, "/decks/"+testDeckID+"/unbury") {
		t.Errorf("no unbury control on the deck page:\n%s", body)
	}
	if !strings.Contains(body, "Study (1)") {
		t.Errorf("the study count still promises the buried card:\n%s", body)
	}
}
