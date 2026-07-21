BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM turns WHERE status = 'awaiting_input')
        OR EXISTS (SELECT 1 FROM turn_runs WHERE status = 'awaiting_input')
        OR EXISTS (SELECT 1 FROM tool_calls WHERE status IN ('awaiting_input', 'cancelled')) THEN
        RAISE EXCEPTION 'cannot remove ask_user waiting state while interactions are awaiting input';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM tool_calls
        WHERE answer_idempotency_key IS NOT NULL
           OR answer_fingerprint IS NOT NULL
           OR answer_option_id IS NOT NULL
           OR answer_output_pending
    ) THEN
        RAISE EXCEPTION 'cannot remove ask_user answer idempotency data';
    END IF;
END $$;

ALTER TABLE tool_calls
    DROP CONSTRAINT tool_calls_status_check,
    DROP CONSTRAINT tool_calls_answer_declaration_check,
    DROP COLUMN cancelled_at,
    DROP COLUMN answer_output_pending,
    DROP COLUMN answer_option_id,
    DROP COLUMN answer_fingerprint,
    DROP COLUMN answer_idempotency_key,
    ADD CONSTRAINT tool_calls_status_check
        CHECK (status IN ('running', 'completed', 'failed', 'ambiguous'));

ALTER TABLE turn_runs
    DROP CONSTRAINT turn_runs_status_check,
    DROP COLUMN billing_settled_at,
    ADD CONSTRAINT turn_runs_status_check
        CHECK (status IN ('queued', 'running', 'cancel_requested', 'completed', 'failed', 'cancelled'));

ALTER TABLE turns
    DROP CONSTRAINT turns_status_check,
    ADD CONSTRAINT turns_status_check
        CHECK (status IN ('accepted', 'context_ready', 'processing', 'cancel_requested', 'completed', 'failed', 'cancelled'));

COMMIT;
