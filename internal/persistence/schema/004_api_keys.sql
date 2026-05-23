CREATE TABLE IF NOT EXISTS api_keys (
    id               TEXT    NOT NULL PRIMARY KEY,
    key_hash         TEXT    NOT NULL UNIQUE,
    name             TEXT    NOT NULL,
    permissions_json TEXT    NOT NULL DEFAULT '{}',
    created_at       INTEGER NOT NULL DEFAULT 0,
    revoked_at       INTEGER
);
