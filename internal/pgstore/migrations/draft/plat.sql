-- +goose Up
CREATE TABLE org (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE member (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES org (id),
    handle     text NOT NULL,
    name       text NOT NULL DEFAULT '',
    role       text NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, handle),
    UNIQUE (org_id, id)
);

CREATE TABLE project (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL REFERENCES org (id),
    key          text NOT NULL,
    name         text NOT NULL DEFAULT '',
    card_counter bigint NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, key),
    UNIQUE (org_id, id)
);

CREATE TABLE project_member (
    org_id     uuid NOT NULL,
    project_id uuid NOT NULL,
    member_id  uuid NOT NULL,
    role       text NOT NULL DEFAULT 'member' CHECK (role IN ('maintainer', 'member')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, member_id),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id),
    FOREIGN KEY (org_id, member_id) REFERENCES member (org_id, id)
);

CREATE TABLE token (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    member_id  uuid NOT NULL REFERENCES member (id),
    name       text NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('agent', 'person')),
    hash       bytea NOT NULL UNIQUE CHECK (length(hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);
CREATE UNIQUE INDEX token_live_name ON token (member_id, name) WHERE revoked_at IS NULL;

CREATE TABLE board (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES project (id),
    owner_id   uuid NOT NULL REFERENCES member (id),
    name       text NOT NULL,
    scope      text NOT NULL DEFAULT 'personal' CHECK (scope IN ('personal', 'project')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, owner_id, name),
    UNIQUE (project_id, id)
);

CREATE TABLE board_column (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    board_id   uuid NOT NULL REFERENCES board (id) ON DELETE CASCADE,
    name       text NOT NULL,
    position   integer NOT NULL,
    done       boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (board_id, name),
    UNIQUE (board_id, id)
);

CREATE TABLE card (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        uuid NOT NULL,
    board_id          uuid NOT NULL,
    column_id         uuid NOT NULL,
    number            bigint NOT NULL CHECK (number > 0),
    ref               text NOT NULL,
    title             text NOT NULL,
    body              text NOT NULL DEFAULT '',
    position          double precision NOT NULL DEFAULT 0,
    version           bigint NOT NULL DEFAULT 1,
    claimed_by        text,
    claimed_member_id uuid REFERENCES member (id),
    claim_until       timestamptz,
    created_by        text NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, number),
    UNIQUE (project_id, ref),
    FOREIGN KEY (project_id, board_id) REFERENCES board (project_id, id),
    FOREIGN KEY (board_id, column_id) REFERENCES board_column (board_id, id),
    CHECK ((claimed_by IS NULL) = (claim_until IS NULL)),
    CHECK ((claimed_by IS NULL) = (claimed_member_id IS NULL))
);
CREATE INDEX card_board ON card (board_id, column_id, position);

CREATE TABLE comment (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    card_id    uuid NOT NULL REFERENCES card (id) ON DELETE CASCADE,
    member_id  uuid NOT NULL REFERENCES member (id),
    actor      text NOT NULL,
    body       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX comment_card ON comment (card_id, created_at);

CREATE TABLE blocker (
    card_id    uuid NOT NULL REFERENCES card (id) ON DELETE CASCADE,
    blocker_id uuid NOT NULL REFERENCES card (id) ON DELETE CASCADE,
    member_id  uuid NOT NULL REFERENCES member (id),
    actor      text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (card_id, blocker_id),
    CHECK (card_id <> blocker_id)
);
CREATE INDEX blocker_blocker ON blocker (blocker_id);

CREATE TABLE event (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES org (id),
    project_id uuid REFERENCES project (id),
    board_id   uuid REFERENCES board (id) ON DELETE CASCADE,
    card_id    uuid REFERENCES card (id) ON DELETE CASCADE,
    kind       text NOT NULL,
    actor      text NOT NULL,
    member_id  uuid REFERENCES member (id),
    data       jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX event_card ON event (card_id, id);
CREATE INDEX event_board ON event (board_id, id);

CREATE TABLE invocation (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id      uuid REFERENCES org (id),
    member_id   uuid REFERENCES member (id),
    token_id    uuid REFERENCES token (id),
    actor       text NOT NULL,
    tool        text NOT NULL,
    action      text NOT NULL DEFAULT '',
    arg_names   text[] NOT NULL DEFAULT '{}',
    code        text NOT NULL,
    duration_ms integer NOT NULL CHECK (duration_ms >= 0),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX invocation_member ON invocation (member_id, created_at);

-- +goose Down
DROP TABLE invocation;
DROP TABLE event;
DROP TABLE blocker;
DROP TABLE comment;
DROP TABLE card;
DROP TABLE board_column;
DROP TABLE board;
DROP TABLE token;
DROP TABLE project_member;
DROP TABLE project;
DROP TABLE member;
DROP TABLE org;
