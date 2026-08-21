package services

import (
	"database/sql"
	"testing"
	"time"

	"github.com/open-spaced-repetition/go-fsrs/v3"
)

func seedQueueCard(t *testing.T, db *sql.DB, id string, state int, dueIn time.Duration) {
	t.Helper()
	due := time.Now().UTC().Add(dueIn).Format(time.RFC3339)
	mustExec(t, db, `
		INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days,
			scheduled_days, reps, lapses, state, last_review, created_at, updated_at)
		VALUES (?, 'deck', ?, 'back', ?, 0, 0, 0, 0, 0, 0, ?,
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		id, "front "+id, due, state)
}

func queueDB(t *testing.T) *sql.DB {
	t.Helper()
	db := newTestDB(t)
	seedUser(t, db, "u1")
	mustExec(t, db, "INSERT INTO decks (id, user_id, name, description) VALUES ('deck','u1','Deck','')")
	return db
}

// ISC-42 — a card failed a moment ago comes back inside the same session. Its
// due date is minutes away, so a queue that only serves cards already due would
// end the session instead, and the short loop the scheduler just regained would
// never close.
func TestGetNextDueServesLearningCardsSlightlyAhead(t *testing.T) {
	db := queueDB(t)
	seedQueueCard(t, db, "relearning", int(fsrs.Relearning), 3*time.Minute)

	card, count, err := NewReviewService(db).GetNextDue("deck")
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if card == nil {
		t.Fatal("session ended with a card due in 3 minutes — the failed card never returns")
	}
	if card.ID != "relearning" {
		t.Errorf("served %q, want the relearning card", card.ID)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 — a served card must be counted", count)
	}
}

// The window is bounded. A learning card due well beyond it waits, so studying
// ahead never turns into studying everything.
func TestGetNextDueDoesNotReachBeyondTheWindow(t *testing.T) {
	db := queueDB(t)
	seedQueueCard(t, db, "far", int(fsrs.Relearning), LearnAheadWindow+10*time.Minute)

	card, count, err := NewReviewService(db).GetNextDue("deck")
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if card != nil {
		t.Errorf("served %q from beyond the learn-ahead window", card.ID)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

// Learn-ahead applies to the short loop only. A review card due tomorrow is not
// dragged forward just because the session has run dry.
func TestGetNextDueDoesNotPullReviewCardsForward(t *testing.T) {
	db := queueDB(t)
	seedQueueCard(t, db, "review-soon", int(fsrs.Review), 5*time.Minute)

	card, _, err := NewReviewService(db).GetNextDue("deck")
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if card != nil {
		t.Errorf("served the review card %q ahead of its due date", card.ID)
	}
}

// A card mid-loop is time-sensitive and comes first, ahead of new cards waiting
// to be introduced. Without this the failed card sits behind the whole new-card
// backlog and the session ends before it resurfaces.
func TestGetNextDueServesLearningBeforeNew(t *testing.T) {
	db := queueDB(t)
	seedQueueCard(t, db, "new", int(fsrs.New), -time.Hour)
	seedQueueCard(t, db, "relearning", int(fsrs.Relearning), -time.Minute)

	card, count, err := NewReviewService(db).GetNextDue("deck")
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if card == nil || card.ID != "relearning" {
		t.Fatalf("served %v, want the relearning card first", card)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

// New cards still precede reviews — the existing ordering choice is preserved.
func TestGetNextDueStillServesNewBeforeReview(t *testing.T) {
	db := queueDB(t)
	seedQueueCard(t, db, "review", int(fsrs.Review), -2*time.Hour)
	seedQueueCard(t, db, "new", int(fsrs.New), -time.Hour)

	card, _, err := NewReviewService(db).GetNextDue("deck")
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if card == nil || card.ID != "new" {
		t.Fatalf("served %v, want the new card first", card)
	}
}

// A card actually due beats one that is merely close, so learn-ahead never
// jumps the real queue.
func TestGetNextDuePrefersDueOverAhead(t *testing.T) {
	db := queueDB(t)
	seedQueueCard(t, db, "due-now", int(fsrs.Review), -time.Minute)
	seedQueueCard(t, db, "ahead", int(fsrs.Learning), 5*time.Minute)

	card, _, err := NewReviewService(db).GetNextDue("deck")
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if card == nil || card.ID != "due-now" {
		t.Fatalf("served %v, want the card that is actually due", card)
	}
}

// An empty queue is still an empty queue.
func TestGetNextDueReturnsNothingWhenNothingIsDue(t *testing.T) {
	db := queueDB(t)
	seedQueueCard(t, db, "tomorrow", int(fsrs.Review), 24*time.Hour)

	card, count, err := NewReviewService(db).GetNextDue("deck")
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if card != nil || count != 0 {
		t.Errorf("got card=%v count=%d, want nothing", card, count)
	}
}
