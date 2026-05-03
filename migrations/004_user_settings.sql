-- +goose Up
ALTER TABLE users ADD COLUMN daily_card_limit INTEGER NOT NULL DEFAULT 5;
ALTER TABLE users ADD COLUMN readeck_url TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN readeck_api_token TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite doesn't support DROP COLUMN in older versions, but goose needs a Down block
-- This is a no-op since we can't easily remove a column in SQLite
