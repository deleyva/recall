-- +goose Up
-- Numbered 016 because the deployed database reached version 15 on 2026-08-21
-- ("successfully migrated database to version: 15" in the startup log). The
-- rule that produced 015's off-by-one still holds: read the deployed
-- goose_db_version before numbering, not just the files on disk.
--
-- Cards generated from one article are siblings. They share retrieval cues, so
-- the first one served primes the rest and the session measures priming rather
-- than memory. buried_until hides a sibling from the queue until a timestamp.
--
-- It is a presentation column, not a scheduling one: nothing writes it except
-- the bury and unbury paths, and neither touches due, stability, difficulty,
-- elapsed_days, scheduled_days, reps, lapses, state or last_review. NULL means
-- not buried, which is what every existing row gets.
ALTER TABLE cards ADD COLUMN buried_until TEXT;

CREATE INDEX IF NOT EXISTS idx_cards_buried_until ON cards(buried_until);

-- +goose Down
DROP INDEX IF EXISTS idx_cards_buried_until;
ALTER TABLE cards DROP COLUMN buried_until;
