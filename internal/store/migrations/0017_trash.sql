-- +goose Up
-- A trashed item is a snapshot: its own row and every row that depends on it,
-- as JSON, taken just before the live rows are deleted. project_id carries no
-- foreign key: a trashed project's rows are gone, its trash rows stay.
CREATE TABLE trash (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL,
    project_key TEXT NOT NULL,
    kind        TEXT NOT NULL,            -- project, board, card, entry, artifact
    item_id     TEXT NOT NULL,            -- the id of the snapshot's own row
    name        TEXT NOT NULL,            -- what restore is given: ref, slug, name or key
    title       TEXT NOT NULL DEFAULT '',
    seq         INTEGER,                  -- a card's number, kept from reuse
    trashed_at  INTEGER NOT NULL,
    trashed_by  TEXT NOT NULL,
    rows        TEXT NOT NULL,            -- [{"table": ..., "row": {...}}], parents first
    files       TEXT NOT NULL DEFAULT '[]' -- [{"from": ..., "to": ...}], relative to the root
);
CREATE INDEX trash_project ON trash(project_id, kind, name);
CREATE INDEX trash_trashed_at ON trash(trashed_at);

-- When the last purge ran. One row, claimed by a conditional UPDATE so two
-- processes starting together sweep once.
CREATE TABLE trash_sweep (
    id      INTEGER PRIMARY KEY CHECK (id = 1),
    last_at INTEGER NOT NULL
);
INSERT INTO trash_sweep (id, last_at) VALUES (1, 0);

-- +goose Down
DROP TABLE trash_sweep;
DROP TABLE trash;
