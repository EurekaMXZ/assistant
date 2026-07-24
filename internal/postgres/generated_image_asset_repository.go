package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type GeneratedImageAssetRepository struct {
	pool *pgxpool.Pool
}

func NewGeneratedImageAssetRepository(pool *pgxpool.Pool) *GeneratedImageAssetRepository {
	return &GeneratedImageAssetRepository{pool: pool}
}

func (r *GeneratedImageAssetRepository) UpsertGeneratedImageAsset(ctx context.Context, params domain.UpsertGeneratedImageAssetParams) (*domain.GeneratedImageAsset, error) {
	asset, err := scanGeneratedImageAsset(r.pool.QueryRow(ctx, `
		INSERT INTO generated_image_assets (
			id, conversation_id, turn_id, turn_run_id, response_id, item_id, kind,
			revision, object_key, content_type, size_bytes, sha256, width, height,
			attachment_id, expires_at
		)
		VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14, NULLIF($15, '')::uuid, $16
		)
		ON CONFLICT (turn_run_id, item_id, kind, revision) DO UPDATE SET
			response_id = EXCLUDED.response_id,
			object_key = EXCLUDED.object_key,
			content_type = EXCLUDED.content_type,
			size_bytes = EXCLUDED.size_bytes,
			sha256 = EXCLUDED.sha256,
			width = EXCLUDED.width,
			height = EXCLUDED.height,
			attachment_id = EXCLUDED.attachment_id,
			expires_at = EXCLUDED.expires_at,
			status = 'ready',
			updated_at = now()
		RETURNING id::text, conversation_id::text, turn_id::text, turn_run_id::text,
			response_id, item_id, kind, revision, status, object_key, content_type,
			size_bytes, sha256, width, height, attachment_id::text, expires_at,
			created_at, updated_at
	`, params.ID, params.ConversationID, params.TurnID, params.TurnRunID,
		strings.TrimSpace(params.ResponseID), strings.TrimSpace(params.ItemID), params.Kind,
		params.Revision, params.ObjectKey, params.ContentType, params.SizeBytes,
		strings.ToLower(strings.TrimSpace(params.SHA256)), params.Width, params.Height,
		params.AttachmentID, params.ExpiresAt))
	if err != nil {
		return nil, fmt.Errorf("upsert generated image asset: %w", err)
	}
	return asset, nil
}

func (r *GeneratedImageAssetRepository) GetGeneratedImageAsset(ctx context.Context, ownerUserID string, conversationID string, assetID string) (*domain.GeneratedImageAsset, error) {
	asset, err := scanGeneratedImageAsset(r.pool.QueryRow(ctx, `
		SELECT a.id::text, a.conversation_id::text, a.turn_id::text, a.turn_run_id::text,
			a.response_id, a.item_id, a.kind, a.revision, a.status, a.object_key,
			a.content_type, a.size_bytes, a.sha256, a.width, a.height,
			a.attachment_id::text, a.expires_at, a.created_at, a.updated_at
		FROM generated_image_assets a
		JOIN conversations c ON c.id = a.conversation_id
		WHERE a.id = $1::uuid
		  AND a.conversation_id = $2::uuid
		  AND c.owner_user_id = $3::uuid
		  AND c.status <> 'deleted'
		  AND a.status = 'ready'
		  AND (a.expires_at IS NULL OR a.expires_at > now())
	`, assetID, conversationID, ownerUserID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get generated image asset: %w", err)
	}
	return asset, nil
}

func (r *GeneratedImageAssetRepository) ListGeneratedImageAssetsByTurn(ctx context.Context, turnID string) ([]domain.GeneratedImageAsset, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, conversation_id::text, turn_id::text, turn_run_id::text,
			response_id, item_id, kind, revision, status, object_key, content_type,
			size_bytes, sha256, width, height, attachment_id::text, expires_at,
			created_at, updated_at
		FROM generated_image_assets
		WHERE turn_id = $1::uuid AND status = 'ready'
		  AND (expires_at IS NULL OR expires_at > now())
		ORDER BY created_at ASC, revision ASC
	`, turnID)
	if err != nil {
		return nil, fmt.Errorf("list generated image assets by turn: %w", err)
	}
	defer rows.Close()

	assets := make([]domain.GeneratedImageAsset, 0)
	for rows.Next() {
		asset, scanErr := scanGeneratedImageAsset(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		assets = append(assets, *asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate generated image assets by turn: %w", err)
	}
	return assets, nil
}

func (r *GeneratedImageAssetRepository) ListExpiredGeneratedImageAssets(ctx context.Context, expiredBefore time.Time, limit int) ([]domain.GeneratedImageAsset, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, conversation_id::text, turn_id::text, turn_run_id::text,
			response_id, item_id, kind, revision, status, object_key, content_type,
			size_bytes, sha256, width, height, attachment_id::text, expires_at,
			created_at, updated_at
		FROM generated_image_assets
		WHERE kind = 'partial' AND expires_at <= $1 AND status IN ('ready', 'deleting')
		ORDER BY expires_at ASC
		LIMIT $2
	`, expiredBefore, clampLimit(limit, 100, 1000))
	if err != nil {
		return nil, fmt.Errorf("list expired generated image assets: %w", err)
	}
	defer rows.Close()

	assets := make([]domain.GeneratedImageAsset, 0)
	for rows.Next() {
		asset, scanErr := scanGeneratedImageAsset(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		assets = append(assets, *asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired generated image assets: %w", err)
	}
	return assets, nil
}

func (r *GeneratedImageAssetRepository) ClaimExpiredGeneratedImageAsset(ctx context.Context, assetID string, expiredBefore time.Time) (*domain.GeneratedImageAsset, error) {
	asset, err := scanGeneratedImageAsset(r.pool.QueryRow(ctx, `
		UPDATE generated_image_assets
		SET status = 'deleting', updated_at = now()
		WHERE id = $1::uuid AND kind = 'partial' AND expires_at <= $2
		  AND status IN ('ready', 'deleting')
		RETURNING id::text, conversation_id::text, turn_id::text, turn_run_id::text,
			response_id, item_id, kind, revision, status, object_key, content_type,
			size_bytes, sha256, width, height, attachment_id::text, expires_at,
			created_at, updated_at
	`, assetID, expiredBefore))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("claim expired generated image asset: %w", err)
	}
	return asset, nil
}

func (r *GeneratedImageAssetRepository) DeleteClaimedGeneratedImageAsset(ctx context.Context, assetID string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM generated_image_assets WHERE id = $1::uuid AND status = 'deleting'`, assetID)
	if err != nil {
		return fmt.Errorf("delete claimed generated image asset: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

type generatedImageAssetScanner interface {
	Scan(dest ...any) error
}

func scanGeneratedImageAsset(row generatedImageAssetScanner) (*domain.GeneratedImageAsset, error) {
	var asset domain.GeneratedImageAsset
	var attachmentID *string
	if err := row.Scan(
		&asset.ID, &asset.ConversationID, &asset.TurnID, &asset.TurnRunID,
		&asset.ResponseID, &asset.ItemID, &asset.Kind, &asset.Revision, &asset.Status,
		&asset.ObjectKey, &asset.ContentType, &asset.SizeBytes, &asset.SHA256,
		&asset.Width, &asset.Height, &attachmentID, &asset.ExpiresAt,
		&asset.CreatedAt, &asset.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if attachmentID != nil {
		asset.AttachmentID = *attachmentID
	}
	return &asset, nil
}
