-- +goose Up
-- link's UNIQUE includes anchor, which is NULL on a card-to-card link, so
-- SQLite never saw two such links as equal and INSERT OR IGNORE stored both.
DELETE FROM link WHERE from_type = 'card' AND to_type = 'card' AND rowid NOT IN (
    SELECT MIN(rowid) FROM link WHERE from_type = 'card' AND to_type = 'card'
    GROUP BY from_id, to_id, rel);
CREATE UNIQUE INDEX link_card_card ON link (from_id, to_id, rel)
    WHERE from_type = 'card' AND to_type = 'card';

-- +goose Down
DROP INDEX link_card_card;
