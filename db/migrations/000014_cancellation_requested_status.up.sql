BEGIN;

ALTER TABLE turns
    DROP CONSTRAINT turns_status_check,
    ADD CONSTRAINT turns_status_check
        CHECK (status IN ('accepted', 'context_ready', 'processing', 'cancel_requested', 'completed', 'failed', 'cancelled'));

ALTER TABLE turn_runs
    DROP CONSTRAINT turn_runs_status_check,
    ADD CONSTRAINT turn_runs_status_check
        CHECK (status IN ('queued', 'running', 'cancel_requested', 'completed', 'failed', 'cancelled'));

COMMIT;
