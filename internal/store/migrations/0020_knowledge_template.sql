-- +goose Up
-- Rename doc_type to template throughout the knowledge table.
-- The column keeps its old DEFAULT 'note'; code always writes the value explicitly.
ALTER TABLE knowledge RENAME COLUMN doc_type TO template;
UPDATE knowledge SET template = '' WHERE template = 'note';

-- +goose Down
UPDATE knowledge SET template = 'note' WHERE template = '';
ALTER TABLE knowledge RENAME COLUMN template TO doc_type;
