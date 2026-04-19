package scheduler

import (
	"fmt"
	"time"

	"github.com/deleyva/recall/internal/models"
	"github.com/open-spaced-repetition/go-fsrs/v3"
)

type Scheduler struct {
	fsrs *fsrs.FSRS
}

func New() *Scheduler {
	params := fsrs.DefaultParam()
	return &Scheduler{
		fsrs: fsrs.NewFSRS(params),
	}
}

type ScheduleResult struct {
	Card      models.Card
	Log       models.ReviewLog
	Intervals map[int]time.Duration // rating -> interval
}

func (s *Scheduler) toFSRSCard(c models.Card) fsrs.Card {
	return fsrs.Card{
		Due:           c.Due,
		Stability:     c.Stability,
		Difficulty:    c.Difficulty,
		ElapsedDays:   uint64(c.ElapsedDays),
		ScheduledDays: uint64(c.ScheduledDays),
		Reps:          uint64(c.Reps),
		Lapses:        uint64(c.Lapses),
		State:         fsrs.State(c.State),
		LastReview:    c.LastReview,
	}
}

func (s *Scheduler) fromFSRSCard(fc fsrs.Card, original models.Card) models.Card {
	original.Due = fc.Due
	original.Stability = fc.Stability
	original.Difficulty = fc.Difficulty
	original.ElapsedDays = int(fc.ElapsedDays)
	original.ScheduledDays = int(fc.ScheduledDays)
	original.Reps = int(fc.Reps)
	original.Lapses = int(fc.Lapses)
	original.State = int(fc.State)
	original.LastReview = fc.LastReview
	return original
}

func (s *Scheduler) Schedule(card models.Card, rating int, now time.Time) (models.Card, models.ReviewLog) {
	fc := s.toFSRSCard(card)
	result := s.fsrs.Repeat(fc, now)

	r := fsrs.Rating(rating)
	info := result[r]

	updated := s.fromFSRSCard(info.Card, card)

	log := models.ReviewLog{
		CardID:        card.ID,
		Rating:        rating,
		ScheduledDays: int(info.Card.ScheduledDays),
		ElapsedDays:   int(info.Card.ElapsedDays),
		ReviewTime:    now,
		State:         int(info.Card.State),
	}

	return updated, log
}

func (s *Scheduler) PreviewIntervals(card models.Card, now time.Time) map[int]string {
	fc := s.toFSRSCard(card)
	result := s.fsrs.Repeat(fc, now)

	intervals := make(map[int]string)
	for _, r := range []fsrs.Rating{fsrs.Again, fsrs.Hard, fsrs.Good, fsrs.Easy} {
		info := result[r]
		dur := info.Card.Due.Sub(now)
		intervals[int(r)] = formatDuration(dur)
	}
	return intervals
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	if days < 30 {
		return fmt.Sprintf("%dd", days)
	}
	months := days / 30
	return fmt.Sprintf("%dmo", months)
}
