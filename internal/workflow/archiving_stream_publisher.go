package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
)

type ArchivingStreamPublisher struct {
	next   stream.Publisher
	store  TurnArtifactStore
	events TurnStreamEventStore

	mu sync.Mutex
}

func NewArchivingStreamPublisher(next stream.Publisher, store TurnArtifactStore, events TurnStreamEventStore) *ArchivingStreamPublisher {
	return &ArchivingStreamPublisher{
		next:   next,
		store:  store,
		events: events,
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
	if p == nil {
		return event, nil
	}
	if strings.TrimSpace(event.ConversationID) == "" || strings.TrimSpace(event.TurnID) == "" {
		return event, nil
	}

	raw, err := json.Marshal(event)
	if err != nil {
		return event, fmt.Errorf("marshal stream event archive payload: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error

	if p.store != nil {
		key := p.store.TurnStreamKey(event.ConversationID, event.TurnID)
		existing, err := p.store.GetBytes(ctx, key)
		switch {
		case err == nil:
			// keep existing bytes as-is
		case errors.Is(err, domain.ErrNotFound):
			existing = nil
		default:
			errs = append(errs, fmt.Errorf("get stream archive %q: %w", key, err))
			existing = nil
		}

		if len(existing) > 0 && existing[len(existing)-1] != '\n' {
			existing = append(existing, '\n')
		}
		existing = append(existing, raw...)
		existing = append(existing, '\n')

		if err := p.store.PutBytes(ctx, key, existing, "application/x-ndjson"); err != nil {
			errs = append(errs, fmt.Errorf("persist stream archive %q: %w", key, err))
		}
	}

	if p.events != nil {
		stored, err := p.events.AppendTurnStreamEvent(ctx, event.ConversationID, event.TurnID, event.Type, raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("persist turn stream event for turn %q: %w", event.TurnID, err))
		} else if stored != nil {
			event.EventIndex = stored.EventIndex
		}
	}

	return event, errors.Join(errs...)
}

var _ stream.Publisher = (*ArchivingStreamPublisher)(nil)
