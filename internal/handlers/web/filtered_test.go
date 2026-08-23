package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/deleyva/recall/internal/services"
)

// fsrsSnapshot dumps every scheduling column of every card, so "the session
// changed nothing" can be a claim with evidence rather than an assertion.
func fsrsSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query(`SELECT id, due, stability, difficulty, elapsed_days, scheduled_days,
		reps, lapses, state, last_review FROM cards ORDER BY id`)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id, due, last string
		var stab, diff float64
		var e, s, r, l, st int
		if err := rows.Scan(&id, &due, &stab, &diff, &e, &s, &r, &l, &st, &last); err != nil {
			t.Fatalf("scan: %v", err)
		}
		fmt.Fprintf(&b, "%s|%s|%v|%v|%d|%d|%d|%d|%d|%s\n", id, due, stab, diff, e, s, r, l, st, last)
	}
	return b.String()
}

func reviewLogCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM review_logs").Scan(&n); err != nil {
		t.Fatalf("count logs: %v", err)
	}
	return n
}

// seedFilteredSession gives the user three tagged, due cards across two decks.
func seedFilteredSession(t *testing.T, db *sql.DB) *services.TagService {
	t.Helper()
	exec := func(q string, args ...interface{}) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	exec("UPDATE cards SET due = '2030-01-01T00:00:00Z'")
	exec("INSERT INTO decks (id, user_id, name, description) VALUES ('d2', ?, 'Second', '')", testUserID)
	for i, deck := range []string{testDeckID, testDeckID, "d2"} {
		exec(`INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days,
			scheduled_days, reps, lapses, state, last_review, created_at, updated_at, kind)
			VALUES (?, ?, ?, ?, '2026-01-01T00:00:00Z', 4.5, 6.5, 3, 5, 8, 2, 2,
			'2026-08-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 'recognition')`,
			fmt.Sprintf("ft%d", i), deck, fmt.Sprintf("front %d", i), fmt.Sprintf("back %d", i))
	}
	tags := services.NewTagService(db)
	for i := 0; i < 3; i++ {
		if err := tags.Attach(testUserID, fmt.Sprintf("ft%d", i), "musica/armonia"); err != nil {
			t.Fatalf("attach: %v", err)
		}
	}
	return tags
}

// runFilteredSession studies every card the filter offers, carrying cookies so
// the server-side filter and cursor survive between requests. Returns how many
// cards were graded.
func runFilteredSession(t *testing.T, h *ReviewHandler, query string, rating string) int {
	t.Helper()

	c, rec := call(t, http.MethodGet, "/study?"+query, nil, nil)
	if err := h.FilteredStudyPage(c); err != nil {
		t.Fatalf("start session: %v", err)
	}
	cookies := rec.Result().Cookies()
	body := rec.Body.String()

	graded := 0
	for graded < 10 { // a bound, so a bug is a failure rather than a hang
		id := cardIDFrom(body)
		if id == "" {
			break
		}
		c, rec = call(t, http.MethodPost, "/study",
			url.Values{"card_id": {id}, "rating": {rating}}, cookies)
		if err := h.FilteredSubmitReview(c); err != nil {
			t.Fatalf("submit: %v", err)
		}
		if next := rec.Result().Cookies(); len(next) > 0 {
			cookies = next
		}
		body = rec.Body.String()
		graded++
	}
	return graded
}

// cardIDFrom pulls the card id out of a rendered study partial.
func cardIDFrom(body string) string {
	for _, marker := range []string{`/study/`, `value="`} {
		_ = marker
	}
	i := strings.Index(body, "/study/")
	if i >= 0 {
		rest := body[i+len("/study/"):]
		if j := strings.IndexAny(rest, `"/`); j > 0 {
			return rest[:j]
		}
	}
	return ""
}

// ISC-58 — a whole session in no-reschedule mode leaves every scheduling column
// exactly as it found it, and writes no review log either. A log entry with no
// scheduling behind it would feed cram reviews to the one instrument the
// outcome claims are measured with.
func TestNoRescheduleSessionWritesNothing(t *testing.T) {
	h, db := newReviewHandler(t)
	seedFilteredSession(t, db)

	before := fsrsSnapshot(t, db)
	logsBefore := reviewLogCount(t, db)

	graded := runFilteredSession(t, h, "tag=musica/armonia&no_reschedule=1", "3")
	if graded != 3 {
		t.Fatalf("graded %d cards, want all 3 — the pass must terminate and cover the set", graded)
	}

	if after := fsrsSnapshot(t, db); after != before {
		t.Errorf("a no-reschedule session rewrote scheduling state:\nbefore:\n%safter:\n%s", before, after)
	}
	if now := reviewLogCount(t, db); now != logsBefore {
		t.Errorf("a no-reschedule session wrote %d review logs", now-logsBefore)
	}
}

// ISC-59 — normal mode does reschedule, and the review is logged exactly as a
// deck session logs one.
func TestFilteredSessionInNormalModeReschedulesAndLogs(t *testing.T) {
	h, db := newReviewHandler(t)
	seedFilteredSession(t, db)

	before := fsrsSnapshot(t, db)
	logsBefore := reviewLogCount(t, db)

	graded := runFilteredSession(t, h, "tag=musica/armonia", "3")
	if graded == 0 {
		t.Fatal("nothing was graded")
	}

	if after := fsrsSnapshot(t, db); after == before {
		t.Error("normal mode changed no scheduling state")
	}
	written := reviewLogCount(t, db) - logsBefore
	if written != graded {
		t.Errorf("%d reviews graded but %d logged", graded, written)
	}

	// The log carries the same shape a deck session writes.
	var rating, state, scheduled int
	if err := db.QueryRow(`SELECT rating, state, scheduled_days FROM review_logs
		ORDER BY review_time DESC LIMIT 1`).Scan(&rating, &state, &scheduled); err != nil {
		t.Fatalf("read log: %v", err)
	}
	if rating != 3 {
		t.Errorf("logged rating %d, want 3", rating)
	}
}
