package services

import (
	"database/sql"
	"testing"
	"time"
)

func setLimits(t *testing.T, db *sql.DB, userID string, newLimit, reviewLimit int) {
	t.Helper()
	mustExec(t, db, "UPDATE users SET daily_new_limit = ?, daily_review_limit = ? WHERE id = ?",
		newLimit, reviewLimit, userID)
}

// logReview writes a review at a given offset from now. offsetDays of 0 is
// today in the learner's own day, which is the window the limits are counted in.
func logReview(t *testing.T, db *sql.DB, cardID string, offsetDays int) {
	t.Helper()
	at := time.Now().UTC()
	if offsetDays != 0 {
		at = at.AddDate(0, 0, offsetDays)
	}
	mustExec(t, db, `
		INSERT INTO review_logs (id, card_id, rating, scheduled_days, elapsed_days, review_time, state)
		VALUES (?, ?, 3, 1, 1, ?, 2)`,
		generateID(), cardID, at.Format(time.RFC3339))
}

// ISC-68 — the new-card limit stops the queue introducing more material, and
// stops nothing else. A card already introduced today is not new load.
func TestNewCardLimitStopsTheQueueIntroducingMore(t *testing.T) {
	db := buryDB(t)
	setLimits(t, db, "u1", 2, 100)
	reviews := NewReviewService(db)

	// Two cards introduced today: reviewed once, moved into learning, and now
	// waiting well beyond the learn-ahead window. This is what a card that has
	// been introduced actually looks like — it is no longer state 0, which is
	// why the count cannot be taken from the card table.
	for _, id := range []string{"intro1", "intro2"} {
		burySeed(t, db, id, "deck", "", 1, 2*time.Hour)
		logReview(t, db, id, 0)
	}
	burySeed(t, db, "stillNew", "deck", "", 0, -time.Hour)

	card, count, err := reviews.GetNextDue("u1", "deck")
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if card != nil || count != 0 {
		t.Fatalf("served %v with the new-card budget spent (count=%d)", card, count)
	}

	// Raising the limit by one releases exactly one card.
	setLimits(t, db, "u1", 3, 100)
	card, count, err = reviews.GetNextDue("u1", "deck")
	if err != nil {
		t.Fatalf("next due after raising: %v", err)
	}
	if card == nil || card.ID != "stillNew" || count != 1 {
		t.Fatalf("card=%v count=%d, want the one remaining new card", card, count)
	}
}

// ISC-68 — the review limit is counted and enforced independently of the new
// one. Spending the review budget must not stop new cards, and vice versa.
func TestReviewLimitIsIndependentOfTheNewLimit(t *testing.T) {
	db := buryDB(t)
	setLimits(t, db, "u1", 5, 1)
	reviews := NewReviewService(db)

	// One review card seen today, introduced long ago; another still waiting.
	burySeed(t, db, "reviewedToday", "deck", "", 2, -time.Hour)
	logReview(t, db, "reviewedToday", -30)
	logReview(t, db, "reviewedToday", 0)
	burySeed(t, db, "waitingReview", "deck", "", 2, -time.Hour)
	burySeed(t, db, "freshCard", "deck", "", 0, -time.Hour)

	card, count, err := reviews.GetNextDue("u1", "deck")
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if card == nil || card.ID != "freshCard" {
		t.Fatalf("card=%v, want the new card — the review budget is spent, the new one is not", card)
	}
	if count != 1 {
		t.Errorf("count %d, want 1: only the new card is servable", count)
	}

	// Spend the new budget too, and the session is over even though a review
	// card is sitting there due.
	setLimits(t, db, "u1", 0, 1)
	card, count, _ = reviews.GetNextDue("u1", "deck")
	if card != nil || count != 0 {
		t.Fatalf("served %v with both budgets spent", card)
	}
}

// ISC-68 — a card mid-loop always finishes. Limits bound how much load is taken
// on, not whether load already taken on gets closed; refusing to serve a card
// failed ten minutes ago would strand exactly the re-retrieval ISC-51 restored.
func TestLimitsNeverStrandACardMidLoop(t *testing.T) {
	db := buryDB(t)
	setLimits(t, db, "u1", 0, 0)
	burySeed(t, db, "relearning", "deck", "", 3, -time.Minute)
	burySeed(t, db, "learning", "deck", "", 1, -time.Minute)
	burySeed(t, db, "newCard", "deck", "", 0, -time.Hour)
	burySeed(t, db, "reviewCard", "deck", "", 2, -time.Hour)

	card, count, err := NewReviewService(db).GetNextDue("u1", "deck")
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if count != 2 {
		t.Errorf("count %d, want 2 — the two mid-loop cards and nothing else", count)
	}
	if card == nil || (card.ID != "relearning" && card.ID != "learning") {
		t.Fatalf("card=%v, want one of the mid-loop cards", card)
	}
}

