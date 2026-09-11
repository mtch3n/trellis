-- +goose Up
-- Knowledge entries are files; these rows are a cache and an index over them.
-- The file always wins (§5): content_hash, mtime and size detect an external
-- edit, and a mismatch means re-read rather than reconcile.
CREATE TABLE knowledge (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    board_id     TEXT REFERENCES board(id) ON DELETE SET NULL,  -- association, never ownership
    slug         TEXT NOT NULL,
    title        TEXT NOT NULL,
    path         TEXT NOT NULL,                                 -- absolute path to the markdown file
    doc_type     TEXT NOT NULL DEFAULT 'note',                  -- the template it came from
    summary      TEXT NOT NULL DEFAULT '',                      -- frontmatter summary:, the recap fallback
    recap        TEXT,                                          -- agent-written pinned-context summary
    recap_hash   TEXT,                                          -- content_hash when the recap was written
    body_md      TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    mtime        INTEGER NOT NULL,
    size         INTEGER NOT NULL,
    global       INTEGER NOT NULL DEFAULT 0,                    -- escalated to ~/.trellis/global/kb
    review_by    INTEGER,                                       -- set at escalation; global rots louder
    reviewed_at  INTEGER,
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (project_id, slug)
);
CREATE INDEX knowledge_project ON knowledge(project_id, updated_at DESC);
CREATE INDEX knowledge_board ON knowledge(board_id);
CREATE INDEX knowledge_global ON knowledge(global) WHERE global = 1;

-- Tags and labels are the same vocabulary cards use, so `search --label research`
-- spans both (§10).
CREATE TABLE knowledge_label (
    doc_id   TEXT NOT NULL REFERENCES knowledge(id) ON DELETE CASCADE,
    label_id TEXT NOT NULL REFERENCES label(id) ON DELETE CASCADE,
    PRIMARY KEY (doc_id, label_id)
);
CREATE INDEX knowledge_label_label ON knowledge_label(label_id);

CREATE TABLE knowledge_tag (
    doc_id TEXT NOT NULL REFERENCES knowledge(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL REFERENCES tag(id) ON DELETE CASCADE,
    PRIMARY KEY (doc_id, tag_id)
);
CREATE INDEX knowledge_tag_tag ON knowledge_tag(tag_id);

-- A NULL board means project-wide. A doc may be pinned to several boards.
CREATE TABLE pin (
    id           TEXT PRIMARY KEY,
    knowledge_id TEXT NOT NULL REFERENCES knowledge(id) ON DELETE CASCADE,
    board_id     TEXT REFERENCES board(id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    UNIQUE (knowledge_id, board_id)
);

-- Nominations are evidence for escalation (§10.7), not a vote.
CREATE TABLE nomination (
    id           TEXT PRIMARY KEY,
    knowledge_id TEXT NOT NULL REFERENCES knowledge(id) ON DELETE CASCADE,
    actor        TEXT NOT NULL,
    reason       TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    UNIQUE (knowledge_id, actor)
);

-- summary is indexed beside the body: it is the author's own one-line
-- distillation, which is the text a search is most likely to match.
CREATE VIRTUAL TABLE knowledge_fts USING fts5(
    doc_id UNINDEXED,
    title,
    summary,
    body_md
);

-- +goose StatementBegin
CREATE TRIGGER knowledge_fts_ai AFTER INSERT ON knowledge BEGIN
    INSERT INTO knowledge_fts (doc_id, title, summary, body_md)
    VALUES (NEW.id, NEW.title, NEW.summary, NEW.body_md);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER knowledge_fts_ad AFTER DELETE ON knowledge BEGIN
    DELETE FROM knowledge_fts WHERE doc_id = OLD.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER knowledge_fts_au AFTER UPDATE ON knowledge BEGIN
    DELETE FROM knowledge_fts WHERE doc_id = OLD.id;
    INSERT INTO knowledge_fts (doc_id, title, summary, body_md)
    VALUES (NEW.id, NEW.title, NEW.summary, NEW.body_md);
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER knowledge_fts_au;
DROP TRIGGER knowledge_fts_ad;
DROP TRIGGER knowledge_fts_ai;
DROP TABLE knowledge_fts;
DROP TABLE nomination;
DROP TABLE pin;
DROP TABLE knowledge_tag;
DROP TABLE knowledge_label;
DROP TABLE knowledge;
