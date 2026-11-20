CREATE TABLE IF NOT EXISTS webhook_queue (
    id               TEXT    NOT NULL PRIMARY KEY,
    webhook_id       TEXT    NOT NULL,
    url              TEXT    NOT NULL,
    payload          TEXT    NOT NULL,
    headers_json     TEXT    NOT NULL DEFAULT '{}',
    secret           TEXT    NOT NULL DEFAULT '',
    state            TEXT    NOT NULL DEFAULT 'failed',
    retry_count      INTEGER NOT NULL DEFAULT 1,
    max_retries      INTEGER NOT NULL DEFAULT 5,
    scheduled_at     INTEGER NOT NULL DEFAULT 0,
    last_error       TEXT    NOT NULL DEFAULT '',
    last_status_code INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL DEFAULT 0,
    updated_at       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_webhook_queue_pull
    ON webhook_queue (state, scheduled_at);

CREATE INDEX IF NOT EXISTS idx_webhook_queue_webhook_id
    ON webhook_queue (webhook_id, state);
