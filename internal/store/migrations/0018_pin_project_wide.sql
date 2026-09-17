-- +goose Up
-- UNIQUE (knowledge_id, board_id) treats NULL boards as distinct, so a
-- project-wide pin could be stored twice. Keep the newest of any duplicates.
DELETE FROM pin WHERE board_id IS NULL AND rowid NOT IN (
    SELECT MAX(rowid) FROM pin WHERE board_id IS NULL GROUP BY knowledge_id);
CREATE UNIQUE INDEX pin_project_wide ON pin (knowledge_id) WHERE board_id IS NULL;

-- +goose Down
DROP INDEX pin_project_wide;
