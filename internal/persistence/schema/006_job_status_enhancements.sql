-- Migration to add matrix_event_id and resolved_event_ids columns to support status tracking.
ALTER TABLE outbound_queue ADD COLUMN matrix_event_id TEXT NOT NULL DEFAULT '';
ALTER TABLE correlation_state ADD COLUMN resolved_event_ids TEXT NOT NULL DEFAULT '';
