package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ConversationEventRepository struct {
	pool *pgxpool.Pool
}

func NewConversationEventRepository(pool *pgxpool.Pool) *ConversationEventRepository {
	return &ConversationEventRepository{pool: pool}
}

func (r *ConversationEventRepository) AppendCompleteEvent(ctx context.Context, params domain.ConversationEventInput) (*domain.ConversationEvent, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("conversation event repository is not configured")
	}
	if strings.TrimSpace(params.ConversationID) == "" || strings.TrimSpace(params.EventKey) == "" {
		return nil, domain.NewValidationError("conversation id and event key are required")
	}
	if params.SchemaVersion <= 0 {
		params.SchemaVersion = 1
	}
	if len(params.Payload) == 0 {
		params.Payload = json.RawMessage(`{}`)
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin append conversation event: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentContextSeq int64
	if err := tx.QueryRow(ctx, `
		SELECT last_context_event_seq
		FROM context_heads
		WHERE conversation_id = $1::uuid
		FOR UPDATE
	`, params.ConversationID).Scan(&currentContextSeq); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("lock conversation context head: %w", err)
	}

	var existing domain.ConversationEvent
	var existingPayload []byte
	var existingTurnID, existingRunID *string
	if err := tx.QueryRow(ctx, `
		SELECT id::text, conversation_id::text, turn_id::text, turn_run_id::text,
			event_seq, event_key, schema_version, event_type, payload, context_included, created_at
		FROM conversation_events
		WHERE conversation_id = $1::uuid AND event_key = $2
	`, params.ConversationID, params.EventKey).Scan(
		&existing.ID, &existing.ConversationID, &existingTurnID, &existingRunID,
		&existing.EventSeq, &existing.EventKey, &existing.SchemaVersion, &existing.EventType,
		&existingPayload, &existing.ContextIncluded, &existing.CreatedAt,
	); err == nil {
		existing.Payload = existingPayload
		if existingTurnID != nil {
			existing.TurnID = *existingTurnID
		}
		if existingRunID != nil {
			existing.TurnRunID = *existingRunID
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit existing conversation event: %w", err)
		}
		return &existing, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("check existing conversation event: %w", err)
	}

	var eventSeq int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(event_seq), 0) + 1
		FROM conversation_events
		WHERE conversation_id = $1::uuid
	`, params.ConversationID).Scan(&eventSeq); err != nil {
		return nil, fmt.Errorf("allocate conversation event sequence: %w", err)
	}
	if params.ContextIncluded {
		if _, err := tx.Exec(ctx, `
			UPDATE context_heads
			SET last_context_event_seq = $2
			WHERE conversation_id = $1::uuid
		`, params.ConversationID, eventSeq); err != nil {
			return nil, fmt.Errorf("advance context event sequence: %w", err)
		}
	}

	turnID := nullableUUIDString(params.TurnID)
	runID := nullableUUIDString(params.TurnRunID)
	var event domain.ConversationEvent
	var payload []byte
	var eventTurnID, eventRunID *string
	if err := tx.QueryRow(ctx, `
		INSERT INTO conversation_events (
			conversation_id, turn_id, turn_run_id, event_seq, event_key,
			schema_version, event_type, payload, context_included
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8::jsonb, $9)
		RETURNING id::text, conversation_id::text, turn_id::text, turn_run_id::text,
			event_seq, event_key, schema_version, event_type, payload, context_included, created_at
	`, params.ConversationID, turnID, runID, eventSeq, params.EventKey, params.SchemaVersion,
		params.EventType, params.Payload, params.ContextIncluded).Scan(
		&event.ID, &event.ConversationID, &eventTurnID, &eventRunID, &event.EventSeq,
		&event.EventKey, &event.SchemaVersion, &event.EventType, &payload,
		&event.ContextIncluded, &event.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert conversation event: %w", err)
	}
	event.Payload = payload
	if eventTurnID != nil {
		event.TurnID = *eventTurnID
	}
	if eventRunID != nil {
		event.TurnRunID = *eventRunID
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit conversation event: %w", err)
	}
	return &event, nil
}

func (r *ConversationEventRepository) ListContextEvents(ctx context.Context, conversationID string, fromSeq int64, toSeq int64) ([]domain.ConversationEvent, error) {
	return r.list(ctx, conversationID, fromSeq, toSeq, true, 0)
}

func (r *ConversationEventRepository) ListConversationEvents(ctx context.Context, conversationID string, limit int, beforeSeq int64, afterSeq int64) ([]domain.ConversationEvent, error) {
	return r.list(ctx, conversationID, beforeSeq, afterSeq, false, limit)
}

func (r *ConversationEventRepository) ListConversationEventsByTurn(ctx context.Context, turnID string) ([]domain.ConversationEvent, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text, conversation_id::text, turn_id::text, turn_run_id::text,
		event_seq, event_key, schema_version, event_type, payload, context_included, created_at
		FROM conversation_events WHERE turn_id = $1::uuid ORDER BY event_seq ASC`, turnID)
	if err != nil {
		return nil, fmt.Errorf("list conversation events by turn: %w", err)
	}
	defer rows.Close()

	events := make([]domain.ConversationEvent, 0)
	for rows.Next() {
		var event domain.ConversationEvent
		var turnID, runID *string
		if err := rows.Scan(&event.ID, &event.ConversationID, &turnID, &runID, &event.EventSeq,
			&event.EventKey, &event.SchemaVersion, &event.EventType, &event.Payload,
			&event.ContextIncluded, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation event by turn: %w", err)
		}
		if turnID != nil {
			event.TurnID = *turnID
		}
		if runID != nil {
			event.TurnRunID = *runID
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation events by turn: %w", err)
	}
	return events, nil
}

