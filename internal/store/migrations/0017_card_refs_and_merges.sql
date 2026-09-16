-- +goose Up
-- A card's ref is data, not a rendering of its project's key: after a project
-- merge, API-12 lives in MONO and is still called API-12. The index is global
-- because a ref's prefix is a key, keys are unique, and a merged key stays
-- reserved in merged_project below.
ALTER TABLE card ADD COLUMN ref TEXT NOT NULL DEFAULT '';
UPDATE card SET ref = (SELECT key FROM project WHERE project.id = card.project_id) || '-' || seq;
CREATE UNIQUE INDEX card_ref ON card(ref);

-- A key retired by a merge. A pin, --project or address naming it fails with a
-- pointer to where its contents went, and the key is never reused while cards
-- still carry it. Deleting the survivor frees it; those cards are gone too.
CREATE TABLE merged_project (
    key       TEXT PRIMARY KEY,
    into_id   TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    merged_at INTEGER NOT NULL
);

-- +goose Down
DROP TABLE merged_project;
DROP INDEX card_ref;
ALTER TABLE card DROP COLUMN ref;
