CREATE TABLE IF NOT EXISTS outbound_queue (
    id               TEXT    NOT NULL PRIMARY KEY,
    account_id       TEXT    NOT NULL,
    room_id          TEXT    NOT NULL,
    payload          TEXT    NOT NULL,
    state            TEXT    NOT NULL DEFAULT 'accepted',
    retry_count      INTEGER NOT NULL DEFAULT 0,
    scheduled_at     INTEGER NOT NULL DEFAULT 0,
    idempotency_key  TEXT    NOT NULL DEFAULT '',
    created_at       INTEGER NOT NULL DEFAULT 0,
    updated_at       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_outbound_queue_pull
    ON outbound_queue (account_id, state, scheduled_at);

CREATE UNIQUE INDEX IF NOT EXISTS idx_outbound_queue_idempotency
    ON outbound_queue (account_id, idempotency_key)
    WHERE idempotency_key != '';
