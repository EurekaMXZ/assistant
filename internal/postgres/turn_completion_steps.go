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
			var metadata struct {
				AttachmentIDs []string `json:"attachment_ids"`
			}
			if json.Unmarshal(draft.Metadata, &metadata) != nil || len(metadata.AttachmentIDs) == 0 {
				continue
			}
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
		"model":                 summary.Model,
		"context_window_tokens": summary.ContextWindowTokens,
		"input_tokens":          summary.InputTokens,
		"output_tokens":         summary.OutputTokens,
		"total_tokens":          summary.TotalTokens,
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
			metadata,
			context_excluded
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::jsonb, $8)
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
	`, turn.ConversationID, turn.ID, assistantSeq, domain.RoleAssistant, assistantText, assistantTokens, metadata, false)

	message, err := scanMessage(row)
	if err != nil {
		return nil, fmt.Errorf("insert assistant message: %w", err)
	}
	return message, nil
}

func switchTurnVariantMessages(ctx context.Context, tx pgx.Tx, turn *domain.Turn) (int, int, error) {
	if turn == nil || turn.RetryOfTurnID == "" {
		return 0, 0, nil
	}
	rows, err := tx.Query(ctx, `
		UPDATE messages
		SET context_excluded = true
		WHERE context_excluded = false
			AND turn_id IN (
				SELECT id
				FROM turns
				WHERE id = $1::uuid OR retry_of_turn_id = $1::uuid
			)
		RETURNING COALESCE(token_count, 0)
	`, turn.RetryOfTurnID)
	if err != nil {
		return 0, 0, fmt.Errorf("exclude previous turn variant: %w", err)
	}
	defer rows.Close()

	tokens := 0
	for rows.Next() {
		var count int
		if err := rows.Scan(&count); err != nil {
			return 0, 0, fmt.Errorf("scan excluded turn variant tokens: %w", err)
		}
		tokens += count
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate excluded turn variants: %w", err)
	}
	var selectedUserTokens int
	if err := tx.QueryRow(ctx, `
		UPDATE messages
		SET context_excluded = false
		WHERE turn_id = $1::uuid AND role = $2
		RETURNING COALESCE(token_count, 0)
	`, turn.ID, domain.RoleUser).Scan(&selectedUserTokens); err != nil {
		return 0, 0, fmt.Errorf("activate retry user message: %w", err)
	}
	return tokens, selectedUserTokens, nil
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
	`, turnID, domain.TurnStatusCompleted, summary.RequestBlobKey, summary.ResponseBlobKey, summary.StreamBlobKey, summary.ResponseID, metadata)

	turn, err := scanTurn(row)
	if err != nil {
		return nil, fmt.Errorf("update turn success: %w", err)
	}
	return turn, nil
}

func updateContextHeadAfterAssistant(ctx context.Context, tx pgx.Tx, conversationID string, assistantSeq int64, activeTokens int, summary domain.TurnRunSummary) (*domain.ContextHead, error) {
	row := tx.QueryRow(ctx, `
		UPDATE context_heads
		SET
			last_seq = $2,
			active_context_tokens = $3,
			version = version + 1,
			latest_request_run_id = COALESCE(NULLIF($4, '')::uuid, latest_request_run_id),
			latest_successful_run_id = COALESCE(NULLIF($4, '')::uuid, latest_successful_run_id),
			latest_checkpoint_key = COALESCE(NULLIF($5, ''), latest_checkpoint_key),
			latest_checkpoint_checksum = CASE WHEN $5 <> '' THEN NULLIF($6, '') ELSE latest_checkpoint_checksum END,
			checkpoint_covered_event_seq = CASE WHEN $5 <> '' THEN last_context_event_seq ELSE checkpoint_covered_event_seq END
		WHERE conversation_id = $1::uuid
		RETURNING `+contextHeadColumns+`
	`, conversationID, assistantSeq, activeTokens, summary.RunID, summary.CheckpointBlobKey, summary.CheckpointChecksum)

	head, err := scanContextHead(row)
	if err != nil {
		return nil, fmt.Errorf("update context head: %w", err)
	}
	return head, nil
}

func shouldRequestCompaction(head *domain.ContextHead, compactTriggerTokens int) bool {
	return head != nil && compactTriggerTokens > 0 && head.ActiveContextTokens >= compactTriggerTokens && head.RawTailStartSeq <= head.LastSeq
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
