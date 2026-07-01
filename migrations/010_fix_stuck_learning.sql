-- +goose Up
-- Reset cards stuck in Learning state (state=1) back to New (state=0).
-- The long-term scheduler (EnableShortTerm=false) handles New cards directly
-- without the 10-minute learning loop that caused cards to cycle repeatedly.
UPDATE cards SET state = 0, reps = 0, stability = 0, difficulty = 0,
  elapsed_days = 0, scheduled_days = 0, due = datetime('now')
WHERE state = 1;

-- +goose Down
-- No rollback: cards will be re-scheduled naturally on next review.
