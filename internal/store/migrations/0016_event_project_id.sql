-- +goose Up
-- Events now carry their project_id so a project-scoped feed can reach
-- hard-deleted entities' history without joining the live row. Backfill
-- using the same logic EventFeed uses today.
ALTER TABLE event ADD COLUMN project_id TEXT;
CREATE INDEX event_project_seq ON event(project_id, seq);

-- Backfill from the entity's live row, following EventFeed's joins.
-- Card events: join to card, then to project.
UPDATE event
SET project_id = (
    SELECT pc.id
    FROM card c
    LEFT JOIN project pc ON pc.id = c.project_id
    WHERE c.id = event.entity_id AND event.entity_type = 'card'
)
WHERE entity_type = 'card';

-- Knowledge events: join to knowledge, then to project.
UPDATE event
SET project_id = (
    SELECT pk.id
    FROM knowledge k
    LEFT JOIN project pk ON pk.id = k.project_id
    WHERE k.id = event.entity_id AND event.entity_type = 'knowledge'
)
WHERE entity_type = 'knowledge';

-- Board events: join to board, then to project.
UPDATE event
SET project_id = (
    SELECT b.project_id
    FROM board b
    WHERE b.id = event.entity_id AND event.entity_type = 'board'
)
WHERE entity_type = 'board';

-- Label events: join to label, then to project.
UPDATE event
SET project_id = (
    SELECT l.project_id
    FROM label l
    WHERE l.id = event.entity_id AND event.entity_type = 'label'
)
WHERE entity_type = 'label';

-- Note events: join to note, then to card, then to project.
UPDATE event
SET project_id = (
    SELECT nc.project_id
    FROM note n
    LEFT JOIN card nc ON nc.id = n.card_id
    WHERE n.id = event.entity_id AND event.entity_type = 'note'
)
WHERE entity_type = 'note';

-- +goose Down
DROP INDEX event_project_seq;
ALTER TABLE event DROP COLUMN project_id;
