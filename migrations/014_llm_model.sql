-- +goose Up
-- Per-user LLM model override. Empty string means "fall back to the instance
-- default" (the LLM_MODEL env var, then the compiled constant), so an existing
-- row keeps working without any backfill.
ALTER TABLE users ADD COLUMN llm_model TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite doesn't support DROP COLUMN before 3.35.0; no-op.
