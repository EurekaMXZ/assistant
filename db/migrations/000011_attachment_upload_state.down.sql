DROP INDEX IF EXISTS idx_attachments_upload_cleanup_updated_at;

ALTER TABLE attachments
	DROP CONSTRAINT IF EXISTS attachments_status_check,
	DROP COLUMN IF EXISTS upload_completed_at,
	DROP COLUMN IF EXISTS content_md5,
	DROP COLUMN IF EXISTS status;
