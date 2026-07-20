package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
)

func insertCompleteEvent(ctx context.Context, tx pgx.Tx, head *domain.ContextHead, input domain.ConversationEventInput) error {
	if head == nil {
		return fmt.Errorf("context head is required for complete event")
	}
	if input.SchemaVersion <= 0 {
		input.SchemaVersion = 1
	}
	if len(input.Payload) == 0 {
		input.Payload = json.RawMessage(`{}`)
	}

	var existingSeq int64
	if err := tx.QueryRow(ctx, `
		SELECT event_seq
		FROM conversation_events
		WHERE conversation_id = $1::uuid AND event_key = $2
	`, input.ConversationID, input.EventKey).Scan(&existingSeq); err == nil {
		return nil
	} else if err != pgx.ErrNoRows {
		return fmt.Errorf("check complete event %q: %w", input.EventKey, err)
	}

	var eventSeq int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(event_seq), 0) + 1
		FROM conversation_events
		WHERE conversation_id = $1::uuid
	`, input.ConversationID).Scan(&eventSeq); err != nil {
		return fmt.Errorf("allocate complete event sequence: %w", err)
	}
	if input.ContextIncluded {
		head.LastContextEventSeq = eventSeq
		if _, err := tx.Exec(ctx, `
			UPDATE context_heads
			SET last_context_event_seq = $2
			WHERE conversation_id = $1::uuid
		`, input.ConversationID, eventSeq); err != nil {
			return fmt.Errorf("advance context event sequence: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO conversation_events (
			conversation_id, turn_id, turn_run_id, event_seq, event_key,
			schema_version, event_type, payload, context_included
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8::jsonb, $9)
	`, input.ConversationID, nullableUUIDString(input.TurnID), nullableUUIDString(input.TurnRunID),
		eventSeq, input.EventKey, input.SchemaVersion, input.EventType, input.Payload, input.ContextIncluded); err != nil {
		return fmt.Errorf("insert complete event %q: %w", input.EventKey, err)
	}
	return nil
}
