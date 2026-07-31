package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type OutboxRelay struct {
	settings WorkflowSettings
	store    WorkflowOutboxRepository
}

func (r *OutboxRelay) Flush(ctx context.Context, publish WorkflowEventBatchPublisher) error {
	items, err := r.store.ClaimPendingOutboxEvents(ctx, r.settings.WorkerLeaseTimeout, r.settings.OutboxBatchSize)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	events := make([]WorkflowEvent, 0, len(items))
	eventIDs := make([]string, 0, len(items))
	for _, item := range items {
		events = append(events, WorkflowEvent{
			ID:             item.ID,
			EventType:      item.EventType,
			ConversationID: item.ConversationID,
			TurnID:         item.TurnID,
			TurnRunID:      item.TurnRunID,
			ToolCallID:     item.ToolCallID,
			ExecutionGroup: item.ExecutionGroup,
			CreatedAt:      item.CreatedAt,
		})
		eventIDs = append(eventIDs, item.ID)
	}

	if err := publish(ctx, events); err != nil {
		for _, item := range items {
			_ = r.store.MarkOutboxPublishError(ctx, item.ID, item.ClaimToken, err.Error())
		}
		return fmt.Errorf("publish workflow events %s: %w", strings.Join(eventIDs, ","), err)
	}

	for _, item := range items {
		if err := r.store.MarkOutboxPublished(ctx, item.ID, item.ClaimToken); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("mark workflow event published %s: %w", item.ID, err)
		}
	}

	return nil
}
