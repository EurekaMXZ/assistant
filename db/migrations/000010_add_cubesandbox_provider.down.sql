LOCK TABLE sandboxes IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM sandboxes
        WHERE provider = 'cubesandbox'
    ) THEN
        RAISE EXCEPTION 'cannot remove CubeSandbox provider while CubeSandbox sandbox records exist; terminate active sandboxes and archive or delete their history before retrying';
    END IF;
END
$$;

ALTER TABLE sandboxes
    DROP CONSTRAINT sandboxes_provider_check,
    ADD CONSTRAINT sandboxes_provider_check
    CHECK (
        provider = 'firecracker'
        OR (provider = 'agentbay' AND status = 'destroyed')
    );
