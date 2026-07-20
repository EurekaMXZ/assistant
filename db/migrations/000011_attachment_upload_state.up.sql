ALTER TABLE attachments
    ADD COLUMN status text NOT NULL DEFAULT 'ready',
	ADD COLUMN content_md5 text NOT NULL DEFAULT '',
    ADD COLUMN upload_completed_at timestamptz;

ALTER TABLE attachments
    ADD CONSTRAINT attachments_status_check
    CHECK (status IN ('pending', 'ready', 'deleting'));

UPDATE attachments
SET upload_completed_at = created_at
WHERE status = 'ready' AND upload_completed_at IS NULL;

CREATE INDEX idx_attachments_upload_cleanup_updated_at
    ON attachments (updated_at)
    WHERE status IN ('pending', 'deleting');
