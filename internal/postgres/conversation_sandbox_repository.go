package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ConversationSandboxRepository struct {
	pool *pgxpool.Pool
}

func NewConversationSandboxRepository(pool *pgxpool.Pool) *ConversationSandboxRepository {
	return &ConversationSandboxRepository{pool: pool}
}

func (r *ConversationSandboxRepository) GetActiveConversationSandbox(ctx context.Context, conversationID string) (*domain.ConversationSandbox, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			conversation_id::text,
			provider,
			runtime_id,
			status,
			runtime_metadata,
			created_at,
			updated_at,
			destroyed_at
		FROM sandboxes
		WHERE conversation_id = $1::uuid
		  AND status = $2
		  AND destroyed_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, conversationID, domain.SandboxStatusActive)

	sandbox, err := scanConversationSandbox(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get active conversation sandbox: %w", err)
	}

	return sandbox, nil
}

func (r *ConversationSandboxRepository) GetLatestConversationSandbox(ctx context.Context, conversationID string) (*domain.ConversationSandbox, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, conversation_id::text, provider, runtime_id, status,
			runtime_metadata, created_at, updated_at, destroyed_at
		FROM sandboxes
		WHERE conversation_id = $1::uuid
		ORDER BY created_at DESC
		LIMIT 1
	`, conversationID)
	sandbox, err := scanConversationSandbox(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get latest conversation sandbox: %w", err)
	}
	return sandbox, nil
}

func (r *ConversationSandboxRepository) CreateConversationSandbox(ctx context.Context, conversationID string, provider string, runtimeID string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO sandboxes (
			conversation_id,
			provider,
			runtime_id,
			status,
			runtime_metadata
		)
		VALUES ($1::uuid, $2, $3, $4, $5::jsonb)
		RETURNING
			id::text,
			conversation_id::text,
			provider,
			runtime_id,
			status,
			runtime_metadata,
			created_at,
			updated_at,
			destroyed_at
	`, conversationID, provider, runtimeID, domain.SandboxStatusActive, normalizedJSON(metadata))

	sandbox, err := scanConversationSandbox(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("create conversation sandbox: %w", err)
	}

	return sandbox, nil
}

func (r *ConversationSandboxRepository) DestroyConversationSandbox(ctx context.Context, sandboxID string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE sandboxes
		SET
			status = $2,
			runtime_metadata = $3::jsonb,
			destroyed_at = now()
		WHERE id = $1::uuid
		  AND status = $4
		  AND destroyed_at IS NULL
		RETURNING
			id::text,
			conversation_id::text,
			provider,
			runtime_id,
			status,
			runtime_metadata,
			created_at,
			updated_at,
			destroyed_at
	`, sandboxID, domain.SandboxStatusDestroyed, normalizedJSON(metadata), domain.SandboxStatusActive)

	sandbox, err := scanConversationSandbox(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("destroy conversation sandbox: %w", err)
	}

	return sandbox, nil
}

func (r *ConversationSandboxRepository) RestoreConversationSandbox(ctx context.Context, sandboxID string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE sandboxes
		SET status = $2, runtime_metadata = $3::jsonb, destroyed_at = NULL
		WHERE id = $1::uuid AND status = $4 AND destroyed_at IS NOT NULL
		RETURNING id::text, conversation_id::text, provider, runtime_id, status,
			runtime_metadata, created_at, updated_at, destroyed_at
	`, sandboxID, domain.SandboxStatusActive, normalizedJSON(metadata), domain.SandboxStatusDestroyed)
	sandbox, err := scanConversationSandbox(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isUniqueViolation(err) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("restore conversation sandbox: %w", err)
	}
	return sandbox, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation
}
