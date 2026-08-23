-- +goose Up
-- The tag store. Its shape is the Tagging Standard, not a free-text field:
-- every tag is `dominio/tema`, exactly two segments, the first drawn from a
-- closed list. Free depth was considered and rejected — it offers infinitely
-- many defensible places to file one idea, which is a drift generator rather
-- than a remedy.
--
-- `key` is the normalized form (lowercase, no diacritics) and `display` is what
-- a human reads. The UNIQUE below is the whole of the orthographic fix and it
-- lives in the schema on purpose: two tags with the same key ARE the same tag,
-- and no write path gets to disagree.
CREATE TABLE IF NOT EXISTS tags (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    display    TEXT NOT NULL,
    domain     TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(user_id, key)
);

CREATE TABLE IF NOT EXISTS card_tags (
    card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    tag_id  TEXT NOT NULL REFERENCES tags(id)  ON DELETE CASCADE,
    PRIMARY KEY (card_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_tags_user_domain ON tags(user_id, domain);
CREATE INDEX IF NOT EXISTS idx_card_tags_tag ON card_tags(tag_id);

-- +goose Down
DROP INDEX IF EXISTS idx_card_tags_tag;
DROP INDEX IF EXISTS idx_tags_user_domain;
DROP TABLE IF EXISTS card_tags;
DROP TABLE IF EXISTS tags;
