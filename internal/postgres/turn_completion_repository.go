package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *WorkflowTurnRepository) FinalizeTurnSuccess(ctx context.Context, turnID string, assistantDrafts []domain.AssistantMessageDraft, summary domain.TurnRunSummary, compactTriggerTokens int) (*domain.Turn, []domain.Message, *domain.ContextHead, bool, error) {
	assistantDrafts = prepareAssistantDrafts(assistantDrafts)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	turn, err := lockTurnForCompletion(ctx, tx, turnID)
	if err != nil {
		return nil, nil, nil, false, err
	}

	head, err := queryContextHeadForUpdate(ctx, tx, turn.ConversationID)
	if err != nil {
		return nil, nil, nil, false, err
	}
	replacedTokens, selectedUserTokens, err := switchTurnVariantMessages(ctx, tx, turn)
	if err != nil {
		return nil, nil, nil, false, err
	}

	assistantSeq := head.LastSeq
	assistantMessages := make([]domain.Message, 0, len(assistantDrafts))
	assistantTokens := 0
	for _, draft := range assistantDrafts {
		assistantSeq++
		assistantText, tokens := prepareAssistantText(draft.ContentText)
		assistantTokens += tokens
		assistantMessage, err := insertAssistantMessage(ctx, tx, turn, assistantSeq, assistantText, tokens, draft.Metadata)
		if err != nil {
			return nil, nil, nil, false, err
		}
		assistantMessages = append(assistantMessages, *assistantMessage)
		payload, err := json.Marshal(map[string]any{"message": assistantMessage})
		if err != nil {
			return nil, nil, nil, false, fmt.Errorf("marshal assistant complete event: %w", err)
		}
		if err := insertCompleteEvent(ctx, tx, head, domain.ConversationEventInput{
			ConversationID:  turn.ConversationID,
			TurnID:          turn.ID,
			TurnRunID:       summary.RunID,
			EventKey:        "message:" + assistantMessage.ID,
			SchemaVersion:   1,
			EventType:       "message.completed",
			Payload:         payload,
			ContextIncluded: true,
		}); err != nil {
			return nil, nil, nil, false, err
		}
	}

	mergedMetadata, err := buildTurnRunMetadata(turn.Metadata, summary)
	if err != nil {
		return nil, nil, nil, false, err
	}

	turn, err = updateTurnSuccess(ctx, tx, turn.ID, summary, mergedMetadata)
	if err != nil {
		return nil, nil, nil, false, err
	}
	turnPayload, err := json.Marshal(map[string]any{"turn": turn})
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("marshal turn completion event: %w", err)
	}
	if err := insertCompleteEvent(ctx, tx, head, domain.ConversationEventInput{
		ConversationID: turn.ConversationID, TurnID: turn.ID, TurnRunID: summary.RunID,
		EventKey: "turn:" + turn.ID + ":completed", SchemaVersion: 1,
		EventType: domain.ConversationEventTurnCompleted, Payload: turnPayload, ContextIncluded: false,
	}); err != nil {
		return nil, nil, nil, false, err
	}

	activeTokens := activeContextTokensAfterTurn(head, turn, summary, assistantTokens, replacedTokens, selectedUserTokens)
	head, err = updateContextHeadAfterAssistant(ctx, tx, turn.ConversationID, assistantSeq, activeTokens, summary)
	if err != nil {
		return nil, nil, nil, false, err
	}

	triggerCompact := shouldRequestCompaction(head, compactTriggerTokens)
	if err := enqueueCompactionRequest(ctx, tx, turn, triggerCompact); err != nil {
		return nil, nil, nil, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, nil, false, fmt.Errorf("commit turn success: %w", err)
	}

	return turn, assistantMessages, head, triggerCompact, nil
}

func activeContextTokensAfterTurn(head *domain.ContextHead, turn *domain.Turn, summary domain.TurnRunSummary, assistantTokens int, replacedTokens int, selectedUserTokens int) int {
	providerTokens := summary.TotalTokens
	if providerTokens <= 0 {
		providerTokens = summary.InputTokens + summary.OutputTokens
	}
	if providerTokens > 0 {
		return providerTokens
	}

	activeTokens := assistantTokens
	if head != nil {
		activeTokens += head.ActiveContextTokens
	}
	if turn != nil && turn.RetryOfTurnID != "" {
		activeTokens = max(0, activeTokens-assistantTokens-replacedTokens) + selectedUserTokens + assistantTokens
	}
	return activeTokens
}
