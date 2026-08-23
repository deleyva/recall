package services

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/deleyva/recall/internal/models"
)

// leechSeed inserts a card with a chosen lapse count, due an hour ago.
func leechSeed(t *testing.T, db *sql.DB, id, deckID string, lapses int) {
	t.Helper()
	mustExec(t, db, `
		INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days,
			scheduled_days, reps, lapses, state, last_review, created_at, updated_at)
		VALUES (?, ?, ?, 'back', ?, 3.5, 5.5, 2, 4, 12, ?, 2,
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		id, deckID, "front "+id, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), lapses)
}

func suspendedFlag(t *testing.T, db *sql.DB, id string) bool {
	t.Helper()
	var v bool
	if err := db.QueryRow("SELECT suspended FROM cards WHERE id = ?", id).Scan(&v); err != nil {
		t.Fatalf("read suspended for %s: %v", id, err)
	}
	return v
}

// ISC-60 — the threshold is a boundary, not a vibe. Eight lapses is a leech;
// seven is a card the learner is still allowed to find hard.
func TestIsLeechAtTheThreshold(t *testing.T) {
	cases := []struct {
		lapses int
		want   bool
	}{{0, false}, {7, false}, {8, true}, {20, true}}
	for _, tc := range cases {
		if got := (models.Card{Lapses: tc.lapses}).IsLeech(); got != tc.want {
			t.Errorf("IsLeech(lapses=%d) = %v, want %v", tc.lapses, got, tc.want)
		}
	}
	if models.LeechThreshold != 8 {
		t.Errorf("threshold is %d; Anki's default and this ISA's number is 8", models.LeechThreshold)
	}
}

// ISC-61 — the list holds every card at or over the threshold that the user
// owns, worst first, and stops at the owner.
func TestListLeechesIsOwnerScopedAndWorstFirst(t *testing.T) {
	db := buryDB(t)
	leechSeed(t, db, "mild", "deck", 7)
	leechSeed(t, db, "bad", "deck", 9)
	leechSeed(t, db, "worst", "deck2", 14)
	leechSeed(t, db, "suspendedLeech", "deck", 11)
	leechSeed(t, db, "otherUser", "otherdeck", 30)
	mustExec(t, db, "UPDATE cards SET suspended = 1 WHERE id = 'suspendedLeech'")

	cards, err := NewCardService(db).ListLeeches("u1")
	if err != nil {
		t.Fatalf("list leeches: %v", err)
	}

	var got []string
	for _, c := range cards {
		got = append(got, c.ID)
	}
	want := []string{"worst", "suspendedLeech", "bad"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v (worst first, suspended leeches included)", got, want)
		}
	}
}

// ISC-62 — a suspended card is served by nothing, and comes back with the
// schedule it left with. Suspension must never be a way to lose work.
func TestSuspendedCardIsNeverServedAndKeepsItsSchedule(t *testing.T) {
	db := buryDB(t)
	leechSeed(t, db, "leech", "deck", 9)
	cards := NewCardService(db)

	const cols = `due, stability, difficulty, elapsed_days, scheduled_days, reps, lapses, state, last_review`
	snapshot := func() string {
		var due, last string
		var stab, diff float64
		var e, sc, r, l, st int
		if err := db.QueryRow("SELECT "+cols+" FROM cards WHERE id = 'leech'").
			Scan(&due, &stab, &diff, &e, &sc, &r, &l, &st, &last); err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		return fmt.Sprintf("%s|%v|%v|%d|%d|%d|%d|%d|%s", due, stab, diff, e, sc, r, l, st, last)
	}
	before := snapshot()

	if card, count, _ := NewReviewService(db).GetNextDue("u1", "deck"); card == nil || count != 1 {
		t.Fatalf("the card was not being served before suspension: card=%v count=%d", card, count)
	}

	if err := cards.SetSuspendedForUser("leech", "u1", true); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if !suspendedFlag(t, db, "leech") {
		t.Fatal("the card was not suspended")
	}
	if card, count, _ := NewReviewService(db).GetNextDue("u1", "deck"); card != nil || count != 0 {
		t.Fatalf("a suspended card was served: card=%v count=%d", card, count)
	}

	if err := cards.SetSuspendedForUser("leech", "u1", false); err != nil {
		t.Fatalf("unsuspend: %v", err)
	}
	if card, count, _ := NewReviewService(db).GetNextDue("u1", "deck"); card == nil || count != 1 {
		t.Fatalf("the card did not come back: card=%v count=%d", card, count)
	}
	if after := snapshot(); after != before {
		t.Errorf("suspending rewrote scheduling state:\nbefore: %s\nafter:  %s", before, after)
	}
}

// ISC-62 — no count promises a suspended card either. A "Study (1)" that opens
// on an empty session is the defect this guards.
func TestSuspendedCardsLeaveEveryDueCount(t *testing.T) {
	db := buryDB(t)
	leechSeed(t, db, "leech", "deck", 9)
	cards, decks, reviews := NewCardService(db), NewDeckService(db), NewReviewService(db)

	deck, err := decks.Get("u1", "deck")
	if err != nil {
		t.Fatalf("get deck: %v", err)
	}
	if deck.DueCount != 1 {
		t.Fatalf("deck due count %d before suspension, want 1", deck.DueCount)
	}

	if err := cards.SetSuspendedForUser("leech", "u1", true); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	deck, _ = decks.Get("u1", "deck")
	if deck.DueCount != 0 {
		t.Errorf("deck overview still counts a suspended card as due (%d)", deck.DueCount)
	}
	list, _ := decks.List("u1")
	for _, d := range list {
		if d.ID == "deck" && d.DueCount != 0 {
			t.Errorf("deck list still counts a suspended card as due (%d)", d.DueCount)
		}
	}
	stats, err := reviews.GetStats("u1")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.DueToday != 0 {
		t.Errorf("dashboard still counts a suspended card as due (%d)", stats.DueToday)
	}
	if stats.Leeches != 1 {
		t.Errorf("leech count is %d, want 1", stats.Leeches)
	}
	if _, total, _ := cards.ListForUser("u1", "", "", true, 50, 0); total != 0 {
		t.Errorf("the due-only card API still lists a suspended card (%d)", total)
	}
}

// ISC-62 — suspension and burying do not confuse each other. A suspended
// sibling is not buried and is not reported as held back, because it was never
// going to be served in the first place.
func TestSuspendedSiblingsAreNotBuriedOrReportedAsHeldBack(t *testing.T) {
	db := buryDB(t)
	burySeed(t, db, "answered", "deck", "art1", 2, -time.Hour)
	burySeed(t, db, "liveSibling", "deck", "art1", 2, -time.Hour)
	burySeed(t, db, "suspendedSibling", "deck", "art1", 2, -time.Hour)
	mustExec(t, db, "UPDATE cards SET suspended = 1 WHERE id = 'suspendedSibling'")

	buried, err := NewCardService(db).BurySiblings("answered", "u1", time.Now().UTC(), time.UTC)
	if err != nil {
		t.Fatalf("bury: %v", err)
	}
	if buried != 1 {
		t.Fatalf("buried %d siblings, want 1 — the suspended one should be skipped", buried)
	}
	if buriedUntil(t, db, "suspendedSibling") != nil {
		t.Error("a suspended sibling was buried")
	}

	deck, err := NewDeckService(db).Get("u1", "deck")
	if err != nil {
		t.Fatalf("get deck: %v", err)
	}
	if deck.BuriedCount != 1 {
		t.Errorf("held-back count is %d, want 1 — a suspended card is not held back, it is out", deck.BuriedCount)
	}
}

// ISC-62 — suspension is owner-scoped, like every other write path.
func TestSuspendStopsAtTheOwner(t *testing.T) {
	db := buryDB(t)
	leechSeed(t, db, "otherUser", "otherdeck", 9)

	if err := NewCardService(db).SetSuspendedForUser("otherUser", "u1", true); err == nil {
		t.Fatal("suspended another user's card")
	}
	if suspendedFlag(t, db, "otherUser") {
		t.Error("another user's card was suspended")
	}
}
