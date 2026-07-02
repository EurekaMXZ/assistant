package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/segmentio/kafka-go"
)

func (s *Service) relayLoop(ctx context.Context) {
	ticker := time.NewTicker(s.settings.WorkerPollInterval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}

		if err := s.engine.FlushOutbox(ctx, s.publishWorkflowEvent); err != nil && ctx.Err() == nil {
			s.logger.Printf("outbox relay: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) publishWorkflowEvent(ctx context.Context, event workflow.WorkflowEvent) error {
	value, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return s.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(event.ConversationID),
		Value: value,
		Time:  event.CreatedAt,
	})
}
