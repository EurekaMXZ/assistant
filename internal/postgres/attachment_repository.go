package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/pagination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AttachmentRepository struct {
	pool *pgxpool.Pool
}

var _ attachment.Repository = (*AttachmentRepository)(nil)

func NewAttachmentRepository(pool *pgxpool.Pool) *AttachmentRepository {
	return &AttachmentRepository{pool: pool}
}

func (r *AttachmentRepository) CreateAttachment(ctx context.Context, params attachment.CreateAttachmentParams) (*domain.Attachment, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO attachments (
			id,
			conversation_id,
			uploaded_by_user_id,
			idempotency_key,
			filename,
			content_type,
			category,
			size_bytes,
			sha256,
			content_md5,
			status,
			object_key,
			metadata,
			upload_completed_at
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, NULLIF($4, ''), $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, CASE WHEN $11 = 'ready' THEN now() ELSE NULL END)
		ON CONFLICT (conversation_id, uploaded_by_user_id, idempotency_key) DO UPDATE
		SET idempotency_key = EXCLUDED.idempotency_key
		RETURNING
			id::text,
			conversation_id::text,
			uploaded_by_user_id::text,
			filename,
			content_type,
			category,
			size_bytes,
			sha256,
			content_md5,
			status,
			object_key,
			metadata,
			upload_completed_at,
			created_at,
			updated_at
	`, params.ID, params.ConversationID, params.UploadedByUserID, params.IdempotencyKey, params.Filename, params.ContentType, params.Category, params.SizeBytes, params.SHA256, params.ContentMD5, attachmentStatus(params.Status), params.ObjectKey, normalizedJSON(params.Metadata))

	stored, err := scanAttachment(row)
	if err != nil {
		if errors.Is(err, domain.ErrStorageQuotaExceeded) {
			return nil, err
		}
		if quotaErr := classifyStorageQuotaError(err); quotaErr != nil {
			return nil, quotaErr
		}
		return nil, fmt.Errorf("insert attachment: %w", err)
	}

	return stored, nil
}

func (r *AttachmentRepository) GetAttachment(ctx context.Context, conversationID string, uploadedByUserID string, attachmentID string) (*domain.Attachment, error) {
	stored, err := scanAttachment(r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			conversation_id::text,
			uploaded_by_user_id::text,
			filename,
			content_type,
			category,
			size_bytes,
			sha256,
			content_md5,
			status,
			object_key,
			metadata,
			upload_completed_at,
			created_at,
			updated_at
		FROM attachments
		WHERE conversation_id = $1::uuid
		  AND uploaded_by_user_id = $2::uuid
		  AND id = $3::uuid
	`, conversationID, uploadedByUserID, attachmentID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get attachment: %w", err)
	}
	return stored, nil
}

