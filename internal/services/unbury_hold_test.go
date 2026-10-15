package services

import (
	"testing"
	"time"
)

// ISC-103 — unburying is a decision for the day, not for one answer. Before the
// hold, "Study them now" cleared the batch, the learner graded one card, and
// that grading buried the rest again: two clicks, one card. This test is that
// sequence, and it fails without decks.unburied_until.
func TestUnburyDeckHoldsForTheRestOfTheDay(t *testing.T) {
	db := buryDB(t)
	burySeed(t, db, "a", "deck", "art1", 2, -time.Hour)
	burySeed(t, db, "b", "deck", "art1", 2, -time.Hour)
	burySeed(t, db, "c", "deck", "art1", 2, -time.Hour)

	cards := NewCardService(db)
	now := time.Now().UTC()

	// The day begins as designed: the first answer hides its two siblings.
	if n, err := cards.BurySiblings("a", "u1", now, time.Local); err != nil || n != 2 {
		t.Fatalf("first answer buried %d siblings (err=%v), want 2", n, err)
	}

	// The learner asks for the whole batch.
	if _, err := cards.UnburyDeck("deck", "u1", now, time.Local); err != nil {
		t.Fatalf("unbury: %v", err)
	}

	// The second answer must not hide the third card.
	if n, err := cards.BurySiblings("b", "u1", now, time.Local); err != nil || n != 0 {
		t.Fatalf("answering after unbury re-buried %d siblings (err=%v), want 0", n, err)
	}
	if buriedUntil(t, db, "c") != nil {
		t.Fatalf("c is buried again after the learner asked for the whole batch")
	}

	// And the queue has to keep serving: this is what the learner sees.
	card, count, err := NewReviewService(db).GetNextDue("u1", "deck")
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if card == nil {
		t.Fatalf("queue empty after one answer; the session ended on the first card")
	}
	if count != 3 {
		t.Fatalf("queue counts %d cards, want 3 (nothing was graded for real)", count)
	}
}

// The hold expires with the day. Tomorrow the batch is held back again, exactly
// as if the learner had never clicked — unburying is not a permanent opt-out.
func TestUnburyHoldExpiresAtMidnight(t *testing.T) {
	db := buryDB(t)
	burySeed(t, db, "a", "deck", "art1", 2, -time.Hour)
	burySeed(t, db, "b", "deck", "art1", 2, -time.Hour)

	cards := NewCardService(db)
	now := time.Now().UTC()
	if _, err := cards.UnburyDeck("deck", "u1", now, time.Local); err != nil {
		t.Fatalf("unbury: %v", err)
	}

	tomorrow := nextDayBoundary(now, time.Local).Add(time.Hour)
	if n, err := cards.BurySiblings("a", "u1", tomorrow, time.Local); err != nil || n != 1 {
		t.Fatalf("the day after an unbury buried %d siblings (err=%v), want 1", n, err)
	}
}

// The hold is per deck. Releasing one deck's batch says nothing about another
// deck, and nothing about another user's deck of the same name.
func TestUnburyHoldStopsAtTheDeckAndTheOwner(t *testing.T) {
	db := buryDB(t)
	burySeed(t, db, "a", "deck2", "art2", 2, -time.Hour)
	burySeed(t, db, "b", "deck2", "art2", 2, -time.Hour)
	burySeed(t, db, "x", "otherdeck", "art1", 2, -time.Hour)
	burySeed(t, db, "y", "otherdeck", "art1", 2, -time.Hour)

	cards := NewCardService(db)
	now := time.Now().UTC()
	if _, err := cards.UnburyDeck("deck", "u1", now, time.Local); err != nil {
		t.Fatalf("unbury: %v", err)
	}
	// u1 naming u2's deck must write nothing.
	if _, err := cards.UnburyDeck("otherdeck", "u1", now, time.Local); err != nil {
		t.Fatalf("unbury other: %v", err)
	}

	if n, err := cards.BurySiblings("a", "u1", now, time.Local); err != nil || n != 1 {
		t.Fatalf("deck2 buried %d siblings (err=%v), want 1 — the hold leaked across decks", n, err)
	}
	if n, err := cards.BurySiblings("x", "u2", now, time.Local); err != nil || n != 1 {
		t.Fatalf("u2's deck buried %d siblings (err=%v), want 1 — the hold leaked across owners", n, err)
	}
	var hold *string
	if err := db.QueryRow("SELECT unburied_until FROM decks WHERE id = 'otherdeck'").Scan(&hold); err != nil {
		t.Fatalf("read hold: %v", err)
	}
	if hold != nil {
		t.Fatalf("u1 set a hold on u2's deck")
	}
}
