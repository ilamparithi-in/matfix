CREATE TABLE IF NOT EXISTS correlation_state (
    id                TEXT    NOT NULL PRIMARY KEY,
    type              TEXT    NOT NULL,
    account_id        TEXT    NOT NULL,
    room_id           TEXT    NOT NULL,
    outbound_event_id TEXT    NOT NULL DEFAULT '',
    filter_json       TEXT    NOT NULL DEFAULT '{}',
    timeout_at        INTEGER NOT NULL DEFAULT 0,
    state             TEXT    NOT NULL DEFAULT 'pending',
    created_at        INTEGER NOT NULL DEFAULT 0,
    updated_at        INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_correlation_timeout
    ON correlation_state (state, timeout_at);
