BEGIN;

ALTER TABLE turn_runs
    ADD COLUMN failure_blob_key text,
    ADD COLUMN artifact_metadata jsonb NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(artifact_metadata) = 'object'),
    ALTER COLUMN attempt SET DEFAULT 1;

ALTER TABLE context_heads
    ADD COLUMN latest_checkpoint_checksum text;

COMMIT;
