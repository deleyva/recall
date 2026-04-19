-- +goose Up
CREATE TABLE users (
    id            TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE decks (
    id          TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(user_id, name)
);

CREATE TABLE cards (
    id             TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    deck_id        TEXT NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
    front          TEXT NOT NULL,
    back           TEXT NOT NULL,
    due            TEXT NOT NULL DEFAULT (datetime('now')),
    stability      REAL NOT NULL DEFAULT 0,
    difficulty     REAL NOT NULL DEFAULT 0,
    elapsed_days   INTEGER NOT NULL DEFAULT 0,
    scheduled_days INTEGER NOT NULL DEFAULT 0,
    reps           INTEGER NOT NULL DEFAULT 0,
    lapses         INTEGER NOT NULL DEFAULT 0,
    state          INTEGER NOT NULL DEFAULT 0,
    last_review    TEXT NOT NULL DEFAULT '0001-01-01T00:00:00Z',
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at     TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_cards_due ON cards(deck_id, due);

CREATE TABLE review_logs (
    id             TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    card_id        TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    rating         INTEGER NOT NULL,
    scheduled_days INTEGER NOT NULL DEFAULT 0,
    elapsed_days   INTEGER NOT NULL DEFAULT 0,
    review_time    TEXT NOT NULL DEFAULT (datetime('now')),
    state          INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_review_logs_card ON review_logs(card_id, review_time);

-- +goose Down
DROP TABLE IF EXISTS review_logs;
DROP TABLE IF EXISTS cards;
DROP TABLE IF EXISTS decks;
DROP TABLE IF EXISTS users;