func (r *AttachmentRepository) GetAttachmentByIdempotencyKey(ctx context.Context, conversationID string, uploadedByUserID string, idempotencyKey string) (*domain.Attachment, error) {
	stored, err := scanAttachment(r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			conversation_id::text,
			uploaded_by_user_id::text,
			filename,
			content_type,
			category,
			size_bytes,
			sha256,
			content_md5,
			status,
			object_key,
			metadata,
			upload_completed_at,
			created_at,
			updated_at
		FROM attachments
		WHERE conversation_id = $1::uuid
		  AND uploaded_by_user_id = $2::uuid
		  AND idempotency_key = $3
	`, conversationID, uploadedByUserID, strings.TrimSpace(idempotencyKey)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get attachment by idempotency key: %w", err)
	}
	return stored, nil
}

func (r *AttachmentRepository) RefreshPendingAttachment(ctx context.Context, attachmentID string) (*domain.Attachment, error) {
	stored, err := scanAttachment(r.pool.QueryRow(ctx, `
		UPDATE attachments
		SET updated_at = now()
		WHERE id = $1::uuid AND status = 'pending'
		RETURNING id::text, conversation_id::text, uploaded_by_user_id::text, filename, content_type,
			category, size_bytes, sha256, content_md5, status, object_key, metadata, upload_completed_at, created_at, updated_at
	`, attachmentID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("refresh pending attachment: %w", err)
	}
	return stored, nil
}

func (r *AttachmentRepository) CompleteAttachment(ctx context.Context, conversationID string, uploadedByUserID string, attachmentID string, sha256 string) (*domain.Attachment, error) {
	stored, err := scanAttachment(r.pool.QueryRow(ctx, `
		UPDATE attachments
		SET status = 'ready',
			sha256 = $4,
			upload_completed_at = now(),
			updated_at = now()
		WHERE conversation_id = $1::uuid
		  AND uploaded_by_user_id = $2::uuid
		  AND id = $3::uuid
		  AND status = 'pending'
		RETURNING
			id::text,
			conversation_id::text,
			uploaded_by_user_id::text,
			filename,
			content_type,
			category,
			size_bytes,
			sha256,
			content_md5,
			status,
			object_key,
			metadata,
			upload_completed_at,
			created_at,
			updated_at
	`, conversationID, uploadedByUserID, attachmentID, strings.TrimSpace(sha256)))
	if err == nil {
		return stored, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("complete attachment: %w", err)
	}
	existing, getErr := r.GetAttachment(ctx, conversationID, uploadedByUserID, attachmentID)
	if getErr != nil {
		return nil, getErr
	}
	if existing.Status == domain.AttachmentStatusReady && existing.SHA256 == strings.TrimSpace(sha256) {
		return existing, nil
	}
	return nil, domain.ErrConflict
}

func (r *AttachmentRepository) UpsertAttachment(ctx context.Context, params attachment.CreateAttachmentParams) (*domain.Attachment, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO attachments (
			id,
			conversation_id,
			uploaded_by_user_id,
			filename,
			content_type,
			category,
			size_bytes,
			sha256,
			content_md5,
			status,
			object_key,
			metadata,
			upload_completed_at
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, now())
		ON CONFLICT (object_key) DO UPDATE SET
			filename = EXCLUDED.filename,
			content_type = EXCLUDED.content_type,
			category = EXCLUDED.category,
			size_bytes = EXCLUDED.size_bytes,
			sha256 = EXCLUDED.sha256,
			content_md5 = EXCLUDED.content_md5,
			status = 'ready',
			metadata = EXCLUDED.metadata,
			upload_completed_at = COALESCE(attachments.upload_completed_at, now()),
			updated_at = now()
		RETURNING
			id::text,
			conversation_id::text,
			uploaded_by_user_id::text,
			filename,
			content_type,
			category,
			size_bytes,
			sha256,
			content_md5,
			status,
			object_key,
			metadata,
			upload_completed_at,
			created_at,
			updated_at
	`, params.ID, params.ConversationID, params.UploadedByUserID, params.Filename, params.ContentType, params.Category, params.SizeBytes, params.SHA256, params.ContentMD5, attachmentStatus(params.Status), params.ObjectKey, normalizedJSON(params.Metadata))

	stored, err := scanAttachment(row)
	if err != nil {
		if quotaErr := classifyStorageQuotaError(err); quotaErr != nil {
			return nil, quotaErr
		}
		return nil, fmt.Errorf("upsert attachment: %w", err)
	}

	return stored, nil
}

func (r *AttachmentRepository) ListAttachmentsByIDs(ctx context.Context, conversationID string, ids []string) ([]domain.Attachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			conversation_id::text,
			uploaded_by_user_id::text,
			filename,
			content_type,
			category,
			size_bytes,
			sha256,
			content_md5,
			status,
			object_key,
			metadata,
			upload_completed_at,
			created_at,
			updated_at
		FROM attachments
		WHERE conversation_id = $1::uuid
		  AND id::text = ANY($2::text[])
		  AND status = 'ready'
	`, conversationID, ids)
	if err != nil {
		return nil, fmt.Errorf("list attachments by ids: %w", err)
	}
	defer rows.Close()

	byID := make(map[string]domain.Attachment, len(ids))
	for rows.Next() {
		item, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		byID[item.ID] = *item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachments by ids: %w", err)
	}

	attachments := make([]domain.Attachment, 0, len(ids))
	for _, id := range ids {
		item, ok := byID[strings.TrimSpace(id)]
		if !ok {
			return nil, domain.ErrNotFound
		}
		attachments = append(attachments, item)
	}

	return attachments, nil
}

func (r *AttachmentRepository) GetStorageUsage(ctx context.Context, userID string) (*domain.StorageUsage, error) {
	var usage domain.StorageUsage
	if err := r.pool.QueryRow(ctx, `
		SELECT storage_quota_bytes, storage_used_bytes
		FROM users
		WHERE id = $1::uuid AND deleted_at IS NULL
	`, userID).Scan(&usage.QuotaBytes, &usage.UsedBytes); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get storage usage: %w", err)
	}
	usage.AvailableBytes = usage.QuotaBytes - usage.UsedBytes
	if usage.AvailableBytes < 0 {
		usage.AvailableBytes = 0
	}
	return &usage, nil
}

func (r *AttachmentRepository) ListStorageAttachments(ctx context.Context, userID string, limit int, cursor string) ([]domain.StorageAttachment, string, error) {
	limit = clampLimit(limit, 50, 200)
	decoded, err := pagination.Decode(strings.TrimSpace(cursor))
	if err != nil {
		return nil, "", domain.NewValidationError("invalid cursor")
	}
	args := []any{userID}
	conditions := []string{
		"a.uploaded_by_user_id = $1::uuid",
		"a.status <> 'deleting'",
		"c.deleted_at IS NULL",
	}
	if decoded != nil {
		args = append(args, decoded.CreatedAt, decoded.ID)
		conditions = append(conditions, fmt.Sprintf("(a.created_at, a.id) < ($%d, $%d::uuid)", len(args)-1, len(args)))
	}
	args = append(args, limit+1)
	rows, err := r.pool.Query(ctx, `
		SELECT a.id::text, a.conversation_id::text, a.uploaded_by_user_id::text, a.filename,
			a.content_type, a.category, a.size_bytes, a.sha256, a.content_md5, a.status,
			a.object_key, a.metadata, a.upload_completed_at, a.created_at, a.updated_at, c.title
		FROM attachments a
		JOIN conversations c ON c.id = a.conversation_id
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, "", fmt.Errorf("list storage attachments: %w", err)
	}
	defer rows.Close()
	items := make([]domain.StorageAttachment, 0, limit+1)
	for rows.Next() {
		item, err := scanStorageAttachment(rows)
		if err != nil {
			return nil, "", err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate storage attachments: %w", err)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = pagination.Encode(last.CreatedAt, last.ID)
	}
	return items, next, nil
}

