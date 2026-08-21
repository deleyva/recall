package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deleyva/recall/internal/database"
	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/scheduler"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
	"github.com/pressly/goose/v3"
)

const (
	testUserID   = "u1"
	testDeckID   = "d1"
	testFront    = "Which work argues that understanding is historically situated?"
	testBack     = "Verdad y método"
	recognitionI = "c-recognition"
	productionID = "c-production"
)

func newReviewHandler(t *testing.T) (*ReviewHandler, *sql.DB) {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	if err := goose.Up(db, "../../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	exec := func(q string, args ...interface{}) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	exec("INSERT INTO users (id, email, password_hash) VALUES (?, 'u1@example.com', 'x')", testUserID)
	exec("INSERT INTO decks (id, user_id, name, description) VALUES (?, ?, 'Deck', '')", testDeckID, testUserID)
	insertCard := func(id, kind string) {
		exec(`INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days,
			scheduled_days, reps, lapses, state, last_review, created_at, updated_at, kind)
			VALUES (?, ?, ?, ?, '2026-01-01T00:00:00Z', 0, 0, 0, 0, 0, 0, 0,
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', ?)`,
			id, testDeckID, testFront, testBack, kind)
	}
	insertCard(recognitionI, services.KindRecognition)
	insertCard(productionID, services.KindProduction)

	tmpl := templates.NewRegistry()
	if err := tmpl.Load("../../../templates"); err != nil {
		t.Fatalf("load templates: %v", err)
	}

	store := sessions.NewCookieStore([]byte("test-session-key-at-least-32-bytes!!"))
	h := NewReviewHandler(
		services.NewReviewService(db),
		services.NewCardService(db),
		services.NewDeckService(db),
		scheduler.New(),
		tmpl,
		store,
	)
	return h, db
}

// call builds an authenticated echo context for one of the study routes. Any
// cookies on the request are carried through, which is how a test can act as the
// same session across two calls.
func call(t *testing.T, method, target string, form url.Values, cookies []*http.Cookie) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()

	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.Set(middleware.UserIDKey, testUserID)
	return c, rec
}

func withCardParams(c echo.Context, cardID string) echo.Context {
	c.SetParamNames("id", "cardID")
	c.SetParamValues(testDeckID, cardID)
	return c
}

// ISC-29 — a production card's study view carries a text input and does not
// include the answer anywhere in the served HTML.
func TestProductionCardViewDoesNotServeTheAnswer(t *testing.T) {
	h, db := newReviewHandler(t)
	// Only the production card is due, so it is the one served.
	if _, err := db.Exec("UPDATE cards SET due = '2030-01-01T00:00:00Z' WHERE id = ?", recognitionI); err != nil {
		t.Fatalf("park recognition card: %v", err)
	}

	c, rec := call(t, http.MethodGet, "/decks/"+testDeckID+"/study", nil, nil)
	c.SetParamNames("id")
	c.SetParamValues(testDeckID)
	c.Request().Header.Set("HX-Request", "true")

	if err := h.StudyPage(c); err != nil {
		t.Fatalf("study page: %v", err)
	}

	body := rec.Body.String()
	if strings.Contains(body, testBack) {
		t.Errorf("the answer %q was served with the question:\n%s", testBack, body)
	}
	if !strings.Contains(body, `name="answer"`) {
		t.Errorf("no answer input in the production card view:\n%s", body)
	}
	if strings.Contains(body, "Show Answer") {
		t.Error("production card offered a reveal button")
	}
}

