-- +goose Up
-- Artifacts are filesystem objects. SQLite stores only their metadata and
-- relationships to cards; it never stores the file bytes.
CREATE TABLE artifact (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    path         TEXT NOT NULL,
    kind         TEXT NOT NULL,
    mime         TEXT NOT NULL,
    size         INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (project_id, path)
);
CREATE INDEX artifact_project ON artifact(project_id, updated_at DESC);

-- Links use the existing graph model: card -> artifact with rel='artifact'.

-- +goose Down
DROP TABLE artifact;
