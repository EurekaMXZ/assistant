BEGIN;

ALTER TABLE context_heads
    DROP COLUMN IF EXISTS latest_checkpoint_checksum;

ALTER TABLE turn_runs
    ALTER COLUMN attempt SET DEFAULT 0,
    DROP COLUMN IF EXISTS artifact_metadata,
    DROP COLUMN IF EXISTS failure_blob_key;

COMMIT;
