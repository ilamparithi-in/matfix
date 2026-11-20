ALTER TABLE outbound_queue ADD COLUMN last_error TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_outbound_queue_dead_letter
    ON outbound_queue (account_id, updated_at DESC)
    WHERE state = 'dead_letter';

