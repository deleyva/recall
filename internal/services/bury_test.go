package services

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// burySeed inserts one card with an article, a state and a due offset. Siblings
// are cards that share articleID — the de-facto sibling relationship in Recall,
// since a batch is generated from one article.
func burySeed(t *testing.T, db *sql.DB, id, deckID, articleID string, state int, dueIn time.Duration) {
	t.Helper()
	var article interface{}
	if articleID != "" {
		article = articleID
	}
	mustExec(t, db, `
		INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days,
			scheduled_days, reps, lapses, state, last_review, created_at, updated_at, article_id)
		VALUES (?, ?, ?, 'back', ?, 3.5, 5.5, 2, 4, 7, 1, ?,
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', ?)`,
		id, deckID, "front "+id, time.Now().UTC().Add(dueIn).Format(time.RFC3339), state, article)
}

func buriedUntil(t *testing.T, db *sql.DB, id string) *string {
	t.Helper()
	var v *string
	if err := db.QueryRow("SELECT buried_until FROM cards WHERE id = ?", id).Scan(&v); err != nil {
		t.Fatalf("read buried_until for %s: %v", id, err)
	}
	return v
}

// buryDB gives two users, one deck each, so every claim about burying can also
// prove it stopped at the owner's boundary.
func buryDB(t *testing.T) *sql.DB {
	t.Helper()
	db := newTestDB(t)
	seedUser(t, db, "u1")
	seedUser(t, db, "u2")
	// decks are unique per (user, name), so each needs its own name
	mustExec(t, db, "INSERT INTO decks (id, user_id, name, description, created_at) VALUES ('deck','u1','Deck','','2026-01-01T00:00:00Z')")
	mustExec(t, db, "INSERT INTO decks (id, user_id, name, description, created_at) VALUES ('deck2','u1','Second','','2026-01-01T00:00:00Z')")
	mustExec(t, db, "INSERT INTO decks (id, user_id, name, description, created_at) VALUES ('otherdeck','u2','Deck','','2026-01-01T00:00:00Z')")
	seedArticle(t, db, "art1", "u1", "Article one", "body")
	seedArticle(t, db, "art2", "u1", "Article two", "body")
	return db
}

// ISC-45 — answering a card hides the rest of its article's batch. Siblings
// share retrieval cues, so a batch studied back to back measures priming.
func TestBurySiblingsHidesDueSiblingsOfTheSameArticle(t *testing.T) {
	db := buryDB(t)
	burySeed(t, db, "answered", "deck", "art1", 2, -time.Hour)
	burySeed(t, db, "sibling", "deck", "art1", 2, -time.Hour)
	burySeed(t, db, "newSibling", "deck", "art1", 0, 0)

	now := time.Now().UTC()
	buried, err := NewCardService(db).BurySiblings("answered", "u1", now, time.UTC)
	if err != nil {
		t.Fatalf("bury: %v", err)
	}
	if buried != 2 {
		t.Fatalf("buried %d siblings, want 2", buried)
	}
	if buriedUntil(t, db, "answered") != nil {
		t.Error("the card just answered was buried; only its siblings should be")
	}
	for _, id := range []string{"sibling", "newSibling"} {
		if buriedUntil(t, db, id) == nil {
			t.Errorf("%s was not buried", id)
		}
	}
}

// ISC-45 — a card mid-loop is never buried. It was failed minutes ago and is due
// in five; hiding it until tomorrow would delete the same-session re-retrieval
// that ISC-51 and ISC-74 exist to restore, which is the whole point of failing.
func TestBurySiblingsLeavesLearningAndRelearningAlone(t *testing.T) {
	db := buryDB(t)
	burySeed(t, db, "answered", "deck", "art1", 2, -time.Hour)
	burySeed(t, db, "learning", "deck", "art1", 1, -time.Minute)
	burySeed(t, db, "relearning", "deck", "art1", 3, -time.Minute)

	if _, err := NewCardService(db).BurySiblings("answered", "u1", time.Now().UTC(), time.UTC); err != nil {
		t.Fatalf("bury: %v", err)
	}
	for _, id := range []string{"learning", "relearning"} {
		if buriedUntil(t, db, id) != nil {
			t.Errorf("%s was buried; a card mid-loop must stay in the session", id)
		}
	}
}

