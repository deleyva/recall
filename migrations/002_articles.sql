-- +goose Up
CREATE TABLE articles (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    domain     TEXT NOT NULL DEFAULT '',
    content    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_articles_user ON articles(user_id, created_at);
CREATE UNIQUE INDEX idx_articles_user_url ON articles(user_id, url);

ALTER TABLE cards ADD COLUMN article_id TEXT REFERENCES articles(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE cards DROP COLUMN article_id;
DROP TABLE IF EXISTS articles;