// ISC-32 — the recognition flow is untouched: a reveal button, no input, and the
// answer route hands back the answer as before.
func TestRecognitionCardFlowIsUnchanged(t *testing.T) {
	h, db := newReviewHandler(t)
	if _, err := db.Exec("UPDATE cards SET due = '2030-01-01T00:00:00Z' WHERE id = ?", productionID); err != nil {
		t.Fatalf("park production card: %v", err)
	}

	c, rec := call(t, http.MethodGet, "/decks/"+testDeckID+"/study", nil, nil)
	c.SetParamNames("id")
	c.SetParamValues(testDeckID)
	c.Request().Header.Set("HX-Request", "true")
	if err := h.StudyPage(c); err != nil {
		t.Fatalf("study page: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Show Answer") {
		t.Errorf("recognition card lost its reveal button:\n%s", body)
	}
	if strings.Contains(body, `name="answer"`) {
		t.Error("recognition card grew an answer input")
	}

	c2, rec2 := call(t, http.MethodGet, "/decks/"+testDeckID+"/study/"+recognitionI+"/answer", nil, nil)
	if err := h.ShowAnswer(withCardParams(c2, recognitionI)); err != nil {
		t.Fatalf("show answer: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), testBack) {
		t.Error("recognition answer view did not include the answer")
	}
}

// ISC-33 — the reveal is server-side. A request that skips the typing step
// cannot obtain a production card's answer, through the answer route or the edit
// route.
func TestProductionAnswerIsUnreachableWithoutAnAttempt(t *testing.T) {
	h, _ := newReviewHandler(t)

	for _, tc := range []struct {
		name    string
		handler func(echo.Context) error
	}{
		{"answer route", h.ShowAnswer},
		{"edit route", h.StudyEditCard},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := call(t, http.MethodGet, "/decks/"+testDeckID+"/study/"+productionID+"/answer", nil, nil)
			if err := tc.handler(withCardParams(c, productionID)); err != nil {
				t.Fatalf("handler: %v", err)
			}
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", rec.Code)
			}
			if strings.Contains(rec.Body.String(), testBack) {
				t.Errorf("the answer leaked through the %s: %s", tc.name, rec.Body.String())
			}
		})
	}
}

// ISC-31 — a miss pre-selects Again, a hit pre-selects Good, and every button
// stays clickable either way.
func TestSubmitAnswerPreSelectsARating(t *testing.T) {
	cases := []struct {
		name      string
		typed     string
		wantRing  string
		otherRing string
		wantBadge string
	}{
		{"miss", "no idea", "ring-red-300", "ring-green-300", "Not produced"},
		{"hit", "verdad y metodo", "ring-green-300", "ring-red-300", "Produced"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newReviewHandler(t)

			c, rec := call(t, http.MethodPost, "/decks/"+testDeckID+"/study/"+productionID+"/answer",
				url.Values{"answer": {tc.typed}}, nil)
			if err := h.SubmitAnswer(withCardParams(c, productionID)); err != nil {
				t.Fatalf("submit answer: %v", err)
			}

			body := rec.Body.String()
			if !strings.Contains(body, tc.wantRing) {
				t.Errorf("expected %s to be pre-selected:\n%s", tc.wantRing, body)
			}
			if strings.Contains(body, tc.otherRing) {
				t.Errorf("%s should not be pre-selected too", tc.otherRing)
			}
			if !strings.Contains(body, tc.wantBadge) {
				t.Errorf("expected the %q badge", tc.wantBadge)
			}
			// All four grades remain available — the suggestion never removes a choice.
			for _, rating := range []string{`value="1"`, `value="2"`, `value="3"`, `value="4"`} {
				if !strings.Contains(body, rating) {
					t.Errorf("rating %s is missing from the answer view", rating)
				}
			}
			if !strings.Contains(body, testBack) {
				t.Error("the answer view should reveal the answer once an attempt was made")
			}
		})
	}
}

