BEGIN;

ALTER TABLE users
    ADD COLUMN storage_quota_bytes bigint NOT NULL DEFAULT 536870912,
    ADD COLUMN storage_used_bytes bigint NOT NULL DEFAULT 0,
    ADD COLUMN deleted_at timestamptz,
    ADD CONSTRAINT users_storage_quota_bytes_check CHECK (storage_quota_bytes >= 0),
    ADD CONSTRAINT users_storage_used_bytes_check CHECK (storage_used_bytes >= 0);

ALTER TABLE conversations
    ADD COLUMN deleted_at timestamptz;

ALTER TABLE conversations
    DROP CONSTRAINT conversations_status_check,
    ADD CONSTRAINT conversations_status_check
        CHECK (status IN ('active', 'archived', 'deleted')),
    DROP CONSTRAINT conversations_check,
    ADD CONSTRAINT conversations_lifecycle_check
        CHECK (
            (status = 'active' AND archived_at IS NULL AND deleted_at IS NULL)
            OR (status = 'archived' AND archived_at IS NOT NULL AND deleted_at IS NULL)
            OR (status = 'deleted' AND deleted_at IS NOT NULL)
        );

ALTER TABLE models
    ADD COLUMN deleted_at timestamptz;

UPDATE users AS u
SET storage_used_bytes = COALESCE(
    (
        SELECT SUM(a.size_bytes)
        FROM attachments AS a
        WHERE a.uploaded_by_user_id = u.id
    ),
    0
);

CREATE FUNCTION enforce_user_attachment_storage()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    delta bigint;
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE users
        SET storage_used_bytes = storage_used_bytes + NEW.size_bytes
        WHERE id = NEW.uploaded_by_user_id
          AND storage_used_bytes + NEW.size_bytes <= storage_quota_bytes;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'storage quota exceeded' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    IF TG_OP = 'DELETE' THEN
        UPDATE users
        SET storage_used_bytes = storage_used_bytes - OLD.size_bytes
        WHERE id = OLD.uploaded_by_user_id;
        RETURN OLD;
    END IF;

    delta = NEW.size_bytes - OLD.size_bytes;
    IF NEW.uploaded_by_user_id IS DISTINCT FROM OLD.uploaded_by_user_id THEN
        UPDATE users
        SET storage_used_bytes = storage_used_bytes - OLD.size_bytes
        WHERE id = OLD.uploaded_by_user_id;
        UPDATE users
        SET storage_used_bytes = storage_used_bytes + NEW.size_bytes
        WHERE id = NEW.uploaded_by_user_id
          AND storage_used_bytes + NEW.size_bytes <= storage_quota_bytes;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'storage quota exceeded' USING ERRCODE = '23514';
        END IF;
    ELSIF delta > 0 THEN
        UPDATE users
        SET storage_used_bytes = storage_used_bytes + delta
        WHERE id = NEW.uploaded_by_user_id
          AND storage_used_bytes + delta <= storage_quota_bytes;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'storage quota exceeded' USING ERRCODE = '23514';
        END IF;
    ELSIF delta < 0 THEN
        UPDATE users
        SET storage_used_bytes = storage_used_bytes + delta
        WHERE id = NEW.uploaded_by_user_id;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER attachments_enforce_user_storage
BEFORE INSERT OR UPDATE OR DELETE ON attachments
FOR EACH ROW EXECUTE FUNCTION enforce_user_attachment_storage();

COMMIT;
