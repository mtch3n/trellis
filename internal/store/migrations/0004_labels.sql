-- +goose Up
-- Labels are a controlled enum: explicit creation, description required, hard reject on unknown value
CREATE TABLE label (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    UNIQUE (project_id, name)
);
CREATE INDEX label_project ON label(project_id);

-- card_label junction table: many cards can have many labels
CREATE TABLE card_label (
    card_id   TEXT NOT NULL REFERENCES card(id) ON DELETE CASCADE,
    label_id  TEXT NOT NULL REFERENCES label(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, label_id)
);
CREATE INDEX card_label_label ON card_label(label_id);

-- Tags are free-form: implicit creation, no description, auto-created on first use
CREATE TABLE tag (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    UNIQUE (project_id, name)
);
CREATE INDEX tag_project ON tag(project_id);

-- card_tag junction table: many cards can have many tags
CREATE TABLE card_tag (
    card_id  TEXT NOT NULL REFERENCES card(id) ON DELETE CASCADE,
    tag_id   TEXT NOT NULL REFERENCES tag(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, tag_id)
);
CREATE INDEX card_tag_tag ON card_tag(tag_id);

-- +goose Down
DROP TABLE card_tag;
DROP TABLE tag;
DROP TABLE card_label;
DROP TABLE label;
