-- +goose Up
-- Which ingestion path produced an entry. The value is derived from the file's
-- frontmatter like every other knowledge column, so it stays rebuildable.
-- Empty means an entry written before the field existed, which is not the same
-- claim as 'authored'.
ALTER TABLE knowledge ADD COLUMN provenance TEXT NOT NULL DEFAULT '';
CREATE INDEX knowledge_provenance ON knowledge(provenance);

-- +goose Down
DROP INDEX knowledge_provenance;
ALTER TABLE knowledge DROP COLUMN provenance;
