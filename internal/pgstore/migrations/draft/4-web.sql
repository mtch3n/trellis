-- +goose Up
CREATE TABLE web_session (
    hash       bytea PRIMARY KEY CHECK (length(hash) = 32),
    token_id   uuid NOT NULL REFERENCES token (id),
    member_id  uuid NOT NULL REFERENCES member (id),
    csrf       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    ended_at   timestamptz
);
CREATE INDEX web_session_member ON web_session (member_id);

-- +goose Down
DROP TABLE web_session;
