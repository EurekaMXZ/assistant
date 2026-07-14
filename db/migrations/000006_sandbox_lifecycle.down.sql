LOCK TABLE sandboxes IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM sandboxes WHERE status IN ('stopped', 'releasing') OR execution_token IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back sandbox lifecycle migration while stopped or releasing sandboxes or active execution leases exist';
    END IF;
END
$$;

DROP INDEX idx_sandboxes_releasing;
DROP INDEX idx_sandboxes_stopped_retention;
DROP INDEX idx_sandboxes_idle_active;
DROP INDEX idx_sandboxes_conversation_usable;

ALTER TABLE sandboxes
    DROP CONSTRAINT sandboxes_lifecycle_check,
    DROP CONSTRAINT sandboxes_release_lease_check,
    DROP CONSTRAINT sandboxes_execution_lease_check,
    DROP CONSTRAINT sandboxes_status_check,
    DROP COLUMN release_previous_status,
    DROP COLUMN release_lease_until,
    DROP COLUMN release_token,
    DROP COLUMN execution_lease_until,
    DROP COLUMN execution_token,
    DROP COLUMN stopped_at,
    DROP COLUMN last_activity_at,
    ADD CONSTRAINT sandboxes_status_check CHECK (status IN ('active', 'destroyed')),
    ADD CONSTRAINT sandboxes_check CHECK (
        (status = 'active' AND destroyed_at IS NULL)
        OR (status = 'destroyed' AND destroyed_at IS NOT NULL AND destroyed_at >= created_at)
    );

CREATE UNIQUE INDEX idx_sandboxes_conversation_active
    ON sandboxes (conversation_id)
    WHERE status = 'active' AND destroyed_at IS NULL;
