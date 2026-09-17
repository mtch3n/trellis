-- +goose Up
-- Rename note table to comment, and update event records accordingly
ALTER TABLE note RENAME TO comment;

-- Drop and recreate the index with the new name
DROP INDEX note_card;
CREATE INDEX comment_card ON comment(card_id, created_at);

-- Update existing event records: entity_type = 'note' → 'comment'
UPDATE event SET entity_type = 'comment' WHERE entity_type = 'note';

-- +goose Down
UPDATE event SET entity_type = 'note' WHERE entity_type = 'comment';
DROP INDEX comment_card;
ALTER TABLE comment RENAME TO note;
CREATE INDEX note_card ON note(card_id, created_at);
