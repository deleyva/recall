-- +goose Up
-- Numbered 015, not 014: the live database already carries version 14 as
-- applied (2026-08-17) from a migration that is not in this tree. Reusing 014
-- would be silently skipped by goose and the app would start against a schema
-- without this column. Check the deployed goose_db_version before numbering a
-- migration, not just the files on disk.
--
-- A production card makes the learner type the answer before anything is
-- revealed. Existing cards stay recognition cards, so nothing about the current
-- study flow changes until a card is deliberately switched over.
--
-- SQLite cannot add a CHECK constraint to an existing table, so the allowed
-- values are enforced in CardService.SetKind rather than in the schema.
ALTER TABLE cards ADD COLUMN kind TEXT NOT NULL DEFAULT 'recognition';

-- +goose Down
ALTER TABLE cards DROP COLUMN kind;
