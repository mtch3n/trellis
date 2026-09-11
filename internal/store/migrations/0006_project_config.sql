-- +goose Up
CREATE TABLE project_config (
    project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (project_id, key)
);

-- +goose Down
DROP TABLE project_config;
