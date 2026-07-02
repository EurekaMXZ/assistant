package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type OutboxRelay struct {
	settings WorkflowSettings
	store    WorkflowOutboxRepository
}

func (r *OutboxRelay) Flush(ctx context.Context, publish WorkflowEventPublisher) error {
	items, err := r.store.ClaimPendingOutboxEvents(ctx, r.settings.WorkerLeaseTimeout, r.settings.OutboxBatchSize)
	if err != nil {
		return err
	}

	for _, item := range items {
		event := WorkflowEvent{
			ID:             item.ID,
			EventType:      item.EventType,
			ConversationID: item.ConversationID,
			TurnID:         item.TurnID,
			TurnRunID:      item.TurnRunID,
			CreatedAt:      item.CreatedAt,
		}

		if err := publish(ctx, event); err != nil {
			_ = r.store.MarkOutboxPublishError(ctx, item.ID, item.ClaimToken, err.Error())
			return fmt.Errorf("publish workflow event %s: %w", item.ID, err)
		}

		if err := r.store.MarkOutboxPublished(ctx, item.ID, item.ClaimToken); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("mark workflow event published %s: %w", item.ID, err)
		}
	}

	return nil
}
