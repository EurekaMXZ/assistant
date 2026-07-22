BEGIN;

ALTER TABLE turns
    DROP CONSTRAINT turns_status_check,
    ADD CONSTRAINT turns_status_check
        CHECK (status IN ('accepted', 'context_ready', 'processing', 'awaiting_input', 'cancel_requested', 'completed', 'failed', 'cancelled'));

ALTER TABLE turn_runs
    DROP CONSTRAINT turn_runs_status_check,
    ADD CONSTRAINT turn_runs_status_check
        CHECK (status IN ('queued', 'running', 'awaiting_input', 'cancel_requested', 'completed', 'failed', 'cancelled')),
    ADD COLUMN billing_settled_at timestamptz;

ALTER TABLE tool_calls
    DROP CONSTRAINT tool_calls_status_check,
    ADD CONSTRAINT tool_calls_status_check
        CHECK (status IN ('running', 'awaiting_input', 'completed', 'failed', 'cancelled')),
    ADD COLUMN answer_idempotency_key text
        CHECK (answer_idempotency_key IS NULL OR length(btrim(answer_idempotency_key)) BETWEEN 1 AND 128),
    ADD COLUMN answer_fingerprint text
        CHECK (answer_fingerprint IS NULL OR answer_fingerprint ~ '^[0-9a-f]{64}$'),
    ADD COLUMN answer_option_id text
        CHECK (answer_option_id IS NULL OR answer_option_id ~ '^[A-Za-z0-9_-]{1,64}$'),
    ADD COLUMN answer_output_pending boolean NOT NULL DEFAULT false,
    ADD COLUMN cancelled_at timestamptz,
    ADD CONSTRAINT tool_calls_answer_declaration_check CHECK (
        (
            answer_idempotency_key IS NULL
            AND answer_fingerprint IS NULL
            AND answer_option_id IS NULL
            AND answer_output_pending = false
        )
        OR (
            answer_idempotency_key IS NOT NULL
            AND answer_fingerprint IS NOT NULL
            AND answer_option_id IS NOT NULL
            AND output_blob_key IS NOT NULL
        )
    );

COMMIT;
