ALTER TABLE sandboxes
    DROP CONSTRAINT sandboxes_status_check,
    DROP CONSTRAINT sandboxes_check;

ALTER TABLE sandboxes
    ADD COLUMN last_activity_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN stopped_at timestamptz,
    ADD COLUMN execution_token uuid,
    ADD COLUMN execution_lease_until timestamptz,
    ADD COLUMN release_previous_status text,
    ADD COLUMN release_token uuid,
    ADD COLUMN release_lease_until timestamptz,
    ADD CONSTRAINT sandboxes_status_check
        CHECK (status IN ('active', 'stopped', 'releasing', 'destroyed')),
    ADD CONSTRAINT sandboxes_execution_lease_check
        CHECK ((execution_token IS NULL) = (execution_lease_until IS NULL)),
    ADD CONSTRAINT sandboxes_release_lease_check
        CHECK ((release_token IS NULL) = (release_lease_until IS NULL)),
    ADD CONSTRAINT sandboxes_lifecycle_check
        CHECK (
            (
                status = 'active' AND stopped_at IS NULL AND destroyed_at IS NULL
                AND release_previous_status IS NULL AND release_token IS NULL
            )
            OR (
                status = 'stopped' AND stopped_at IS NOT NULL AND destroyed_at IS NULL
                AND release_previous_status IS NULL AND execution_token IS NULL AND release_token IS NULL
            )
            OR (
                status = 'releasing'
                AND destroyed_at IS NULL
                AND execution_token IS NULL
                AND release_previous_status IN ('active', 'stopped')
                AND ((release_previous_status = 'active' AND stopped_at IS NULL) OR (release_previous_status = 'stopped' AND stopped_at IS NOT NULL))
            )
            OR (
                status = 'destroyed' AND destroyed_at IS NOT NULL AND destroyed_at >= created_at
                AND release_previous_status IS NULL AND execution_token IS NULL AND release_token IS NULL
            )
        );

UPDATE sandboxes
SET last_activity_at = now();

DROP INDEX idx_sandboxes_conversation_active;

CREATE UNIQUE INDEX idx_sandboxes_conversation_usable
    ON sandboxes (conversation_id)
    WHERE status IN ('active', 'stopped', 'releasing') AND destroyed_at IS NULL;

CREATE INDEX idx_sandboxes_idle_active
    ON sandboxes (last_activity_at, id)
    WHERE status = 'active' AND destroyed_at IS NULL;

CREATE INDEX idx_sandboxes_stopped_retention
    ON sandboxes (stopped_at, id)
    WHERE status = 'stopped' AND destroyed_at IS NULL;

CREATE INDEX idx_sandboxes_releasing
    ON sandboxes (updated_at, id)
    WHERE status = 'releasing' AND destroyed_at IS NULL;
