BEGIN;

ALTER TABLE turn_runs
    DROP CONSTRAINT turn_runs_status_check,
    ADD CONSTRAINT turn_runs_status_check
        CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled'));

ALTER TABLE turns
    DROP CONSTRAINT turns_status_check,
    ADD CONSTRAINT turns_status_check
        CHECK (status IN ('accepted', 'context_ready', 'processing', 'completed', 'failed', 'cancelled'));

COMMIT;
