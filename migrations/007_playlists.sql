-- +goose Up
CREATE TABLE playlists (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL DEFAULT '',
    external_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE playlist_articles (
    playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    article_id TEXT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    PRIMARY KEY (playlist_id, article_id)
);
CREATE TABLE playlist_decks (
    playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    deck_id TEXT NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
    PRIMARY KEY (playlist_id, deck_id)
);
CREATE INDEX idx_playlists_user_id ON playlists(user_id);
CREATE INDEX idx_playlist_articles_article ON playlist_articles(article_id);
CREATE INDEX idx_playlist_decks_deck ON playlist_decks(deck_id);

-- +goose Down
DROP TABLE IF EXISTS playlist_decks;
DROP TABLE IF EXISTS playlist_articles;
DROP TABLE IF EXISTS playlists;
