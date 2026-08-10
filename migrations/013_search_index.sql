-- +goose Up
-- Full-text search over every text Recall stores for a user: article bodies,
-- flashcard fronts/backs and chat messages. Kept in sync by triggers so that
-- every write path (web handlers, API, Readeck sync, card cron, CLI) is covered.
CREATE VIRTUAL TABLE search_index USING fts5(
    title,
    body,
    kind UNINDEXED,
    entity_id UNINDEXED,
    user_id UNINDEXED,
    parent_id UNINDEXED,
    deck_id UNINDEXED,
    tokenize = 'unicode61 remove_diacritics 2'
);

-- Backfill existing rows.
INSERT INTO search_index (title, body, kind, entity_id, user_id, parent_id, deck_id)
SELECT a.title, a.content, 'article', a.id, a.user_id, a.id, '' FROM articles a;

INSERT INTO search_index (title, body, kind, entity_id, user_id, parent_id, deck_id)
SELECT c.front, c.back, 'flashcard', c.id,
       (SELECT d.user_id FROM decks d WHERE d.id = c.deck_id),
       COALESCE(c.article_id, ''), c.deck_id
FROM cards c;

INSERT INTO search_index (title, body, kind, entity_id, user_id, parent_id, deck_id)
SELECT COALESCE((SELECT a.title FROM articles a WHERE a.id = m.article_id), ''),
       m.content, 'chat', m.id, m.user_id, m.article_id, ''
FROM chat_messages m;

-- +goose StatementBegin
CREATE TRIGGER search_articles_ai AFTER INSERT ON articles BEGIN
    INSERT INTO search_index (title, body, kind, entity_id, user_id, parent_id, deck_id)
    VALUES (new.title, new.content, 'article', new.id, new.user_id, new.id, '');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER search_articles_ad AFTER DELETE ON articles BEGIN
    DELETE FROM search_index WHERE kind = 'article' AND entity_id = old.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER search_articles_au AFTER UPDATE ON articles BEGIN
    DELETE FROM search_index WHERE kind = 'article' AND entity_id = old.id;
    INSERT INTO search_index (title, body, kind, entity_id, user_id, parent_id, deck_id)
    VALUES (new.title, new.content, 'article', new.id, new.user_id, new.id, '');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER search_cards_ai AFTER INSERT ON cards BEGIN
    INSERT INTO search_index (title, body, kind, entity_id, user_id, parent_id, deck_id)
    VALUES (new.front, new.back, 'flashcard', new.id,
            (SELECT d.user_id FROM decks d WHERE d.id = new.deck_id),
            COALESCE(new.article_id, ''), new.deck_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER search_cards_ad AFTER DELETE ON cards BEGIN
    DELETE FROM search_index WHERE kind = 'flashcard' AND entity_id = old.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER search_cards_au AFTER UPDATE ON cards BEGIN
    DELETE FROM search_index WHERE kind = 'flashcard' AND entity_id = old.id;
    INSERT INTO search_index (title, body, kind, entity_id, user_id, parent_id, deck_id)
    VALUES (new.front, new.back, 'flashcard', new.id,
            (SELECT d.user_id FROM decks d WHERE d.id = new.deck_id),
            COALESCE(new.article_id, ''), new.deck_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER search_chat_ai AFTER INSERT ON chat_messages BEGIN
    INSERT INTO search_index (title, body, kind, entity_id, user_id, parent_id, deck_id)
    VALUES (COALESCE((SELECT a.title FROM articles a WHERE a.id = new.article_id), ''),
            new.content, 'chat', new.id, new.user_id, new.article_id, '');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER search_chat_ad AFTER DELETE ON chat_messages BEGIN
    DELETE FROM search_index WHERE kind = 'chat' AND entity_id = old.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER search_chat_au AFTER UPDATE ON chat_messages BEGIN
    DELETE FROM search_index WHERE kind = 'chat' AND entity_id = old.id;
    INSERT INTO search_index (title, body, kind, entity_id, user_id, parent_id, deck_id)
    VALUES (COALESCE((SELECT a.title FROM articles a WHERE a.id = new.article_id), ''),
            new.content, 'chat', new.id, new.user_id, new.article_id, '');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS search_chat_au;
DROP TRIGGER IF EXISTS search_chat_ad;
DROP TRIGGER IF EXISTS search_chat_ai;
DROP TRIGGER IF EXISTS search_cards_au;
DROP TRIGGER IF EXISTS search_cards_ad;
DROP TRIGGER IF EXISTS search_cards_ai;
DROP TRIGGER IF EXISTS search_articles_au;
DROP TRIGGER IF EXISTS search_articles_ad;
DROP TRIGGER IF EXISTS search_articles_ai;
DROP TABLE IF EXISTS search_index;
