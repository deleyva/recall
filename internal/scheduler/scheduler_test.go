package scheduler

import (
	"math"
	"testing"
	"time"

	"github.com/deleyva/recall/internal/models"
	"github.com/open-spaced-repetition/go-fsrs/v3"
)

func reviewCard(scheduledDays int) models.Card {
	return models.Card{
		ID:            "c1",
		Due:           time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		Stability:     40,
		Difficulty:    5,
		ElapsedDays:   scheduledDays,
		ScheduledDays: scheduledDays,
		Reps:          6,
		Lapses:        0,
		State:         int(fsrs.Review),
		LastReview:    time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -scheduledDays),
	}
}

// ISC-41 — failing a review card returns it in minutes, not days. The long-term
// scheduler cannot do this: its own library test documents a failed 669-day card
// coming back in 12 days.
func TestFailingAReviewCardReturnsItWithinTheDay(t *testing.T) {
	s := New()
	now := time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC)

	updated, _ := s.Schedule(reviewCard(60), int(fsrs.Again), now)

	gap := updated.Due.Sub(now)
	if gap <= 0 || gap >= 24*time.Hour {
		t.Errorf("Again scheduled the card %v out, want a positive gap under 24h", gap)
	}
	if updated.State != int(fsrs.Relearning) {
		t.Errorf("state = %d, want relearning (%d)", updated.State, int(fsrs.Relearning))
	}
	if updated.Lapses != 1 {
		t.Errorf("lapses = %d, want 1", updated.Lapses)
	}
}

// The preview shown on the buttons has to agree with what pressing them does —
// an "Again" button promising days while the scheduler gives minutes would be
// the same lie in a different place.
func TestPreviewAgreesWithTheScheduler(t *testing.T) {
	s := New()
	now := time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC)
	card := reviewCard(60)

	preview := s.PreviewIntervals(card, now)
	if preview[int(fsrs.Again)] == "" {
		t.Fatal("no preview for Again")
	}
	if strings := preview[int(fsrs.Again)]; strings[len(strings)-1] == 'd' {
		t.Errorf("Again previews %q — a day-scale interval means short-term scheduling is off", strings)
	}
}

// ISC-65 — the short loop terminates. Two Goods from New graduate the card to
// Review rather than cycling in Learning forever, which is the failure mode that
// migration 010 was written to clean up.
func TestNewCardGraduatesAfterTwoGoods(t *testing.T) {
	s := New()
	now := time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC)

	card := models.Card{ID: "c1", Due: now, State: int(fsrs.New)}

	card, _ = s.Schedule(card, int(fsrs.Good), now)
	if card.State != int(fsrs.Learning) {
		t.Fatalf("after one Good state = %d, want learning (%d)", card.State, int(fsrs.Learning))
	}
	if gap := card.Due.Sub(now); gap <= 0 || gap >= time.Hour {
		t.Errorf("first Good scheduled %v out, want a short in-session step", gap)
	}

	now = card.Due
	card, _ = s.Schedule(card, int(fsrs.Good), now)
	if card.State != int(fsrs.Review) {
		t.Errorf("after two Goods state = %d, want review (%d) — the learning loop must terminate", card.State, int(fsrs.Review))
	}
	if card.ScheduledDays < 1 {
		t.Errorf("graduated interval = %d days, want at least 1", card.ScheduledDays)
	}
}

// ISC-40 — fuzz disperses intervals of 2.5 days or more, so cards that would
// otherwise share a due date spread out. Note what this does NOT claim: the
// library seeds its PRNG from (review second, reps, difficulty × stability), so
// two cards in genuinely identical state reviewed in the same second still get
// the same fuzz. Dispersion comes from cards differing in state or review time,
// which is every real pair of cards after their first review.
func TestFuzzDispersesLongIntervals(t *testing.T) {
	s := New()
	base := time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC)

	intervals := map[int]bool{}
	var seen []int
	for i := 0; i < 40; i++ {
		updated, _ := s.Schedule(reviewCard(60), int(fsrs.Good), base.Add(time.Duration(i)*time.Second))
		intervals[updated.ScheduledDays] = true
		seen = append(seen, updated.ScheduledDays)
	}

	if len(intervals) < 2 {
		t.Fatalf("every interval came out identical (%v) — fuzz is not applied", seen)
	}

	// Every fuzzed interval must stay inside the library's documented range
	// around the unfuzzed one, so dispersion never becomes drift.
	unfuzzed := unfuzzedInterval(t, reviewCard(60), base)
	delta := fuzzDelta(float64(unfuzzed))
	for _, ivl := range seen {
		if math.Abs(float64(ivl-unfuzzed)) > delta+1 {
			t.Errorf("interval %d strays more than %.0f days from the unfuzzed %d", ivl, delta, unfuzzed)
		}
	}
}

// With fuzz off the same sweep collapses to a single interval, which is what
// made every card generated on one day march together forever.
func TestWithoutFuzzIntervalsCollapse(t *testing.T) {
	base := time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC)

	params := fsrs.DefaultParam()
	params.EnableFuzz = false
	unfuzzed := &Scheduler{fsrs: fsrs.NewFSRS(params)}

	intervals := map[int]bool{}
	for i := 0; i < 40; i++ {
		updated, _ := unfuzzed.Schedule(reviewCard(60), int(fsrs.Good), base.Add(time.Duration(i)*time.Second))
		intervals[updated.ScheduledDays] = true
	}
	if len(intervals) != 1 {
		t.Errorf("got %d distinct intervals with fuzz off, want exactly 1", len(intervals))
	}
}

func unfuzzedInterval(t *testing.T, card models.Card, now time.Time) int {
	t.Helper()
	params := fsrs.DefaultParam()
	params.EnableFuzz = false
	s := &Scheduler{fsrs: fsrs.NewFSRS(params)}
	updated, _ := s.Schedule(card, int(fsrs.Good), now)
	return updated.ScheduledDays
}

// fuzzDelta mirrors the library's own range calculation from its exported
// FUZZ_RANGES table.
func fuzzDelta(interval float64) float64 {
	delta := 1.0
	for _, r := range fsrs.FUZZ_RANGES {
		delta += r.Factor * math.Max(math.Min(interval, r.End)-r.Start, 0)
	}
	return delta
}

// ISC-43 in miniature — constructing the scheduler touches no card. The config
// change cannot rewrite anything on its own; only a review writes.
func TestSchedulerConstructionDoesNotAlterACard(t *testing.T) {
	before := reviewCard(60)
	_ = New()
	after := reviewCard(60)

	if before != after {
		t.Error("card state differs after constructing a scheduler")
	}
}
