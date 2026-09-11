-- +goose Up
-- File bodies have no SQL content table. A contentless-delete index supports
-- ordinary DELETE/REPLACE while keeping document text solely on disk.
DROP TRIGGER IF EXISTS knowledge_fts_ai;
DROP TABLE knowledge_fts;
CREATE VIRTUAL TABLE knowledge_fts USING fts5(
    title, summary, body_md, content='', contentless_delete=1
);
CREATE TABLE knowledge_search_state (
    rowid INTEGER PRIMARY KEY,
    mtime INTEGER NOT NULL,
    size INTEGER NOT NULL
);
-- +goose StatementBegin
CREATE TRIGGER knowledge_search_ad AFTER DELETE ON knowledge BEGIN
    DELETE FROM knowledge_fts WHERE rowid = OLD.rowid;
    DELETE FROM knowledge_search_state WHERE rowid = OLD.rowid;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER artifact_links_ad AFTER DELETE ON artifact BEGIN
    DELETE FROM link WHERE (to_type = 'artifact' AND to_id = OLD.id)
                        OR (from_type = 'artifact' AND from_id = OLD.id);
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER artifact_links_ad;
DROP TRIGGER knowledge_search_ad;
DROP TABLE knowledge_search_state;
DROP TABLE knowledge_fts;
CREATE VIRTUAL TABLE knowledge_fts USING fts5(title, summary, body_md, content='knowledge', content_rowid='rowid');