// ISC-45 — burying reaches exactly as far as the article and the owner. A write
// path leaks as readily as a read path.
func TestBurySiblingsStopsAtTheArticleAndTheOwner(t *testing.T) {
	db := buryDB(t)
	burySeed(t, db, "answered", "deck", "art1", 2, -time.Hour)
	burySeed(t, db, "otherArticle", "deck", "art2", 2, -time.Hour)
	burySeed(t, db, "noArticle", "deck", "", 2, -time.Hour)
	burySeed(t, db, "otherDeckSameOwner", "deck2", "art1", 2, -time.Hour)
	burySeed(t, db, "otherUser", "otherdeck", "art1", 2, -time.Hour)
	burySeed(t, db, "notYetDue", "deck", "art1", 2, 48*time.Hour)

	if _, err := NewCardService(db).BurySiblings("answered", "u1", time.Now().UTC(), time.UTC); err != nil {
		t.Fatalf("bury: %v", err)
	}
	for _, id := range []string{"otherArticle", "noArticle", "otherUser", "notYetDue"} {
		if buriedUntil(t, db, id) != nil {
			t.Errorf("%s was buried and should not have been", id)
		}
	}
	// A sibling the learner moved to another of their own decks is still a
	// sibling: it is the article that makes it one, not the deck.
	if buriedUntil(t, db, "otherDeckSameOwner") == nil {
		t.Error("a sibling in another deck of the same owner was not buried")
	}
}

// ISC-47 — burying is presentation. Every FSRS column has to survive it
// untouched, or the mechanism is a scheduling bug wearing a UI costume.
func TestBuryingChangesNoFSRSColumn(t *testing.T) {
	db := buryDB(t)
	burySeed(t, db, "answered", "deck", "art1", 2, -time.Hour)
	burySeed(t, db, "sibling", "deck", "art1", 2, -time.Hour)

	const cols = `due, stability, difficulty, elapsed_days, scheduled_days, reps, lapses, state, last_review`
	snapshot := func() string {
		rows, err := db.Query("SELECT id, " + cols + " FROM cards ORDER BY id")
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		defer rows.Close()
		out := ""
		for rows.Next() {
			var id, due, lastReview string
			var stab, diff float64
			var elapsed, scheduled, reps, lapses, state int
			if err := rows.Scan(&id, &due, &stab, &diff, &elapsed, &scheduled, &reps, &lapses, &state, &lastReview); err != nil {
				t.Fatalf("scan snapshot: %v", err)
			}
			out += fmt.Sprintf("%s|%s|%v|%v|%d|%d|%d|%d|%d|%s\n",
				id, due, stab, diff, elapsed, scheduled, reps, lapses, state, lastReview)
		}
		return out
	}

	before := snapshot()
	if _, err := NewCardService(db).BurySiblings("answered", "u1", time.Now().UTC(), time.UTC); err != nil {
		t.Fatalf("bury: %v", err)
	}
	if after := snapshot(); after != before {
		t.Errorf("burying rewrote scheduling state:\nbefore:\n%safter:\n%s", before, after)
	}
	if buriedUntil(t, db, "sibling") == nil {
		t.Fatal("nothing was buried, so the snapshot proves nothing")
	}
}

