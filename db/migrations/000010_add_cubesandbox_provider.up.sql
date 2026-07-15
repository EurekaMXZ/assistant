ALTER TABLE sandboxes
    DROP CONSTRAINT sandboxes_provider_check,
    ADD CONSTRAINT sandboxes_provider_check
    CHECK (
        provider IN ('firecracker', 'cubesandbox')
        OR (provider = 'agentbay' AND status = 'destroyed')
    );
