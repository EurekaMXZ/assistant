package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
)

type ArchivingStreamPublisher struct {
	next           stream.Publisher
	completeEvents CompleteEventStore
	accumulator    *CompleteEventAccumulator
}

func NewArchivingStreamPublisher(next stream.Publisher, completeEvents CompleteEventStore) *ArchivingStreamPublisher {
	return &ArchivingStreamPublisher{
		next:           next,
		completeEvents: completeEvents,
		accumulator:    NewCompleteEventAccumulator(),
	}
}

func (p *ArchivingStreamPublisher) Publish(ctx context.Context, event stream.Event) error {
	var errs []error

	archivedEvents, err := p.archiveEvents(ctx, event)
	if err != nil {
		errs = append(errs, err)
	}

	if p.next != nil {
		for _, archivedEvent := range archivedEvents {
			if err := p.next.Publish(ctx, archivedEvent); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

func (p *ArchivingStreamPublisher) archiveEvents(ctx context.Context, event stream.Event) ([]stream.Event, error) {
	if p == nil || p.accumulator == nil {
		return []stream.Event{event}, errors.New("stream archive publisher is not configured")
	}
	if p.completeEvents == nil {
		return []stream.Event{event}, errors.New("stream archive publisher requires complete event store")
	}
	completed, err := p.accumulator.Apply(event)
	if err != nil {
		return []stream.Event{event}, err
	}
	archived := make([]stream.Event, 0, len(completed)+1)
	var errs []error
	for _, input := range completed {
		stored, err := p.completeEvents.AppendCompleteEvent(ctx, input)
		if err != nil {
			errs = append(errs, fmt.Errorf("persist complete stream event for turn %q: %w", event.TurnID, err))
			continue
		}
		if stored == nil {
			continue
		}
		if text, ok := completeOutputText(input); ok {
			if event.Type == "response.output_text.done" {
				event.Text = text
				event.EventIndex = stored.EventSeq
				continue
			}
			archived = append(archived, stream.Event{
				Type:           "response.output_text.done",
				EventIndex:     stored.EventSeq,
				ConversationID: input.ConversationID,
				TurnID:         input.TurnID,
				RunID:          input.TurnRunID,
				Text:           text,
				Payload:        string(input.Payload),
			})
			continue
		}
		event.EventIndex = stored.EventSeq
	}

	archived = append(archived, event)
	return archived, errors.Join(errs...)
}

func completeOutputText(input domain.ConversationEventInput) (string, bool) {
	if input.EventType != domain.ConversationEventOutputTextCompleted && input.EventType != domain.ConversationEventOutputTextInterrupted {
		return "", false
	}
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(input.Payload, &payload); err != nil {
		return "", false
	}
	return payload.Text, true
}

var _ stream.Publisher = (*ArchivingStreamPublisher)(nil)
