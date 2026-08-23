-- +goose Up
-- A card failed eight times is not hard, it is malformed. Anki flags that card
-- as a leech at eight lapses on exactly that reasoning. `cards.lapses` has been
-- stored since the beginning and never read; this migration adds the other half
-- of the remedy — the ability to take a card out of rotation without deleting
-- the work that went into it.
--
-- Suspension is presentation state, like buried_until: no study path serves a
-- suspended card, and unsuspending restores it with the schedule it had. The
-- FSRS columns are not touched by either direction.
--
-- SQLite has no boolean; 0 is live, 1 is suspended, and every existing card
-- stays live.
ALTER TABLE cards ADD COLUMN suspended INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_cards_suspended ON cards(suspended);

-- +goose Down
DROP INDEX IF EXISTS idx_cards_suspended;
ALTER TABLE cards DROP COLUMN suspended;
