package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	kafkago "github.com/segmentio/kafka-go"
)

type streamReader interface {
	FetchMessage(context.Context) (kafkago.Message, error)
	CommitMessages(context.Context, ...kafkago.Message) error
	Close() error
}

// StreamRecovery replays retained provider deltas into the complete-event store.
// Its consumer group is separate from the live workflow consumer so recovery can
// start at the beginning of the configured Kafka retention window.
type StreamRecovery struct {
	newReader func() streamReader
	events    workflow.CompleteEventStore
}

func NewStreamRecovery(settings Settings, workflowGroup string, events workflow.CompleteEventStore) *StreamRecovery {
	group := strings.TrimSpace(workflowGroup)
	if group == "" {
		group = "assistant-workers"
	}
	group += "-stream-recovery"
	return &StreamRecovery{
		events: events,
		newReader: func() streamReader {
			return NewStreamReader(settings, group)
		},
	}
}

func NewStreamReader(settings Settings, consumerGroup string) *kafkago.Reader {
	return kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        settings.Brokers,
		GroupID:        consumerGroup,
		Topic:          settings.EffectiveStreamTopic(),
		CommitInterval: 0,
		StartOffset:    kafkago.FirstOffset,
		MinBytes:       1,
		MaxBytes:       10 << 20,
	})
}

func (r *StreamRecovery) Run(ctx context.Context) error {
	if r == nil || r.events == nil || r.newReader == nil {
		return errors.New("kafka stream recovery is not configured")
	}
	reader := r.newReader()
	if reader == nil {
		return errors.New("kafka stream recovery reader is not configured")
	}
	defer reader.Close()

	accumulator := workflow.NewCompleteEventAccumulator()
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("fetch kafka stream recovery message: %w", err)
		}

		var event stream.Event
		if err := json.Unmarshal(message.Value, &event); err != nil {
			if err := reader.CommitMessages(ctx, message); err != nil {
				return fmt.Errorf("commit malformed kafka stream recovery message: %w", err)
			}
			continue
		}

		complete, err := accumulator.Apply(event)
		if err != nil {
			return fmt.Errorf("accumulate kafka stream recovery event: %w", err)
		}
		for _, input := range complete {
			if _, err := r.events.AppendCompleteEvent(ctx, input); err != nil {
				return fmt.Errorf("persist recovered complete event: %w", err)
			}
		}
		if err := reader.CommitMessages(ctx, message); err != nil {
			return fmt.Errorf("commit kafka stream recovery message: %w", err)
		}
	}
}
