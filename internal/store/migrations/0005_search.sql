-- +goose Up
-- FTS5 full-text search for cards: title and body_md
-- Uses a separate FTS5 table (not external-content) to store card_id, title, body_md.
-- This is more straightforward than external-content mode with rowid mapping.
CREATE VIRTUAL TABLE card_fts USING fts5(
    card_id UNINDEXED,
    title,
    body_md
);

-- Triggers to keep card_fts in sync with card
-- +goose StatementBegin
CREATE TRIGGER card_fts_ai AFTER INSERT ON card BEGIN
    INSERT INTO card_fts (card_id, title, body_md)
    VALUES (NEW.id, NEW.title, NEW.body_md);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER card_fts_ad AFTER DELETE ON card BEGIN
    DELETE FROM card_fts WHERE card_id = OLD.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER card_fts_au AFTER UPDATE ON card BEGIN
    DELETE FROM card_fts WHERE card_id = OLD.id;
    INSERT INTO card_fts (card_id, title, body_md)
    VALUES (NEW.id, NEW.title, NEW.body_md);
END;
-- +goose StatementEnd

-- Backfill existing cards (if any)
INSERT INTO card_fts (card_id, title, body_md)
SELECT id, title, body_md FROM card;

-- +goose Down
DROP TRIGGER card_fts_au;
DROP TRIGGER card_fts_ad;
DROP TRIGGER card_fts_ai;
DROP TABLE card_fts;
