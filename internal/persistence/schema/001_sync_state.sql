CREATE TABLE IF NOT EXISTS sync_state (
    account_id  TEXT    NOT NULL PRIMARY KEY,
    next_batch  TEXT    NOT NULL DEFAULT '',
    updated_at  INTEGER NOT NULL DEFAULT 0
);