-- +goose Up
-- The one daily limit was on the wrong side. `daily_card_limit` bounds how many
-- cards the cron GENERATES; nothing bounded how many are STUDIED, so a backlog
-- could arrive as a wall no matter how the generator behaved.
--
-- These two are study limits, enforced by the queue. New and review load are
-- separated because they are different costs: introducing material is expensive
-- and optional, clearing due reviews is cheaper and not optional. Anki's
-- defaults, 20 and 200, because they are the numbers this collection's owner
-- will be comparing against.
--
-- A limit of 0 means none today, not unlimited. It is the honest reading of the
-- number, and it makes "no new cards while I catch up" expressible.
ALTER TABLE users ADD COLUMN daily_new_limit INTEGER NOT NULL DEFAULT 20;
ALTER TABLE users ADD COLUMN daily_review_limit INTEGER NOT NULL DEFAULT 200;

-- +goose Down
ALTER TABLE users DROP COLUMN daily_review_limit;
ALTER TABLE users DROP COLUMN daily_new_limit;
