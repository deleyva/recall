-- +goose Up
-- Numbered 020 because 019 is the highest migration on disk and the tree is
-- level with origin/main. The deployed goose_db_version could not be read this
-- session (NAS unreachable over SSH), so the number rests on the repo, not the
-- database — recheck the startup log before applying if that ever diverges.
--
-- Unburying was a single click that lasted one answer: "Study them now"
-- cleared buried_until, the learner answered one card, and that answer buried
-- the rest of the batch again. The learner had said "the whole batch, today";
-- the system heard "this card". unburied_until records the decision on the
-- deck until the learner's next local midnight, so the sibling bury stays off
-- for the rest of that day and comes back on its own tomorrow.
--
-- Presentation, like buried_until: nothing about scheduling reads it. NULL
-- means no hold, which is what every existing deck gets.
ALTER TABLE decks ADD COLUMN unburied_until TEXT;

-- +goose Down
ALTER TABLE decks DROP COLUMN unburied_until;
