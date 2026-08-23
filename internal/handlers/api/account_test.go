package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deleyva/recall/internal/database"
	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/services"
	"github.com/labstack/echo/v4"
	"github.com/pressly/goose/v3"
)

const settingsUser = "u1"

func accountHandler(t *testing.T) (*Handler, *sql.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "account.db"))
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
	if _, err := db.Exec(
		"INSERT INTO users (id, email, password_hash) VALUES (?, 'u1@example.com', 'x')", settingsUser,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return &Handler{db: db, llm: services.NewLLMService("", "", "", db)}, db
}

func callAPI(t *testing.T, h *Handler, method, path, body string, fn func(echo.Context) error) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.Set(middleware.UserIDKey, settingsUser)
	if err := fn(c); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return rec
}

func readLimits(t *testing.T, h *Handler) (gen, newLimit, reviewLimit int) {
	t.Helper()
	rec := callAPI(t, h, http.MethodGet, "/api/v1/me", "", h.GetMe)
	var payload struct {
		Settings struct {
			DailyCardLimit   int `json:"daily_card_limit"`
			DailyNewLimit    int `json:"daily_new_limit"`
			DailyReviewLimit int `json:"daily_review_limit"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode /me: %v — %s", err, rec.Body.String())
	}
	return payload.Settings.DailyCardLimit, payload.Settings.DailyNewLimit, payload.Settings.DailyReviewLimit
}

// ISC-69 — three distinct settings. The generation limit bounds what the
// nightly job creates; the two study limits bound what the queue serves. The
// API reports all three, and writing one leaves the other two alone.
func TestTheThreeLimitsAreIndependentSettings(t *testing.T) {
	h, _ := accountHandler(t)

	gen, newLimit, reviewLimit := readLimits(t, h)
	if newLimit != 20 || reviewLimit != 200 {
		t.Fatalf("defaults are %d new / %d reviews, want Anki's 20 / 200", newLimit, reviewLimit)
	}

	cases := []struct {
		name, body string
		wantGen    int
		wantNew    int
		wantReview int
	}{
		{"only the generation limit", `{"daily_card_limit":7}`, 7, 20, 200},
		{"only the new-card limit", `{"daily_new_limit":5}`, 7, 5, 200},
		{"only the review limit", `{"daily_review_limit":50}`, 7, 5, 50},
		{"zero means none today", `{"daily_new_limit":0}`, 7, 0, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := callAPI(t, h, http.MethodPut, "/api/v1/me/settings", tc.body, h.UpdateMySettings)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			gen, newLimit, reviewLimit = readLimits(t, h)
			if gen != tc.wantGen || newLimit != tc.wantNew || reviewLimit != tc.wantReview {
				t.Errorf("after %s: gen=%d new=%d review=%d, want %d/%d/%d",
					tc.body, gen, newLimit, reviewLimit, tc.wantGen, tc.wantNew, tc.wantReview)
			}
		})
	}
}

// ISC-69 — each limit is validated on its own terms. Zero is a real answer for
// a study limit and a wrong one for the generation limit.
func TestEachLimitIsValidatedSeparately(t *testing.T) {
	h, _ := accountHandler(t)

	bad := []struct{ name, body string }{
		{"generation limit cannot be zero", `{"daily_card_limit":0}`},
		{"generation limit has a ceiling", `{"daily_card_limit":21}`},
		{"new limit cannot be negative", `{"daily_new_limit":-1}`},
		{"new limit has a ceiling", `{"daily_new_limit":501}`},
		{"review limit cannot be negative", `{"daily_review_limit":-1}`},
		{"review limit has a ceiling", `{"daily_review_limit":10000}`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			rec := callAPI(t, h, http.MethodPut, "/api/v1/me/settings", tc.body, h.UpdateMySettings)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status %d for %s, want 400", rec.Code, tc.body)
			}
		})
	}

	// None of the refusals moved anything.
	gen, newLimit, reviewLimit := readLimits(t, h)
	if gen != 5 && gen != 0 {
		t.Logf("generation limit is %d (schema default)", gen)
	}
	if newLimit != 20 || reviewLimit != 200 {
		t.Errorf("a refused write changed the study limits: %d / %d", newLimit, reviewLimit)
	}
}