// ISC-68 — "introduced today" is a card whose FIRST review is today. The review
// log records the state a card moved INTO, so a new card rated Good logs as
// learning; counting the log's state would miss every card the limit exists to
// count.
func TestIntroducedTodayCountsFirstReviews(t *testing.T) {
	db := buryDB(t)
	reviews := NewReviewService(db)

	burySeed(t, db, "todayFirst", "deck", "", 1, -time.Hour)
	logReview(t, db, "todayFirst", 0)

	burySeed(t, db, "oldCard", "deck", "", 2, -time.Hour)
	logReview(t, db, "oldCard", -20)
	logReview(t, db, "oldCard", 0)

	burySeed(t, db, "neverSeen", "deck", "", 0, -time.Hour)

	introduced, reviewed, err := reviews.TodayCounts("u1", time.Local)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if introduced != 1 {
		t.Errorf("introduced today = %d, want 1", introduced)
	}
	if reviewed != 1 {
		t.Errorf("reviewed today = %d, want 1 — the card whose first review predates today", reviewed)
	}
}

// ISC-68 — relearning steps of one card do not eat the day's review budget.
// Counting reviews rather than distinct cards would punish the learner for
// failing, which is the one thing this system must never do.
func TestRelearningStepsDoNotSpendTheReviewBudget(t *testing.T) {
	db := buryDB(t)
	reviews := NewReviewService(db)

	burySeed(t, db, "failedTwice", "deck", "", 2, -time.Hour)
	logReview(t, db, "failedTwice", -10)
	for i := 0; i < 4; i++ {
		logReview(t, db, "failedTwice", 0)
	}

	_, reviewed, err := reviews.TodayCounts("u1", time.Local)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if reviewed != 1 {
		t.Errorf("reviewed = %d, want 1: four attempts at one card is one card", reviewed)
	}
}

// ISC-68 — the filtered cross-deck session obeys the same budget. A filter is a
// different selection, not a way around the day's load.
func TestFilteredSessionObeysTheSameLimits(t *testing.T) {
	db := buryDB(t)
	setLimits(t, db, "u1", 0, 0)
	tags := NewTagService(db)
	burySeed(t, db, "tagged", "deck", "", 2, -time.Hour)
	if err := tags.Attach("u1", "tagged", "musica/armonia"); err != nil {
		t.Fatalf("attach: %v", err)
	}

	card, count, err := NewReviewService(db).GetNextFiltered("u1", StudyFilter{TagKey: "musica/armonia"})
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if card != nil || count != 0 {
		t.Fatalf("a filter served %v past the day's limits", card)
	}
}

// ISC-68 — every count of "due" respects the day's limits, not only the queue
// that serves cards. A deck reading "Study (5)" that opens onto two cards is
// the same defect burying and suspension each had, in a third dress.
func TestDueCountsRespectTheDayLimits(t *testing.T) {
	db := buryDB(t)
	setLimits(t, db, "u1", 2, 100)
	for _, id := range []string{"n1", "n2", "n3", "n4", "n5"} {
		burySeed(t, db, id, "deck", "", 0, -time.Hour)
	}

	decks, reviews := NewDeckService(db), NewReviewService(db)

	deck, err := decks.Get("u1", "deck")
	if err != nil {
		t.Fatalf("get deck: %v", err)
	}
	if deck.DueCount != 2 {
		t.Errorf("deck overview promises %d cards, the queue would serve 2", deck.DueCount)
	}

	list, _ := decks.List("u1")
	for _, d := range list {
		if d.ID == "deck" && d.DueCount != 2 {
			t.Errorf("deck list promises %d, want 2", d.DueCount)
		}
	}

	stats, err := reviews.GetStats("u1")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.DueToday != 2 {
		t.Errorf("dashboard promises %d due today, want 2", stats.DueToday)
	}

	// And the queue agrees with all of them.
	_, queued, err := reviews.GetNextDue("u1", "deck")
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if queued != 2 {
		t.Errorf("queue count %d, want 2", queued)
	}
}
