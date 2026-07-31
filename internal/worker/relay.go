package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/segmentio/kafka-go"
)

func (s *Service) relayLoop(ctx context.Context) {
	interval := s.settings.OutboxPollInterval
	if interval <= 0 {
		interval = s.settings.WorkerPollInterval
	}
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}

		if err := s.engine.FlushOutbox(ctx, s.publishWorkflowEvents); err != nil && ctx.Err() == nil {
			s.logger.Printf("outbox relay: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) publishWorkflowEvents(ctx context.Context, events []workflow.WorkflowEvent) error {
	messages := make([]kafka.Message, 0, len(events))
	for _, event := range events {
		value, err := json.Marshal(event)
		if err != nil {
			return err
		}
		messages = append(messages, kafka.Message{
			Key:   []byte(event.ConversationID),
			Value: value,
			Time:  event.CreatedAt,
		})
	}

	return s.writer.WriteMessages(ctx, messages...)
}