// ISC-46 — a buried card is not served, and comes back on its own once the
// timestamp passes. No unbury step is needed for the ordinary case.
func TestGetNextDueSkipsBuriedCardsUntilTheBoundaryPasses(t *testing.T) {
	db := buryDB(t)
	burySeed(t, db, "buried", "deck", "art1", 2, -time.Hour)

	future := time.Now().UTC().Add(6 * time.Hour).Format(time.RFC3339)
	mustExec(t, db, "UPDATE cards SET buried_until = ? WHERE id = 'buried'", future)

	card, count, err := NewReviewService(db).GetNextDue("u1", "deck")
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if card != nil || count != 0 {
		t.Fatalf("served a buried card: card=%v count=%d", card, count)
	}

	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	mustExec(t, db, "UPDATE cards SET buried_until = ? WHERE id = 'buried'", past)

	card, count, err = NewReviewService(db).GetNextDue("u1", "deck")
	if err != nil {
		t.Fatalf("next due after boundary: %v", err)
	}
	if card == nil || card.ID != "buried" || count != 1 {
		t.Fatalf("card did not come back after its boundary: card=%v count=%d", card, count)
	}
}

// ISC-48 — the unbury control returns the deck's cards to the session, and
// stops at the deck and the owner.
func TestUnburyDeckRestoresOnlyThatDeck(t *testing.T) {
	db := buryDB(t)
	burySeed(t, db, "a", "deck", "art1", 2, -time.Hour)
	burySeed(t, db, "b", "deck", "art1", 2, -time.Hour)
	burySeed(t, db, "elsewhere", "deck2", "art1", 2, -time.Hour)
	burySeed(t, db, "otherUser", "otherdeck", "art1", 2, -time.Hour)

	future := time.Now().UTC().Add(6 * time.Hour).Format(time.RFC3339)
	mustExec(t, db, "UPDATE cards SET buried_until = ?", future)

	cards := NewCardService(db)
	unburied, err := cards.UnburyDeck("deck", "u1")
	if err != nil {
		t.Fatalf("unbury: %v", err)
	}
	if unburied != 2 {
		t.Fatalf("unburied %d, want 2", unburied)
	}
	for _, id := range []string{"a", "b"} {
		if buriedUntil(t, db, id) != nil {
			t.Errorf("%s is still buried", id)
		}
	}
	for _, id := range []string{"elsewhere", "otherUser"} {
		if buriedUntil(t, db, id) == nil {
			t.Errorf("%s was unburied and belongs to another deck or owner", id)
		}
	}

	// The point of unburying is that the session serves them again.
	card, count, err := NewReviewService(db).GetNextDue("u1", "deck")
	if err != nil {
		t.Fatalf("next due after unbury: %v", err)
	}
	if card == nil || count != 2 {
		t.Fatalf("unburied cards were not served again: card=%v count=%d", card, count)
	}

	// Another user's deck must not be reachable by naming it.
	if n, err := cards.UnburyDeck("otherdeck", "u1"); err != nil || n != 0 {
		t.Fatalf("unburied %d rows of another user's deck (err=%v)", n, err)
	}
}

// The boundary is the learner's next midnight, not a fixed 24 hours, and it
// holds across a DST transition — Spain moves its clocks on the last Sunday of
// October, so the day the boundary lands on is 25 hours long.
func TestNextDayBoundaryIsLocalMidnight(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	cases := []struct {
		name string
		now  time.Time
		want string
	}{
		{"late evening", time.Date(2026, 3, 4, 23, 30, 0, 0, madrid), "2026-03-05T00:00:00+01:00"},
		{"just after midnight", time.Date(2026, 3, 5, 0, 5, 0, 0, madrid), "2026-03-06T00:00:00+01:00"},
		{"month end", time.Date(2026, 1, 31, 20, 0, 0, 0, madrid), "2026-02-01T00:00:00+01:00"},
		{"dst fall back", time.Date(2026, 10, 24, 22, 0, 0, 0, madrid), "2026-10-25T00:00:00+02:00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextDayBoundary(tc.now, madrid).Format(time.RFC3339)
			if got != tc.want {
				t.Errorf("nextDayBoundary(%s) = %s, want %s", tc.now.Format(time.RFC3339), got, tc.want)
			}
			if !nextDayBoundary(tc.now, madrid).After(tc.now) {
				t.Error("boundary is not in the future")
			}
		})
	}
}
