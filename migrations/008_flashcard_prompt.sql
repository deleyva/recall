-- +goose Up
ALTER TABLE users ADD COLUMN flashcard_prompt TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite doesn't support DROP COLUMN before 3.35.0, but goose needs a down block
-- For rollback, recreate without the column if needed
