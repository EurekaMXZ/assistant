package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/jackc/pgx/v5"
)

func prepareAssistantText(text string) (string, int) {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		normalized = " "
	}
	return normalized, domain.EstimateTokens(normalized)
}

func prepareAssistantDrafts(drafts []domain.AssistantMessageDraft) []domain.AssistantMessageDraft {
	prepared := make([]domain.AssistantMessageDraft, 0, len(drafts))
	for _, draft := range drafts {
		if strings.TrimSpace(draft.ContentText) == "" {
			continue
		}
		prepared = append(prepared, draft)
	}
	if len(prepared) == 0 {
		prepared = append(prepared, domain.AssistantMessageDraft{ContentText: " "})
	}
	return prepared
}

func buildTurnRunMetadata(existing json.RawMessage, summary domain.TurnRunSummary) (json.RawMessage, error) {
	metadata := decodeMetadata(existing)
	metadata["run"] = map[string]any{
		"model":         summary.Model,
		"input_tokens":  summary.InputTokens,
		"output_tokens": summary.OutputTokens,
		"total_tokens":  summary.TotalTokens,
	}

	merged, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal turn metadata: %w", err)
	}
	return normalizedJSON(merged), nil
}

func insertAssistantMessage(ctx context.Context, tx pgx.Tx, turn *domain.Turn, assistantSeq int64, assistantText string, assistantTokens int, metadata json.RawMessage) (*domain.Message, error) {
	if len(metadata) == 0 || strings.TrimSpace(string(metadata)) == "" {
		metadata = json.RawMessage(`{}`)
	}
	row := tx.QueryRow(ctx, `
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
	`, turn.ConversationID, turn.ID, assistantSeq, domain.RoleAssistant, assistantText, assistantTokens, metadata)

	message, err := scanMessage(row)
	if err != nil {
		return nil, fmt.Errorf("insert assistant message: %w", err)
	}
	return message, nil
}

func updateTurnSuccess(ctx context.Context, tx pgx.Tx, turnID string, summary domain.TurnRunSummary, metadata json.RawMessage) (*domain.Turn, error) {
	row := tx.QueryRow(ctx, `
		UPDATE turns
		SET
			status = $2,
			request_blob_key = $3,
			response_blob_key = $4,
			stream_blob_key = $5,
			openai_response_id = $6,
			metadata = $7::jsonb,
			completed_at = now(),
			error_code = NULL,
			error_message = NULL,
			failed_at = NULL
		WHERE id = $1::uuid
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
	`, turnID, domain.TurnStatusCompleted, summary.RequestBlobKey, summary.ResponseBlobKey, summary.StreamBlobKey, summary.ResponseID, metadata)

	turn, err := scanTurn(row)
	if err != nil {
		return nil, fmt.Errorf("update turn success: %w", err)
	}
	return turn, nil
}

func updateContextHeadAfterAssistant(ctx context.Context, tx pgx.Tx, conversationID string, assistantSeq int64, activeTokens int) (*domain.ContextHead, error) {
	row := tx.QueryRow(ctx, `
		UPDATE context_heads
		SET
			last_seq = $2,
			active_context_tokens = $3
		WHERE conversation_id = $1::uuid
		RETURNING
			conversation_id::text,
			anchor_generation,
			COALESCE(anchor_key, ''),
			covered_until_seq,
			raw_tail_start_seq,
			last_seq,
			active_context_tokens,
			updated_at
	`, conversationID, assistantSeq, activeTokens)

	head, err := scanContextHead(row)
	if err != nil {
		return nil, fmt.Errorf("update context head: %w", err)
	}
	return head, nil
}

func shouldRequestCompaction(head *domain.ContextHead, compactTriggerTokens int) bool {
	return head != nil && head.ActiveContextTokens > compactTriggerTokens && head.RawTailStartSeq <= head.LastSeq
}

func enqueueCompactionRequest(ctx context.Context, tx pgx.Tx, turn *domain.Turn, triggerCompact bool) error {
	if !triggerCompact {
		return nil
	}

	return insertOutboxEvent(ctx, tx, outboxInsert{
		EventType:      workflow.EventContextCompactionRequest,
		ConversationID: turn.ConversationID,
		TurnID:         turn.ID,
	})
}
