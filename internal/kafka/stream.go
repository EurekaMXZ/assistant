package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/stream"
	kafkago "github.com/segmentio/kafka-go"
)

type StreamPublisher struct {
	writer *kafkago.Writer
	next   stream.Publisher
}

func NewStreamPublisher(settings Settings, next stream.Publisher) *StreamPublisher {
	return &StreamPublisher{
		writer: &kafkago.Writer{
			Addr:         kafkago.TCP(settings.Brokers...),
			Topic:        settings.EffectiveStreamTopic(),
			Balancer:     &kafkago.Hash{},
			RequiredAcks: kafkago.RequireAll,
			Async:        true,
		},
		next: next,
	}
}

func (p *StreamPublisher) Publish(ctx context.Context, event stream.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal kafka stream event: %w", err)
	}
	var errs []error
	if p != nil && p.writer != nil {
		if err := p.writer.WriteMessages(ctx, kafkago.Message{Key: []byte(event.TurnID), Value: payload}); err != nil {
			errs = append(errs, fmt.Errorf("publish kafka stream event: %w", err))
		}
	}
	if p != nil && p.next != nil {
		if err := p.next.Publish(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (p *StreamPublisher) Run(ctx context.Context) error {
	<-ctx.Done()
	if p == nil || p.writer == nil {
		return nil
	}
	return p.writer.Close()
}

var _ stream.Publisher = (*StreamPublisher)(nil)