func (r *ConversationEventRepository) ListConversationEventsByRun(ctx context.Context, runID string) ([]domain.ConversationEvent, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text, conversation_id::text, turn_id::text, turn_run_id::text,
		event_seq, event_key, schema_version, event_type, payload, context_included, created_at
		FROM conversation_events WHERE turn_run_id = $1::uuid ORDER BY event_seq ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("list conversation events by run: %w", err)
	}
	defer rows.Close()

	events := make([]domain.ConversationEvent, 0)
	for rows.Next() {
		var event domain.ConversationEvent
		var turnID, eventRunID *string
		if err := rows.Scan(&event.ID, &event.ConversationID, &turnID, &eventRunID, &event.EventSeq,
			&event.EventKey, &event.SchemaVersion, &event.EventType, &event.Payload,
			&event.ContextIncluded, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation event by run: %w", err)
		}
		if turnID != nil {
			event.TurnID = *turnID
		}
		if eventRunID != nil {
			event.TurnRunID = *eventRunID
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation events by run: %w", err)
	}
	return events, nil
}

func (r *ConversationEventRepository) list(ctx context.Context, conversationID string, firstSeq int64, secondSeq int64, contextOnly bool, limit int) ([]domain.ConversationEvent, error) {
	conditions := []string{"conversation_id = $1::uuid"}
	args := []any{conversationID}
	if contextOnly {
		conditions = append(conditions, "context_included = true")
		if firstSeq > 0 {
			args = append(args, firstSeq)
			conditions = append(conditions, fmt.Sprintf("event_seq > $%d", len(args)))
		}
		if secondSeq > 0 {
			args = append(args, secondSeq)
			conditions = append(conditions, fmt.Sprintf("event_seq <= $%d", len(args)))
		}
	} else {
		if firstSeq > 0 {
			args = append(args, firstSeq)
			conditions = append(conditions, fmt.Sprintf("event_seq < $%d", len(args)))
		}
		if secondSeq > 0 {
			args = append(args, secondSeq)
			conditions = append(conditions, fmt.Sprintf("event_seq > $%d", len(args)))
		}
	}
	query := `SELECT id::text, conversation_id::text, turn_id::text, turn_run_id::text,
		event_seq, event_key, schema_version, event_type, payload, context_included, created_at
		FROM conversation_events WHERE ` + strings.Join(conditions, " AND ") + ` ORDER BY event_seq ASC`
	if !contextOnly && secondSeq == 0 && limit > 0 {
		query = `SELECT * FROM (` + strings.TrimSuffix(query, " ORDER BY event_seq ASC") + ` ORDER BY event_seq DESC`
	}
	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
		if !contextOnly && secondSeq == 0 {
			query += ") recent_events ORDER BY event_seq ASC"
		}
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversation events: %w", err)
	}
	defer rows.Close()

	events := make([]domain.ConversationEvent, 0)
	for rows.Next() {
		var event domain.ConversationEvent
		var turnID, runID *string
		if err := rows.Scan(&event.ID, &event.ConversationID, &turnID, &runID, &event.EventSeq,
			&event.EventKey, &event.SchemaVersion, &event.EventType, &event.Payload,
			&event.ContextIncluded, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation event: %w", err)
		}
		if turnID != nil {
			event.TurnID = *turnID
		}
		if runID != nil {
			event.TurnRunID = *runID
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation events: %w", err)
	}
	return events, nil
}

func nullableUUIDString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
