LOCK TABLE sandboxes IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM sandboxes
        WHERE provider = 'agentbay' AND status <> 'destroyed'
    ) THEN
        RAISE EXCEPTION 'cannot remove AgentBay provider while non-destroyed AgentBay sandboxes exist; terminate them before retrying';
    END IF;
END
$$;

ALTER TABLE sandboxes
    ADD CONSTRAINT sandboxes_provider_check
    CHECK (
        provider = 'firecracker'
        OR (provider = 'agentbay' AND status = 'destroyed')
    );
