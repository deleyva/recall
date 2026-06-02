-- +goose Up
CREATE TABLE learning_goals (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    seed_url        TEXT NOT NULL,
    seed_title      TEXT NOT NULL,
    seed_lang       TEXT NOT NULL DEFAULT 'en',
    time_horizon    INTEGER NOT NULL DEFAULT 14,
    daily_pace      INTEGER NOT NULL DEFAULT 3,
    status          TEXT NOT NULL DEFAULT 'active',
    thumb_url       TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(user_id, seed_url)
);

CREATE TABLE graph_nodes (
    id              TEXT PRIMARY KEY,
    goal_id         TEXT NOT NULL REFERENCES learning_goals(id) ON DELETE CASCADE,
    wiki_title      TEXT NOT NULL,
    wiki_url        TEXT NOT NULL,
    summary         TEXT NOT NULL DEFAULT '',
    thumb_url       TEXT NOT NULL DEFAULT '',
    depth           INTEGER NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'discovered',
    article_id      TEXT REFERENCES articles(id) ON DELETE SET NULL,
    scheduled_day   INTEGER,
    sort_order      INTEGER NOT NULL DEFAULT 0,
    relevance_score REAL NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(goal_id, wiki_url)
);

CREATE TABLE graph_edges (
    id              TEXT PRIMARY KEY,
    goal_id         TEXT NOT NULL REFERENCES learning_goals(id) ON DELETE CASCADE,
    source_node_id  TEXT NOT NULL REFERENCES graph_nodes(id) ON DELETE CASCADE,
    target_node_id  TEXT NOT NULL REFERENCES graph_nodes(id) ON DELETE CASCADE,
    UNIQUE(goal_id, source_node_id, target_node_id)
);

-- +goose Down
DROP TABLE IF EXISTS graph_edges;
DROP TABLE IF EXISTS graph_nodes;
DROP TABLE IF EXISTS learning_goals;
