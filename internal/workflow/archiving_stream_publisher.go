package workflow

import (
	"context"
	"errors"
	"fmt"

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

	archivedEvent, err := p.archiveEvent(ctx, event)
	if err != nil {
		errs = append(errs, err)
	}
	event = archivedEvent

	if p.next != nil {
		if err := p.next.Publish(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (p *ArchivingStreamPublisher) archiveEvent(ctx context.Context, event stream.Event) (stream.Event, error) {
	if p == nil || p.accumulator == nil {
		return event, errors.New("stream archive publisher is not configured")
	}
	if p.completeEvents == nil {
		return event, errors.New("stream archive publisher requires complete event store")
	}
	completed, err := p.accumulator.Apply(event)
	if err != nil {
		return event, err
	}
	var errs []error
	for _, input := range completed {
		stored, err := p.completeEvents.AppendCompleteEvent(ctx, input)
		if err != nil {
			errs = append(errs, fmt.Errorf("persist complete stream event for turn %q: %w", event.TurnID, err))
		} else if stored != nil {
			event.EventIndex = stored.EventSeq
		}
	}

	return event, errors.Join(errs...)
}

var _ stream.Publisher = (*ArchivingStreamPublisher)(nil)
