package postgres

import (
	"context"
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
	}

	mergedMetadata, err := buildTurnRunMetadata(turn.Metadata, summary)
	if err != nil {
		return nil, nil, nil, false, err
	}

	turn, err = updateTurnSuccess(ctx, tx, turn.ID, summary, mergedMetadata)
	if err != nil {
		return nil, nil, nil, false, err
	}

	activeTokens := head.ActiveContextTokens + assistantTokens
	if turn.RetryOfTurnID != "" {
		activeTokens = max(0, head.ActiveContextTokens-replacedTokens) + selectedUserTokens + assistantTokens
	}
	head, err = updateContextHeadAfterAssistant(ctx, tx, turn.ConversationID, assistantSeq, activeTokens)
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
