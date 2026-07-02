package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TurnRepository struct {
	pool *pgxpool.Pool
}

type CreateUserTurnParams struct {
	ConversationID string
	Content        string
	Metadata       json.RawMessage
	ModelSnapshot  domain.ModelExecutionSnapshot
}

func NewTurnRepository(pool *pgxpool.Pool) *TurnRepository {
	return &TurnRepository{pool: pool}
}

func (r *TurnRepository) CreateUserTurn(ctx context.Context, params CreateUserTurnParams) (*domain.EnqueuedTurn, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := createUserTurn(ctx, tx, params)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit user turn: %w", err)
	}
	return result, nil
}

func createUserTurn(ctx context.Context, tx pgx.Tx, params CreateUserTurnParams) (*domain.EnqueuedTurn, error) {
	trimmedContent := strings.TrimSpace(params.Content)
	if trimmedContent == "" && !messageMetadataHasAttachmentIDs(params.Metadata) {
		return nil, fmt.Errorf("message content is required")
	}
	tokenCount := domain.EstimateTokens(trimmedContent)

	head, err := queryContextHeadForUpdate(ctx, tx, params.ConversationID)
	if err != nil {
		return nil, err
	}

	nextSeq := head.LastSeq + 1

	snapshot, err := json.Marshal(params.ModelSnapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal turn model snapshot: %w", err)
	}
	turnRow := tx.QueryRow(ctx, `
		INSERT INTO turns (conversation_id, seq, status, metadata, model_id, model_revision, model_price_id, model_snapshot)
		VALUES ($1::uuid, $2, $3, $4::jsonb, NULLIF($5, '')::uuid, NULLIF($6, 0), NULLIF($7, '')::uuid, $8::jsonb)
		RETURNING
			id::text,
			conversation_id::text,
			seq,
			status,
			COALESCE(request_blob_key, ''),
			COALESCE(response_blob_key, ''),
			COALESCE(stream_blob_key, ''),
			COALESCE(openai_response_id, ''),
			COALESCE(error_code, ''),
			COALESCE(error_message, ''),
			metadata,
			started_at,
			completed_at,
			failed_at,
			created_at,
			updated_at
	`, params.ConversationID, nextSeq, domain.TurnStatusAccepted, normalizedJSON(params.Metadata),
		params.ModelSnapshot.ModelID, params.ModelSnapshot.ModelRevision, params.ModelSnapshot.ModelPriceID, snapshot)

	turn, err := scanTurn(turnRow)
	if err != nil {
		return nil, fmt.Errorf("insert turn: %w", err)
	}

	messageRow := tx.QueryRow(ctx, `
		INSERT INTO messages (
			conversation_id,
			turn_id,
			seq,
			role,
			content_text,
			token_count,
			metadata
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::jsonb)
		RETURNING
			id::text,
			conversation_id::text,
			COALESCE(turn_id::text, ''),
			seq,
			role,
			COALESCE(content_text, ''),
			token_count,
			metadata,
			created_at
	`, params.ConversationID, turn.ID, nextSeq, domain.RoleUser, nullableText(trimmedContent), tokenCount, normalizedJSON(params.Metadata))

	message, err := scanMessage(messageRow)
	if err != nil {
		return nil, fmt.Errorf("insert user message: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE context_heads
		SET
			last_seq = $2,
			active_context_tokens = active_context_tokens + $3
		WHERE conversation_id = $1::uuid
	`, params.ConversationID, nextSeq, tokenCount); err != nil {
		return nil, fmt.Errorf("update context head: %w", err)
	}

	if err := insertOutboxEvent(ctx, tx, outboxInsert{
		EventType:      workflow.EventTurnAccepted,
		ConversationID: params.ConversationID,
		TurnID:         turn.ID,
	}); err != nil {
		return nil, err
	}

	return &domain.EnqueuedTurn{
		ConversationID: params.ConversationID,
		Message:        *message,
		Turn:           *turn,
	}, nil
}

func messageMetadataHasAttachmentIDs(metadata json.RawMessage) bool {
	decoded := decodeMetadata(metadata)
	raw, ok := decoded["attachment_ids"]
	if !ok {
		return false
	}
	items, ok := raw.([]any)
	return ok && len(items) > 0
}

func (r *TurnRepository) GetTurn(ctx context.Context, turnID string) (*domain.Turn, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			conversation_id::text,
			seq,
			status,
			COALESCE(request_blob_key, ''),
			COALESCE(response_blob_key, ''),
			COALESCE(stream_blob_key, ''),
			COALESCE(openai_response_id, ''),
			COALESCE(error_code, ''),
			COALESCE(error_message, ''),
			metadata,
			started_at,
			completed_at,
			failed_at,
			created_at,
			updated_at
		FROM turns
		WHERE id = $1::uuid
	`, turnID)

	turn, err := scanTurn(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get turn: %w", err)
	}

	return turn, nil
}
