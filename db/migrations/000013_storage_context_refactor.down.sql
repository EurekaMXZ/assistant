BEGIN;

DROP TABLE IF EXISTS conversation_events;

ALTER TABLE context_heads
    DROP COLUMN IF EXISTS context_schema_version,
    DROP COLUMN IF EXISTS last_context_event_seq,
    DROP COLUMN IF EXISTS checkpoint_covered_event_seq,
    DROP COLUMN IF EXISTS latest_checkpoint_key,
    DROP COLUMN IF EXISTS latest_successful_run_id,
    DROP COLUMN IF EXISTS latest_request_run_id,
    DROP COLUMN IF EXISTS version;

ALTER TABLE turn_runs
    DROP CONSTRAINT IF EXISTS turn_runs_turn_id_step_index_attempt_key,
    ADD CONSTRAINT turn_runs_turn_id_step_index_key
        UNIQUE (turn_id, step_index),
    DROP COLUMN IF EXISTS cancelled_at,
    DROP COLUMN IF EXISTS response_schema_version,
    DROP COLUMN IF EXISTS request_schema_version,
    DROP COLUMN IF EXISTS response_size_bytes,
    DROP COLUMN IF EXISTS request_size_bytes,
    DROP COLUMN IF EXISTS response_checksum,
    DROP COLUMN IF EXISTS request_checksum,
    DROP COLUMN IF EXISTS checkpoint_blob_key,
    DROP COLUMN IF EXISTS presentation_events_blob_key,
    DROP COLUMN IF EXISTS tool_results_blob_key,
    DROP COLUMN IF EXISTS output_items_blob_key;

ALTER TABLE turn_runs
    DROP CONSTRAINT IF EXISTS turn_runs_status_check,
    ADD CONSTRAINT turn_runs_status_check
        CHECK (status IN ('queued', 'running', 'completed', 'failed'));

ALTER TABLE turns
    DROP COLUMN IF EXISTS cancelled_at,
    DROP COLUMN IF EXISTS cancel_requested_at,
    DROP CONSTRAINT IF EXISTS turns_status_check,
    ADD CONSTRAINT turns_status_check
        CHECK (status IN ('accepted', 'context_ready', 'processing', 'completed', 'failed'));

COMMIT;
