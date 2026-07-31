package postgres

import (
	"context"
	"database/sql"
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

const conversationTurnSummarySelect = `
	SELECT
		t.id::text,
		t.conversation_id::text,
		t.seq AS seq,
		COALESCE(t.retry_of_turn_id::text, ''),
		t.variant_index,
		t.status,
		t.created_at,
		t.updated_at,
		user_message.id::text,
		user_message.conversation_id::text,
		user_message.turn_id::text,
		user_message.seq AS user_message_seq,
		user_message.role,
		LEFT(COALESCE(user_message.content_text, ''), 512),
		user_message.token_count,
		COALESCE(user_message.metadata, '{}'::jsonb),
		user_message.context_excluded,
		user_message.created_at,
		COALESCE(event_bounds.first_event_seq, 0),
		COALESCE(event_bounds.last_event_seq, 0)
	FROM turns AS t
	LEFT JOIN LATERAL (
		SELECT id, conversation_id, turn_id, seq, role, content_text,
			token_count, metadata, context_excluded, created_at
		FROM messages
		WHERE conversation_id = t.conversation_id
			AND turn_id = t.id
			AND role = 'user'
		ORDER BY seq ASC
		LIMIT 1
	) AS user_message ON true
	LEFT JOIN LATERAL (
		SELECT MIN(event_seq) AS first_event_seq, MAX(event_seq) AS last_event_seq
		FROM conversation_events
		WHERE conversation_id = t.conversation_id
			AND turn_id = t.id
	) AS event_bounds ON true`

func (r *TurnRepository) ListConversationTurnSummaries(ctx context.Context, conversationID string, limit int, beforeSeq int64, afterSeq int64, queryText string) ([]domain.ConversationTurnSummary, error) {
	conditions := []string{"t.conversation_id = $1::uuid"}
	args := []any{conversationID}
	if beforeSeq > 0 {
		args = append(args, beforeSeq)
		conditions = append(conditions, fmt.Sprintf("t.seq < $%d", len(args)))
	}
	if afterSeq > 0 {
		args = append(args, afterSeq)
		conditions = append(conditions, fmt.Sprintf("t.seq > $%d", len(args)))
	}
	if trimmed := strings.TrimSpace(queryText); trimmed != "" {
		args = append(args, trimmed)
		conditions = append(conditions, fmt.Sprintf("COALESCE(user_message.content_text, '') ILIKE '%%' || $%d || '%%'", len(args)))
	}

	order := "DESC"
	if afterSeq > 0 {
		order = "ASC"
	}
	args = append(args, clampLimit(limit, 100, 1000))
	query := conversationTurnSummarySelect + "\nWHERE " + strings.Join(conditions, " AND ")
	if afterSeq == 0 {
		query = `SELECT * FROM (` + query +
			"\nORDER BY t.seq " + order +
			fmt.Sprintf("\nLIMIT $%d", len(args)) +
			`) recent_turns ORDER BY seq ASC`
	} else {
		query += "\nORDER BY t.seq " + order + fmt.Sprintf("\nLIMIT $%d", len(args))
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversation turn summaries: %w", err)
	}
	defer rows.Close()

	items := make([]domain.ConversationTurnSummary, 0)
	for rows.Next() {
		item, err := scanConversationTurnSummary(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation turn summaries: %w", err)
	}
	return items, nil
}

func (r *TurnRepository) ListConversationTurnWindow(ctx context.Context, conversationID string, centerSeq int64, beforeLimit int, afterLimit int) ([]domain.ConversationTurnSummary, error) {
	if centerSeq <= 0 {
		return nil, domain.NewValidationError("center turn sequence must be positive")
	}
	args := []any{conversationID, centerSeq, clampLimit(beforeLimit, 3, 1000) + 1, clampLimit(afterLimit, 3, 1000) + 1}
	query := `WITH base AS (` + conversationTurnSummarySelect + `
		WHERE t.conversation_id = $1::uuid
	), selected AS (
		(SELECT * FROM base WHERE seq < $2 ORDER BY seq DESC LIMIT $3)
		UNION ALL
		(SELECT * FROM base WHERE seq = $2)
		UNION ALL
		(SELECT * FROM base WHERE seq > $2 ORDER BY seq ASC LIMIT $4)
	)
	SELECT * FROM selected ORDER BY seq ASC`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversation turn window: %w", err)
	}
	defer rows.Close()

	items := make([]domain.ConversationTurnSummary, 0)
	for rows.Next() {
		item, err := scanConversationTurnSummary(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation turn window: %w", err)
	}
	return items, nil
}

func scanConversationTurnSummary(row scanRow) (*domain.ConversationTurnSummary, error) {
	var (
		item                   domain.ConversationTurnSummary
		retryOfTurnID          sql.NullString
		messageID              sql.NullString
		messageConversationID  sql.NullString
		messageTurnID          sql.NullString
		messageSeq             sql.NullInt64
		messageRole            sql.NullString
		messageContent         sql.NullString
		messageTokenCount      sql.NullInt64
		messageMetadata        []byte
		messageContextExcluded sql.NullBool
		messageCreatedAt       sql.NullTime
	)
	if err := row.Scan(
		&item.ID,
		&item.ConversationID,
		&item.Seq,
		&retryOfTurnID,
		&item.VariantIndex,
		&item.Status,
		&item.CreatedAt,
		&item.UpdatedAt,
		&messageID,
		&messageConversationID,
		&messageTurnID,
		&messageSeq,
		&messageRole,
		&messageContent,
		&messageTokenCount,
		&messageMetadata,
		&messageContextExcluded,
		&messageCreatedAt,
		&item.FirstEventSeq,
		&item.LastEventSeq,
	); err != nil {
		return nil, err
	}
	if retryOfTurnID.Valid {
		item.RetryOfTurnID = retryOfTurnID.String
	}
	if messageID.Valid {
		message := &domain.Message{
			ID:              messageID.String,
			ConversationID:  messageConversationID.String,
			TurnID:          messageTurnID.String,
			Role:            messageRole.String,
			ContentText:     messageContent.String,
			Metadata:        cloneJSON(messageMetadata),
			ContextExcluded: messageContextExcluded.Bool,
		}
		if messageSeq.Valid {
			message.Seq = messageSeq.Int64
		}
		if messageTokenCount.Valid {
			value := int(messageTokenCount.Int64)
			message.TokenCount = &value
		}
		if messageCreatedAt.Valid {
			message.CreatedAt = messageCreatedAt.Time
		}
		item.UserMessage = message
	}
	return &item, nil
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
	messagePayload, err := json.Marshal(map[string]any{"message": message, "turn": turn})
	if err != nil {
		return nil, fmt.Errorf("marshal user complete event: %w", err)
	}
	if err := insertCompleteEvent(ctx, tx, head, domain.ConversationEventInput{
		ConversationID:  params.ConversationID,
		TurnID:          turn.ID,
		EventKey:        "message:" + message.ID,
		SchemaVersion:   1,
		EventType:       "message.completed",
		Payload:         messagePayload,
		ContextIncluded: true,
	}); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE context_heads
		SET
			last_seq = $2,
			active_context_tokens = active_context_tokens + $3,
			version = version + 1
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
				AND status IN ($2, $3, $4, $5)
		)
	`, conversationID, domain.TurnStatusAccepted, domain.TurnStatusContextReady, domain.TurnStatusProcessing, domain.TurnStatusAwaitingInput).Scan(&active); err != nil {
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
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::jsonb, false)
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
	messagePayload, err := json.Marshal(map[string]any{"message": message, "turn": turn})
	if err != nil {
		return nil, fmt.Errorf("marshal retry user complete event: %w", err)
	}
	if err := insertCompleteEvent(ctx, tx, head, domain.ConversationEventInput{
		ConversationID:  root.ConversationID,
		TurnID:          turn.ID,
		EventKey:        "message:" + message.ID,
		SchemaVersion:   1,
		EventType:       "message.completed",
		Payload:         messagePayload,
		ContextIncluded: true,
	}); err != nil {
		return nil, err
	}
	replacedTokens, selectedUserTokens, err := activateTurnVariantMessages(ctx, tx, root.ID, turn.ID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE context_heads
		SET last_seq = $2,
			active_context_tokens = GREATEST(0, active_context_tokens - $3 + $4),
			version = version + 1
		WHERE conversation_id = $1::uuid
	`, root.ConversationID, nextSeq, replacedTokens, selectedUserTokens); err != nil {
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
