CREATE TABLE "project" (
    id         TEXT PRIMARY KEY,
    key        TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    created_at INTEGER NOT NULL
		  , description TEXT NOT NULL DEFAULT '');

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
    archived_at INTEGER, claimed_by TEXT, claim_until INTEGER, ref TEXT NOT NULL DEFAULT '',
    UNIQUE (project_id, seq)
);

CREATE TABLE "comment" (
    id        TEXT PRIMARY KEY,
    card_id   TEXT NOT NULL REFERENCES card(id) ON DELETE CASCADE,
    actor     TEXT NOT NULL,
    body_md   TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE "entry" (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    board_id     TEXT REFERENCES board(id) ON DELETE SET NULL,
    slug         TEXT NOT NULL,
    title        TEXT NOT NULL,
    template     TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    recap        TEXT,
    recap_hash   TEXT,
    content_hash TEXT NOT NULL,
    mtime        INTEGER NOT NULL,
    size         INTEGER NOT NULL,
    global       INTEGER NOT NULL DEFAULT 0,
    verify_by    INTEGER,
    verified_at  INTEGER,
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    provenance   TEXT NOT NULL DEFAULT '',
    private      INTEGER NOT NULL DEFAULT 0,
    UNIQUE (project_id, slug)
);

CREATE TABLE link (
    from_type TEXT NOT NULL,
    from_id   TEXT NOT NULL,
    to_type   TEXT NOT NULL,
    to_id     TEXT,
    to_raw    TEXT NOT NULL,
    anchor    TEXT,
    rel       TEXT NOT NULL,
    UNIQUE (from_type, from_id, to_type, to_raw, anchor, rel)
);

CREATE TABLE pin (
    id           TEXT PRIMARY KEY,
    entry_id TEXT NOT NULL REFERENCES "entry"(id) ON DELETE CASCADE,
    board_id     TEXT REFERENCES board(id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    UNIQUE (entry_id, board_id)
);

CREATE TABLE nomination (
    id           TEXT PRIMARY KEY,
    entry_id TEXT NOT NULL REFERENCES "entry"(id) ON DELETE CASCADE,
    actor        TEXT NOT NULL,
    reason       TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    UNIQUE (entry_id, actor)
);

CREATE TABLE "artifact" (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL,
    mime         TEXT NOT NULL,
    size         INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE label (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE card_label (
    card_id   TEXT NOT NULL REFERENCES card(id) ON DELETE CASCADE,
    label_id  TEXT NOT NULL REFERENCES label(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, label_id)
);

CREATE TABLE tag (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE card_tag (
    card_id  TEXT NOT NULL REFERENCES card(id) ON DELETE CASCADE,
    tag_id   TEXT NOT NULL REFERENCES tag(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, tag_id)
);

CREATE TABLE "entry_label" (
    entry_id   TEXT NOT NULL REFERENCES "entry"(id) ON DELETE CASCADE,
    label_id TEXT NOT NULL REFERENCES label(id) ON DELETE CASCADE,
    PRIMARY KEY (entry_id, label_id)
);

CREATE TABLE "entry_tag" (
    entry_id TEXT NOT NULL REFERENCES "entry"(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL REFERENCES tag(id) ON DELETE CASCADE,
    PRIMARY KEY (entry_id, tag_id)
);

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
, project_id TEXT);

CREATE TABLE trash (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL,
    project_key TEXT NOT NULL,
    kind        TEXT NOT NULL,
    item_id     TEXT NOT NULL,
    name        TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    seq         INTEGER,
    trashed_at  INTEGER NOT NULL,
    trashed_by  TEXT NOT NULL,
    rows        TEXT NOT NULL,
    files       TEXT NOT NULL DEFAULT '[]'
);