func (r *AttachmentRepository) ClaimAttachmentDeletion(ctx context.Context, userID string, attachmentID string) (*domain.Attachment, error) {
	item, err := scanAttachment(r.pool.QueryRow(ctx, `
		UPDATE attachments
		SET status = 'deleting', updated_at = now()
		WHERE id = $1::uuid
		  AND uploaded_by_user_id = $2::uuid
		  AND status IN ('pending', 'ready')
		RETURNING id::text, conversation_id::text, uploaded_by_user_id::text, filename, content_type,
			category, size_bytes, sha256, content_md5, status, object_key, metadata, upload_completed_at,
			created_at, updated_at
	`, attachmentID, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("claim attachment deletion: %w", err)
	}
	return item, nil
}

func (r *AttachmentRepository) ListExpiredAttachmentUploads(ctx context.Context, createdBefore time.Time, limit int) ([]domain.Attachment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, conversation_id::text, uploaded_by_user_id::text, filename, content_type,
			category, size_bytes, sha256, content_md5, status, object_key, metadata, upload_completed_at, created_at, updated_at
		FROM attachments
		WHERE (status = 'pending' AND updated_at < $1) OR status = 'deleting'
		ORDER BY updated_at ASC
		LIMIT $2
	`, createdBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("list expired attachment uploads: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Attachment, 0, limit)
	for rows.Next() {
		item, scanErr := scanAttachment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired attachment uploads: %w", err)
	}
	return items, nil
}

func (r *AttachmentRepository) ClaimExpiredAttachmentUpload(ctx context.Context, attachmentID string, createdBefore time.Time) (*domain.Attachment, error) {
	item, err := scanAttachment(r.pool.QueryRow(ctx, `
		UPDATE attachments
		SET status = 'deleting', updated_at = now()
		WHERE id = $1::uuid
		  AND ((status = 'pending' AND updated_at < $2) OR status = 'deleting')
		RETURNING id::text, conversation_id::text, uploaded_by_user_id::text, filename, content_type,
			category, size_bytes, sha256, content_md5, status, object_key, metadata, upload_completed_at, created_at, updated_at
	`, attachmentID, createdBefore))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("claim expired attachment upload: %w", err)
	}
	return item, nil
}

func (r *AttachmentRepository) DeleteClaimedAttachmentUpload(ctx context.Context, attachmentID string) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM attachments WHERE id = $1::uuid AND status = 'deleting'`, attachmentID)
	if err != nil {
		return fmt.Errorf("delete claimed attachment upload: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func attachmentStatus(status string) string {
	if strings.TrimSpace(status) == domain.AttachmentStatusPending {
		return domain.AttachmentStatusPending
	}
	return domain.AttachmentStatusReady
}

func classifyStorageQuotaError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" && strings.Contains(pgErr.Message, "storage quota exceeded") {
		return domain.ErrStorageQuotaExceeded
	}
	return nil
}
