-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE vault_template (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES project (id),
    name       text NOT NULL,
    raw        text NOT NULL,
    built_in   boolean NOT NULL DEFAULT false,
    version    bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE TABLE entry (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           uuid NOT NULL,
    project_id       uuid NOT NULL,
    owner_id         uuid NOT NULL,
    scope            text NOT NULL DEFAULT 'personal' CHECK (scope IN ('personal', 'project', 'global')),
    slug             text NOT NULL CHECK (slug <> ''),
    title            text NOT NULL CHECK (title <> ''),
    summary          text NOT NULL DEFAULT '',
    body             text NOT NULL DEFAULT '',
    template         text NOT NULL DEFAULT '',
    provenance       text NOT NULL DEFAULT 'authored' CHECK (provenance IN ('authored', 'prompted', 'extracted')),
    private          boolean NOT NULL DEFAULT false,
    tags             text[] NOT NULL DEFAULT '{}',
    labels           text[] NOT NULL DEFAULT '{}',
    sources          text[] NOT NULL DEFAULT '{}',
    fields           jsonb NOT NULL DEFAULT '{}',
    recap            text,
    version          bigint NOT NULL DEFAULT 1,
    verify_by        timestamptz,
    verified_at      timestamptz,
    promoted_by      text,
    promote_reason   text,
    promoted_at      timestamptz,
    embedded_version bigint NOT NULL DEFAULT 0,
    embed_error      text,
    embed_tried_at   timestamptz,
    created_by       text NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    deleted_at       timestamptz,
    deleted_by       text,
    body_key         text GENERATED ALWAYS AS (md5(btrim(regexp_replace(lower(body), '\s+', ' ', 'g')))) STORED,
    search           tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('english', title), 'A') ||
        setweight(to_tsvector('english', summary), 'B') ||
        to_tsvector('english', body)) STORED,
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id),
    FOREIGN KEY (org_id, owner_id) REFERENCES member (org_id, id),
    CHECK ((scope = 'global') = (verify_by IS NOT NULL)),
    CHECK ((deleted_at IS NULL) = (deleted_by IS NULL))
);
CREATE UNIQUE INDEX entry_personal_slug ON entry (project_id, owner_id, slug) WHERE scope = 'personal' AND deleted_at IS NULL;
CREATE UNIQUE INDEX entry_project_slug ON entry (project_id, slug) WHERE scope = 'project' AND deleted_at IS NULL;
CREATE UNIQUE INDEX entry_global_slug ON entry (org_id, slug) WHERE scope = 'global' AND deleted_at IS NULL;
CREATE UNIQUE INDEX entry_project_body ON entry (project_id, body_key) WHERE scope = 'project' AND deleted_at IS NULL;
CREATE INDEX entry_search ON entry USING gin (search);
CREATE INDEX entry_project ON entry (project_id, updated_at DESC);
CREATE INDEX entry_global ON entry (org_id) WHERE scope = 'global';
CREATE INDEX entry_unembedded ON entry (id) WHERE embedded_version < version AND deleted_at IS NULL;

