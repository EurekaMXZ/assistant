BEGIN;

DROP TRIGGER IF EXISTS attachments_enforce_user_storage ON attachments;
DROP FUNCTION IF EXISTS enforce_user_attachment_storage();

ALTER TABLE models
    DROP COLUMN IF EXISTS deleted_at;

ALTER TABLE conversations
    DROP CONSTRAINT IF EXISTS conversations_status_check,
    ADD CONSTRAINT conversations_status_check
        CHECK (status IN ('active', 'archived')),
    DROP CONSTRAINT IF EXISTS conversations_lifecycle_check,
    ADD CONSTRAINT conversations_check
        CHECK (
            (status = 'active' AND archived_at IS NULL)
            OR (status = 'archived' AND archived_at IS NOT NULL AND archived_at >= created_at)
        ),
    DROP COLUMN IF EXISTS deleted_at;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_storage_quota_bytes_check,
    DROP CONSTRAINT IF EXISTS users_storage_used_bytes_check,
    DROP COLUMN IF EXISTS deleted_at,
    DROP COLUMN IF EXISTS storage_used_bytes,
    DROP COLUMN IF EXISTS storage_quota_bytes;

COMMIT;
