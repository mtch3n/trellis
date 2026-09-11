-- +goose Up
CREATE TABLE invocation (
    seq         INTEGER PRIMARY KEY AUTOINCREMENT,
    ts          INTEGER NOT NULL,
    actor       TEXT NOT NULL,
    argv        TEXT NOT NULL,
    exit_code   INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL
);
CREATE INDEX invocation_actor ON invocation(actor);
CREATE INDEX invocation_ts ON invocation(ts DESC);

-- +goose Down
DROP TABLE invocation;
