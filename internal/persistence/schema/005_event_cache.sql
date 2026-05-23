CREATE TABLE IF NOT EXISTS event_cache (
    event_id   TEXT    NOT NULL,
    account_id TEXT    NOT NULL,
    seen_at    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (event_id, account_id)
);

CREATE INDEX IF NOT EXISTS idx_event_cache_seen_at
    ON event_cache (seen_at);
