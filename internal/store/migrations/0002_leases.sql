-- +goose Up
-- Add leases and agent identity to cards
ALTER TABLE card ADD COLUMN owner TEXT;
ALTER TABLE card ADD COLUMN lease_until INTEGER;

-- Agent registry: unique per session with a harness-addressable handle
CREATE TABLE agent (
    id         TEXT PRIMARY KEY,
    handle     TEXT NOT NULL,                   -- harness address for messaging
    kind       TEXT NOT NULL,                   -- "agent", "hook", etc.
    cwd        TEXT NOT NULL,                   -- working directory
    host       TEXT NOT NULL,                   -- hostname
    pid        INTEGER NOT NULL,                -- process id
    first_seen INTEGER NOT NULL,                -- when the agent first appeared
    last_seen  INTEGER NOT NULL                 -- heartbeat for liveness
);
CREATE INDEX agent_handle ON agent(handle);

-- Notes are append-only; each write does NOT bump card.version,
-- so an agent can leave a note on a card it doesn't hold.
CREATE TABLE note (
    id        TEXT PRIMARY KEY,
    card_id   TEXT NOT NULL REFERENCES card(id) ON DELETE CASCADE,
    actor     TEXT NOT NULL,                    -- agent.id
    body_md   TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX note_card ON note(card_id, created_at);

-- Links are the cross-boundary reference table: cards to cards, cards to docs, docs to docs.
-- For blocked_by: from_type='card', to_type='card', rel='blocked_by'
CREATE TABLE link (
    from_type TEXT NOT NULL,                    -- 'card' or 'doc'
    from_id   TEXT NOT NULL,                    -- card uuid or doc slug
    to_type   TEXT NOT NULL,                    -- 'card' or 'doc'
    to_id     TEXT,                             -- resolved to card uuid or doc slug; NULL for stub
    to_raw    TEXT NOT NULL,                    -- the literal text the author wrote
    anchor    TEXT,                             -- heading slug within the target
    rel       TEXT NOT NULL,                    -- 'blocked_by', 'links_to', etc.
    UNIQUE (from_type, from_id, to_type, to_raw, anchor, rel)
);
CREATE INDEX link_from ON link(from_type, from_id);
CREATE INDEX link_to ON link(to_type, to_id);

-- Add foreign key constraints for the new columns
-- owner is nullable; many cards start unowned
-- ALTER TABLE card ADD CONSTRAINT card_owner_fk FOREIGN KEY (owner) REFERENCES agent(id) ON DELETE SET NULL;

-- +goose Down
DROP TABLE link;
DROP TABLE note;
DROP TABLE agent;
ALTER TABLE card DROP COLUMN owner;
ALTER TABLE card DROP COLUMN lease_until;
