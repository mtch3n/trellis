-- +goose Up
CREATE TABLE project (
    id             TEXT PRIMARY KEY,
    key            TEXT NOT NULL UNIQUE,
    identity_kind  TEXT NOT NULL,
    identity_value TEXT,
    root_path      TEXT UNIQUE,   -- rebinding matches on it; duplicates would
                                  -- make that lookup return two rows
    name           TEXT NOT NULL,
    created_at     INTEGER NOT NULL
);
CREATE INDEX project_identity ON project(identity_value);
CREATE INDEX project_root ON project(root_path);

CREATE TABLE board (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    UNIQUE (project_id, name),
    UNIQUE (project_id, slug)
);

CREATE TABLE column_ (
    id       TEXT PRIMARY KEY,
    board_id TEXT NOT NULL REFERENCES board(id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    position INTEGER NOT NULL,
    is_done  INTEGER NOT NULL DEFAULT 0,
    UNIQUE (board_id, name)
);

CREATE TABLE card (
    id          TEXT PRIMARY KEY,
    -- project_id is denormalized alongside board_id: seq is allocated per
    -- project so a reference stays unique across boards, and lookup by seq
    -- must not need to know which board holds the card.
    project_id  TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    board_id    TEXT NOT NULL REFERENCES board(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    column_id   TEXT NOT NULL REFERENCES column_(id),
    rank        TEXT NOT NULL,
    title       TEXT NOT NULL,
    body_md     TEXT NOT NULL DEFAULT '',
    priority    INTEGER NOT NULL DEFAULT 2,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    archived_at INTEGER,
    UNIQUE (project_id, seq)
);
CREATE INDEX card_column ON card(column_id, priority, rank);

CREATE TABLE event (
    seq         INTEGER PRIMARY KEY AUTOINCREMENT,
    ts          INTEGER NOT NULL,
    actor       TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id   TEXT NOT NULL,
    action      TEXT NOT NULL,
    field       TEXT,
    old_value   TEXT,
    new_value   TEXT
);
CREATE INDEX event_entity ON event(entity_type, entity_id);

-- +goose Down
DROP TABLE event;
DROP TABLE card;
DROP TABLE column_;
DROP TABLE board;
DROP TABLE project;
