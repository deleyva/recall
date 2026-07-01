-- +goose Up
ALTER TABLE users ADD COLUMN flashcard_gen_enabled INTEGER NOT NULL DEFAULT 1;

-- +goose Down
-- SQLite doesn't support DROP COLUMN before 3.35.0; no-op.
