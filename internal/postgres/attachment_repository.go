package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
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
			object_key,
			metadata
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, NULLIF($4, ''), $5, $6, $7, $8, $9, $10, $11::jsonb)
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
			object_key,
			metadata,
			created_at,
			updated_at
	`, params.ID, params.ConversationID, params.UploadedByUserID, params.IdempotencyKey, params.Filename, params.ContentType, params.Category, params.SizeBytes, params.SHA256, params.ObjectKey, normalizedJSON(params.Metadata))

	stored, err := scanAttachment(row)
	if err != nil {
		return nil, fmt.Errorf("insert attachment: %w", err)
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
			object_key,
			metadata,
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
			object_key,
			metadata
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, $10::jsonb)
		ON CONFLICT (object_key) DO UPDATE SET
			filename = EXCLUDED.filename,
			content_type = EXCLUDED.content_type,
			category = EXCLUDED.category,
			size_bytes = EXCLUDED.size_bytes,
			sha256 = EXCLUDED.sha256,
			metadata = EXCLUDED.metadata,
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
			object_key,
			metadata,
			created_at,
			updated_at
	`, params.ID, params.ConversationID, params.UploadedByUserID, params.Filename, params.ContentType, params.Category, params.SizeBytes, params.SHA256, params.ObjectKey, normalizedJSON(params.Metadata))

	stored, err := scanAttachment(row)
	if err != nil {
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
			object_key,
			metadata,
			created_at,
			updated_at
		FROM attachments
		WHERE conversation_id = $1::uuid
		  AND id::text = ANY($2::text[])
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
