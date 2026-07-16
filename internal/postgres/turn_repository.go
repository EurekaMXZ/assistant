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
	tokenCount := estimateUserMessageTokens(trimmedContent, params.Metadata)

	head, err := queryContextHeadForUpdate(ctx, tx, params.ConversationID)
	if err != nil {
		return nil, err
	}
	if err := ensureNoActiveTurn(ctx, tx, params.ConversationID); err != nil {
		return nil, err
	}

	var maxTurnSeq int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(seq), 0)
		FROM turns
		WHERE conversation_id = $1::uuid
	`, params.ConversationID).Scan(&maxTurnSeq); err != nil {
		return nil, fmt.Errorf("get next turn sequence: %w", err)
	}
	nextSeq := max(head.LastSeq, maxTurnSeq) + 1

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
			COALESCE(retry_of_turn_id::text, ''),
			variant_index,
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
			context_excluded,
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

func ensureNoActiveTurn(ctx context.Context, tx pgx.Tx, conversationID string) error {
	var active bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM turns
			WHERE conversation_id = $1::uuid
				AND status IN ($2, $3, $4)
		)
	`, conversationID, domain.TurnStatusAccepted, domain.TurnStatusContextReady, domain.TurnStatusProcessing).Scan(&active); err != nil {
		return fmt.Errorf("check active turn: %w", err)
	}
	if active {
		return domain.ErrConflict
	}
	return nil
}

func (r *TurnRepository) CreateRetryTurn(ctx context.Context, sourceTurnID string, params CreateUserTurnParams) (*domain.EnqueuedRetryTurn, error) {
	trimmedContent := strings.TrimSpace(params.Content)
	if trimmedContent == "" && !messageMetadataHasAttachmentIDs(params.Metadata) {
		return nil, domain.NewValidationError("message content is required")
	}
	tokenCount := estimateUserMessageTokens(trimmedContent, params.Metadata)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin retry turn tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var rootTurnID string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(retry_of_turn_id, id)::text
		FROM turns
		WHERE id = $1::uuid
	`, sourceTurnID).Scan(&rootTurnID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("resolve retry root turn: %w", err)
	}

	root, err := scanTurn(tx.QueryRow(ctx, `
		SELECT
			id::text,
			conversation_id::text,
			seq,
			COALESCE(retry_of_turn_id::text, ''),
			variant_index,
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
		FOR UPDATE
	`, rootTurnID))
	if err != nil {
		return nil, fmt.Errorf("lock retry root turn: %w", err)
	}
	if root.Status != domain.TurnStatusCompleted && root.Status != domain.TurnStatusFailed {
		return nil, domain.ErrConflict
	}

	head, err := queryContextHeadForUpdate(ctx, tx, root.ConversationID)
	if err != nil {
		return nil, err
	}
	if root.Seq <= head.CoveredUntilSeq {
		return nil, domain.ErrConflict
	}
	if err := ensureNoActiveTurn(ctx, tx, root.ConversationID); err != nil {
		return nil, err
	}

	var laterPrimary bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM turns
			WHERE conversation_id = $1::uuid
				AND retry_of_turn_id IS NULL
				AND seq > $2
		)
	`, root.ConversationID, root.Seq).Scan(&laterPrimary); err != nil {
		return nil, fmt.Errorf("check retry turn position: %w", err)
	}
	if laterPrimary {
		return nil, domain.ErrConflict
	}

	var maxTurnSeq int64
	var variantIndex int
	if err := tx.QueryRow(ctx, `
		SELECT
			COALESCE(MAX(seq), 0),
			COALESCE(MAX(variant_index) FILTER (WHERE id = $2::uuid OR retry_of_turn_id = $2::uuid), 1)
		FROM turns
		WHERE conversation_id = $1::uuid
	`, root.ConversationID, root.ID).Scan(&maxTurnSeq, &variantIndex); err != nil {
		return nil, fmt.Errorf("get retry turn sequence: %w", err)
	}
	nextSeq := max(head.LastSeq, maxTurnSeq) + 1
	variantIndex++
	snapshot, err := json.Marshal(params.ModelSnapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal retry turn model snapshot: %w", err)
	}
	turnMetadata := decodeMetadata(params.Metadata)
	turnMetadata["variant_source_turn_id"] = sourceTurnID
	encodedTurnMetadata, err := json.Marshal(turnMetadata)
	if err != nil {
		return nil, fmt.Errorf("marshal retry turn metadata: %w", err)
	}

	turn, err := scanTurn(tx.QueryRow(ctx, `
		INSERT INTO turns (
			conversation_id, seq, retry_of_turn_id, variant_index, status, metadata,
			model_id, model_revision, model_price_id, model_snapshot
		)
		VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6::jsonb, NULLIF($7, '')::uuid, NULLIF($8, 0), NULLIF($9, '')::uuid, $10::jsonb)
		RETURNING
			id::text,
			conversation_id::text,
			seq,
			COALESCE(retry_of_turn_id::text, ''),
			variant_index,
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
	`, root.ConversationID, nextSeq, root.ID, variantIndex, domain.TurnStatusAccepted,
		normalizedJSON(encodedTurnMetadata), params.ModelSnapshot.ModelID, params.ModelSnapshot.ModelRevision,
		params.ModelSnapshot.ModelPriceID, snapshot))
	if err != nil {
		return nil, fmt.Errorf("insert retry turn: %w", err)
	}
	message, err := scanMessage(tx.QueryRow(ctx, `
		INSERT INTO messages (
			conversation_id, turn_id, seq, role, content_text, token_count, metadata, context_excluded
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::jsonb, true)
		RETURNING
			id::text,
			conversation_id::text,
			COALESCE(turn_id::text, ''),
			seq,
			role,
			COALESCE(content_text, ''),
			token_count,
			metadata,
			context_excluded,
			created_at
	`, root.ConversationID, turn.ID, nextSeq, domain.RoleUser, nullableText(trimmedContent), tokenCount, normalizedJSON(params.Metadata)))
	if err != nil {
		return nil, fmt.Errorf("insert retry user message: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE context_heads SET last_seq = $2 WHERE conversation_id = $1::uuid
	`, root.ConversationID, nextSeq); err != nil {
		return nil, fmt.Errorf("advance context head for retry: %w", err)
	}

	if err := insertOutboxEvent(ctx, tx, outboxInsert{
		EventType: workflow.EventTurnAccepted, ConversationID: root.ConversationID, TurnID: turn.ID,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit retry turn: %w", err)
	}
	return &domain.EnqueuedRetryTurn{ConversationID: root.ConversationID, Message: *message, Turn: *turn}, nil
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

func estimateUserMessageTokens(content string, metadata json.RawMessage) int {
	tokens := domain.EstimateTokens(content)
	var payload struct {
		Attachments []struct {
			Category string `json:"category"`
		} `json:"attachments"`
	}
	if json.Unmarshal(metadata, &payload) != nil {
		return tokens
	}
	for _, attachment := range payload.Attachments {
		if attachment.Category == domain.AttachmentCategoryImage {
			tokens += domain.EstimatedImageInputTokens
			continue
		}
		tokens += 64
	}
	return tokens
}

func (r *TurnRepository) GetTurn(ctx context.Context, turnID string) (*domain.Turn, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			conversation_id::text,
			seq,
			COALESCE(retry_of_turn_id::text, ''),
			variant_index,
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