CREATE TABLE entry_revision (
    entry_id   uuid NOT NULL REFERENCES entry (id) ON DELETE CASCADE,
    version    bigint NOT NULL CHECK (version > 0),
    title      text NOT NULL,
    summary    text NOT NULL,
    body       text NOT NULL,
    meta       jsonb NOT NULL DEFAULT '{}',
    actor      text NOT NULL,
    member_id  uuid REFERENCES member (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (entry_id, version)
);

CREATE TABLE entry_link (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    from_entry_id uuid NOT NULL REFERENCES entry (id) ON DELETE CASCADE,
    writer_id     uuid NOT NULL REFERENCES member (id),
    rel           text NOT NULL CHECK (rel IN ('wikilink', 'source')),
    target        text NOT NULL,
    to_entry_id   uuid REFERENCES entry (id) ON DELETE SET NULL,
    to_card_id    uuid REFERENCES card (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (from_entry_id, rel, target),
    CHECK (to_entry_id IS NULL OR to_card_id IS NULL)
);
CREATE INDEX entry_link_to_entry ON entry_link (to_entry_id);
CREATE INDEX entry_link_stub ON entry_link (target) WHERE to_entry_id IS NULL AND to_card_id IS NULL;

CREATE TABLE card_entry (
    card_id    uuid NOT NULL REFERENCES card (id) ON DELETE CASCADE,
    entry_id   uuid NOT NULL REFERENCES entry (id) ON DELETE CASCADE,
    member_id  uuid NOT NULL REFERENCES member (id),
    actor      text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (card_id, entry_id)
);
CREATE INDEX card_entry_entry ON card_entry (entry_id);

CREATE TABLE entry_chunk (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    entry_id   uuid NOT NULL REFERENCES entry (id) ON DELETE CASCADE,
    version    bigint NOT NULL,
    seq        integer NOT NULL CHECK (seq >= 0),
    text       text NOT NULL,
    model      text NOT NULL,
    embedding  vector NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (entry_id, seq)
);

CREATE TABLE nomination (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES project (id),
    entry_id   uuid NOT NULL REFERENCES entry (id) ON DELETE CASCADE,
    member_id  uuid NOT NULL REFERENCES member (id),
    actor      text NOT NULL,
    reason     text NOT NULL CHECK (reason <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (entry_id, actor)
);
CREATE INDEX nomination_project ON nomination (project_id);

CREATE TABLE pin (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES project (id),
    entry_id   uuid NOT NULL REFERENCES entry (id) ON DELETE CASCADE,
    board_id   uuid REFERENCES board (id) ON DELETE CASCADE,
    recap      text,
    member_id  uuid NOT NULL REFERENCES member (id),
    actor      text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (project_id, entry_id, board_id)
);

CREATE TABLE artifact (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL,
    project_id   uuid NOT NULL,
    owner_id     uuid NOT NULL,
    scope        text NOT NULL DEFAULT 'personal' CHECK (scope IN ('personal', 'project')),
    name         text NOT NULL CHECK (name <> ''),
    kind         text NOT NULL,
    mime         text NOT NULL,
    size         bigint NOT NULL CHECK (size >= 0),
    content_hash text NOT NULL,
    content      bytea NOT NULL,
    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz,
    deleted_by   text,
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id),
    FOREIGN KEY (org_id, owner_id) REFERENCES member (org_id, id),
    CHECK (size = length(content)),
    CHECK ((deleted_at IS NULL) = (deleted_by IS NULL))
);
CREATE UNIQUE INDEX artifact_personal_name ON artifact (project_id, owner_id, name) WHERE scope = 'personal' AND deleted_at IS NULL;
CREATE UNIQUE INDEX artifact_project_name ON artifact (project_id, name) WHERE scope = 'project' AND deleted_at IS NULL;

CREATE TABLE artifact_link (
    artifact_id uuid NOT NULL REFERENCES artifact (id) ON DELETE CASCADE,
    card_id     uuid REFERENCES card (id) ON DELETE CASCADE,
    entry_id    uuid REFERENCES entry (id) ON DELETE CASCADE,
    member_id   uuid NOT NULL REFERENCES member (id),
    actor       text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (num_nonnulls(card_id, entry_id) = 1),
    UNIQUE NULLS NOT DISTINCT (artifact_id, card_id, entry_id)
);
CREATE INDEX artifact_link_card ON artifact_link (card_id) WHERE card_id IS NOT NULL;
CREATE INDEX artifact_link_entry ON artifact_link (entry_id) WHERE entry_id IS NOT NULL;

ALTER TABLE event ADD COLUMN entry_id uuid REFERENCES entry (id) ON DELETE CASCADE;
ALTER TABLE event ADD COLUMN artifact_id uuid REFERENCES artifact (id) ON DELETE CASCADE;
CREATE INDEX event_entry ON event (entry_id, id) WHERE entry_id IS NOT NULL;
CREATE INDEX event_artifact ON event (artifact_id, id) WHERE artifact_id IS NOT NULL;

-- +goose Down
DROP INDEX event_artifact;
DROP INDEX event_entry;
ALTER TABLE event DROP COLUMN artifact_id;
ALTER TABLE event DROP COLUMN entry_id;
DROP TABLE artifact_link;
DROP TABLE artifact;
DROP TABLE pin;
DROP TABLE nomination;
DROP TABLE entry_chunk;
DROP TABLE card_entry;
DROP TABLE entry_link;
DROP TABLE entry_revision;
DROP TABLE entry;
DROP TABLE vault_template;
