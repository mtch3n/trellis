-- +goose Up
-- What a project is, in at most 50 words, so a project switcher can say more
-- than its key. Empty until someone writes one.
ALTER TABLE project ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE project DROP COLUMN description;
