-- +goose Up
-- Knowledge files are the source of truth. The database keeps only metadata.
-- Recreate the table because SQLite cannot drop a column portably.
DROP TRIGGER IF EXISTS knowledge_fts_au;
DROP TRIGGER IF EXISTS knowledge_fts_ad;
DROP TRIGGER IF EXISTS knowledge_fts_ai;
DROP TABLE IF EXISTS knowledge_fts;

CREATE TABLE knowledge_new (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    board_id     TEXT REFERENCES board(id) ON DELETE SET NULL,
    slug         TEXT NOT NULL,
    title        TEXT NOT NULL,
    path         TEXT NOT NULL,
    doc_type     TEXT NOT NULL DEFAULT 'note',
    summary      TEXT NOT NULL DEFAULT '',
    recap        TEXT,
    recap_hash   TEXT,
    content_hash TEXT NOT NULL,
    mtime        INTEGER NOT NULL,
    size         INTEGER NOT NULL,
    global       INTEGER NOT NULL DEFAULT 0,
    review_by    INTEGER,
    reviewed_at  INTEGER,
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (project_id, slug)
);

INSERT INTO knowledge_new (
    rowid, id, project_id, board_id, slug, title, path, doc_type, summary,
    recap, recap_hash, content_hash, mtime, size, global, review_by,
    reviewed_at, version, created_at, updated_at
)
SELECT rowid, id, project_id, board_id, slug, title, path, doc_type, summary,
       recap, recap_hash, content_hash, mtime, size, global, review_by,
       reviewed_at, version, created_at, updated_at
FROM knowledge;

DROP TABLE knowledge;
ALTER TABLE knowledge_new RENAME TO knowledge;
CREATE INDEX knowledge_project ON knowledge(project_id, updated_at DESC);
CREATE INDEX knowledge_board ON knowledge(board_id);
CREATE INDEX knowledge_global ON knowledge(global) WHERE global = 1;

-- External-content FTS stores only the search index, not another document body.
-- The application refreshes rows from the Markdown files when they change.
CREATE VIRTUAL TABLE knowledge_fts USING fts5(
    title,
    summary,
    body_md,
    content='knowledge',
    content_rowid='rowid'
);

-- Cards remain database-native, but their FTS index is derived rather than a
-- second stored copy of every card body.
DROP TRIGGER IF EXISTS card_fts_au;
DROP TRIGGER IF EXISTS card_fts_ad;
DROP TRIGGER IF EXISTS card_fts_ai;
DROP TABLE IF EXISTS card_fts;
CREATE VIRTUAL TABLE card_fts USING fts5(
    title,
    body_md,
    content='card',
    content_rowid='rowid'
);

-- +goose StatementBegin
CREATE TRIGGER card_fts_ai AFTER INSERT ON card BEGIN
    INSERT INTO card_fts(rowid, title, body_md)
    VALUES (NEW.rowid, NEW.title, NEW.body_md);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER card_fts_ad AFTER DELETE ON card BEGIN
    INSERT INTO card_fts(card_fts, rowid, title, body_md)
    VALUES ('delete', OLD.rowid, OLD.title, OLD.body_md);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER card_fts_au AFTER UPDATE ON card BEGIN
    INSERT INTO card_fts(card_fts, rowid, title, body_md)
    VALUES ('delete', OLD.rowid, OLD.title, OLD.body_md);
    INSERT INTO card_fts(rowid, title, body_md)
    VALUES (NEW.rowid, NEW.title, NEW.body_md);
END;
-- +goose StatementEnd

INSERT INTO card_fts(rowid, title, body_md)
SELECT rowid, title, body_md FROM card;

-- +goose StatementBegin
CREATE TRIGGER knowledge_fts_ai AFTER INSERT ON knowledge BEGIN
    -- New files are indexed by Core after their contents have been written.
    SELECT 1;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER knowledge_fts_ai;
DROP TABLE knowledge_fts;
DROP TRIGGER card_fts_au;
DROP TRIGGER card_fts_ad;
DROP TRIGGER card_fts_ai;
DROP TABLE card_fts;
-- The down migration intentionally does not restore body_md: file-backed
-- knowledge is a breaking storage change before the first public release.
