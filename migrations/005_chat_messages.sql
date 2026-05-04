-- +goose Up
CREATE TABLE IF NOT EXISTS chat_messages (
    id TEXT PRIMARY KEY,
    article_id TEXT NOT NULL REFERENCES articles(id),
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_chat_messages_article ON chat_messages(article_id, user_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS chat_messages;
