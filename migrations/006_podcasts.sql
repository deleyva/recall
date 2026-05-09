-- +goose Up
CREATE TABLE IF NOT EXISTS podcasts (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    title TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    audio_url TEXT NOT NULL DEFAULT '',
    notebook_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT '',
    completed_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS podcast_articles (
    podcast_id TEXT NOT NULL REFERENCES podcasts(id) ON DELETE CASCADE,
    article_id TEXT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    PRIMARY KEY (podcast_id, article_id)
);

CREATE INDEX idx_podcasts_user_id ON podcasts(user_id);
CREATE INDEX idx_podcasts_status ON podcasts(status);

ALTER TABLE users ADD COLUMN podcast_enabled INTEGER NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;

-- +goose Down
DROP TABLE IF EXISTS podcast_articles;
DROP TABLE IF EXISTS podcasts;
