-- +goose Up
-- Whether the author has declared this body not-for-automatic-distribution.
-- Derived from the file's frontmatter like every other knowledge column. It is
-- a mirror and never the authority: code that selects documents for a
-- disclosure-bearing purpose refreshes from the file first, because this value
-- is stale for exactly one read after the file changes.
--
-- Deliberately no index. Nothing may filter on this column in SQL — see the
-- Global Constraints — so an index on it could never be used.
ALTER TABLE knowledge ADD COLUMN private INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE knowledge DROP COLUMN private;