// ISC-33 — after an attempt, the same session may reach the answer again (a
// reload must not lock the learner out), and grading the card closes the reveal
// so the next card has to be earned on its own.
func TestRevealIsScopedToTheAttemptedCard(t *testing.T) {
	h, _ := newReviewHandler(t)

	c, rec := call(t, http.MethodPost, "/decks/"+testDeckID+"/study/"+productionID+"/answer",
		url.Values{"answer": {"verdad y metodo"}}, nil)
	if err := h.SubmitAnswer(withCardParams(c, productionID)); err != nil {
		t.Fatalf("submit answer: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected the attempt to be recorded in the session")
	}

	c2, rec2 := call(t, http.MethodGet, "/decks/"+testDeckID+"/study/"+productionID+"/answer", nil, cookies)
	if err := h.ShowAnswer(withCardParams(c2, productionID)); err != nil {
		t.Fatalf("show answer after attempt: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Errorf("status after an attempt = %d, want 200", rec2.Code)
	}

	// Grading it clears the reveal.
	c3, rec3 := call(t, http.MethodPost, "/decks/"+testDeckID+"/study",
		url.Values{"card_id": {productionID}, "rating": {"3"}}, cookies)
	c3.SetParamNames("id")
	c3.SetParamValues(testDeckID)
	if err := h.SubmitReview(c3); err != nil {
		t.Fatalf("submit review: %v", err)
	}

	c4, rec4 := call(t, http.MethodGet, "/decks/"+testDeckID+"/study/"+productionID+"/answer", nil, rec3.Result().Cookies())
	if err := h.ShowAnswer(withCardParams(c4, productionID)); err != nil {
		t.Fatalf("show answer after grading: %v", err)
	}
	if rec4.Code != http.StatusForbidden {
		t.Errorf("status after grading = %d, want 403 — the reveal must not survive the review", rec4.Code)
	}
	_ = rec
}

// Anti-6 in miniature — switching a card's kind leaves its schedule alone.
func TestSetKindLeavesSchedulingUntouched(t *testing.T) {
	_, db := newReviewHandler(t)
	cards := services.NewCardService(db)

	snapshot := func() (due string, stability, difficulty float64, reps, lapses, state int) {
		row := db.QueryRow("SELECT due, stability, difficulty, reps, lapses, state FROM cards WHERE id = ?", recognitionI)
		if err := row.Scan(&due, &stability, &difficulty, &reps, &lapses, &state); err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		return
	}

	d1, s1, f1, r1, l1, st1 := snapshot()
	if err := cards.SetKindForUser(recognitionI, testUserID, services.KindProduction); err != nil {
		t.Fatalf("set kind: %v", err)
	}
	d2, s2, f2, r2, l2, st2 := snapshot()

	if d1 != d2 || s1 != s2 || f1 != f2 || r1 != r2 || l1 != l2 || st1 != st2 {
		t.Errorf("scheduling changed: %v/%v/%v/%v/%v/%v → %v/%v/%v/%v/%v/%v",
			d1, s1, f1, r1, l1, st1, d2, s2, f2, r2, l2, st2)
	}

	var kind string
	if err := db.QueryRow("SELECT kind FROM cards WHERE id = ?", recognitionI).Scan(&kind); err != nil {
		t.Fatalf("read kind: %v", err)
	}
	if kind != services.KindProduction {
		t.Errorf("kind = %q, want production", kind)
	}
}

// A card belonging to someone else cannot be switched.
func TestSetKindIsUserScoped(t *testing.T) {
	_, db := newReviewHandler(t)
	if err := services.NewCardService(db).SetKindForUser(recognitionI, "someone-else", services.KindProduction); err == nil {
		t.Error("expected an error when switching another user's card")
	}
}

// An unknown kind is refused rather than written.
func TestSetKindRejectsUnknownValues(t *testing.T) {
	_, db := newReviewHandler(t)
	if err := services.NewCardService(db).SetKindForUser(recognitionI, testUserID, "cloze"); err == nil {
		t.Error("expected an error for an unknown kind")
	}
}

// The edit form is the control that switches a card over, and doing so leaves
// the card's schedule alone.
func TestStudyUpdateCardSwitchesKind(t *testing.T) {
	h, db := newReviewHandler(t)

	var dueBefore string
	var repsBefore int
	if err := db.QueryRow("SELECT due, reps FROM cards WHERE id = ?", recognitionI).Scan(&dueBefore, &repsBefore); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	c, _ := call(t, http.MethodPut, "/decks/"+testDeckID+"/study/"+recognitionI,
		url.Values{"front": {testFront}, "back": {testBack}, "kind": {"production"}}, nil)
	if err := h.StudyUpdateCard(withCardParams(c, recognitionI)); err != nil {
		t.Fatalf("update card: %v", err)
	}

	var kind, dueAfter string
	var repsAfter int
	if err := db.QueryRow("SELECT kind, due, reps FROM cards WHERE id = ?", recognitionI).Scan(&kind, &dueAfter, &repsAfter); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if kind != services.KindProduction {
		t.Errorf("kind = %q, want production", kind)
	}
	if dueAfter != dueBefore || repsAfter != repsBefore {
		t.Errorf("schedule moved: %s/%d → %s/%d", dueBefore, repsBefore, dueAfter, repsAfter)
	}
}

// An edit that does not carry a kind leaves the card as it was, so saving a
// wording fix never silently changes how the card is asked.
func TestStudyUpdateCardWithoutKindLeavesItAlone(t *testing.T) {
	h, db := newReviewHandler(t)

	c, _ := call(t, http.MethodPut, "/decks/"+testDeckID+"/study/"+productionID,
		url.Values{"front": {testFront}, "back": {testBack}}, nil)
	if err := h.StudyUpdateCard(withCardParams(c, productionID)); err != nil {
		t.Fatalf("update card: %v", err)
	}

	var kind string
	if err := db.QueryRow("SELECT kind FROM cards WHERE id = ?", productionID).Scan(&kind); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if kind != services.KindProduction {
		t.Errorf("kind = %q, want it unchanged at production", kind)
	}
}
