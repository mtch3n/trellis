-- +goose Up
-- A consumer is a named, durable cursor into the event feed (§ event feed
-- design). Reading never advances cursor; only `ack` does, and ack never
-- moves it backwards. This gives an extension at-least-once delivery: a
-- crash between handling an event and acking it repeats the event next time.
CREATE TABLE event_consumer (
    name       TEXT PRIMARY KEY,
    cursor     INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- +goose Down
DROP TABLE event_consumer;
