-- +goose Up
-- A card's title and body at a point in time. A card's version also goes up
-- on moves, lease changes and archiving, none of which touch title or body,
-- so revision numbers have gaps -- they are the card's version at capture
-- time, not a dense count. Deletion of the card cascades here: a revision
-- has no life of its own once the card is gone.
CREATE TABLE card_revision (
    card_id    TEXT    NOT NULL REFERENCES card(id) ON DELETE CASCADE,
    version    INTEGER NOT NULL,
    title      TEXT    NOT NULL,
    body_md    TEXT    NOT NULL,
    actor      TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (card_id, version)
);

-- +goose Down
DROP TABLE card_revision;
