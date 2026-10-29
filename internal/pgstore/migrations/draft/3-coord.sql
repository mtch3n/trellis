-- +goose Up
ALTER TABLE card ADD COLUMN archived_at timestamptz;
ALTER TABLE card ADD COLUMN deleted_at timestamptz;

CREATE TABLE coord_notice (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      uuid NOT NULL REFERENCES project (id),
    member_id       uuid NOT NULL REFERENCES member (id),
    card_id         uuid NOT NULL REFERENCES card (id) ON DELETE CASCADE,
    other_member_id uuid NOT NULL REFERENCES member (id),
    other_card_id   uuid NOT NULL REFERENCES card (id) ON DELETE CASCADE,
    words           text[] NOT NULL,
    state           text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'dismissed', 'closed')),
    created_by      text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    ended_at        timestamptz,
    UNIQUE (card_id, other_card_id),
    CHECK (card_id <> other_card_id),
    CHECK ((state = 'open') = (ended_at IS NULL))
);
CREATE INDEX coord_notice_member ON coord_notice (member_id, project_id) WHERE state = 'open';
CREATE INDEX coord_notice_other_card ON coord_notice (other_card_id);

-- +goose Down
DROP TABLE coord_notice;
ALTER TABLE card DROP COLUMN deleted_at;
ALTER TABLE card DROP COLUMN archived_at;
