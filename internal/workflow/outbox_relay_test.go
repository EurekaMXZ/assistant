package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type stubOutboxStore struct {
	items         []OutboxEvent
	listErr       error
	publishedIDs  []string
	publishErrors map[string]string
}

func (s *stubOutboxStore) ClaimPendingOutboxEvents(ctx context.Context, leaseTimeout time.Duration, limit int) ([]OutboxEvent, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.items, nil
}

func (s *stubOutboxStore) MarkOutboxPublished(ctx context.Context, eventID string, claimToken string) error {
	s.publishedIDs = append(s.publishedIDs, eventID)
	return nil
}

func (s *stubOutboxStore) MarkOutboxPublishError(ctx context.Context, eventID string, claimToken string, message string) error {
	if s.publishErrors == nil {
		s.publishErrors = map[string]string{}
	}
	s.publishErrors[eventID] = message
	return nil
}

func TestOutboxRelayMarksPublishedAfterPublish(t *testing.T) {
	store := &stubOutboxStore{
		items: []OutboxEvent{
			{
				ID:             "evt_1",
				EventType:      EventTurnAccepted,
				ConversationID: "conv_1",
				TurnID:         "turn_1",
				CreatedAt:      time.Unix(1, 0),
			},
		},
	}

	relay := &OutboxRelay{
		settings: WorkflowSettings{OutboxBatchSize: 16},
		store:    store,
	}

	var published []WorkflowEvent
	if err := relay.Flush(context.Background(), func(ctx context.Context, event WorkflowEvent) error {
		published = append(published, event)
		return nil
	}); err != nil {
		t.Fatalf("flush outbox: %v", err)
	}

	if len(published) != 1 || published[0].ID != "evt_1" {
		t.Fatalf("unexpected published events: %#v", published)
	}
	if len(store.publishedIDs) != 1 || store.publishedIDs[0] != "evt_1" {
		t.Fatalf("expected published mark for evt_1, got %#v", store.publishedIDs)
	}
	if len(store.publishErrors) != 0 {
		t.Fatalf("expected no publish errors, got %#v", store.publishErrors)
	}
}

func TestOutboxRelayPublishesExplicitTurnRunID(t *testing.T) {
	store := &stubOutboxStore{items: []OutboxEvent{{
		ID: "evt_run", TurnRunID: "run_1",
	}}}
	relay := &OutboxRelay{settings: WorkflowSettings{OutboxBatchSize: 1}, store: store}

	var published WorkflowEvent
	if err := relay.Flush(context.Background(), func(_ context.Context, event WorkflowEvent) error {
		published = event
		return nil
	}); err != nil {
		t.Fatalf("flush outbox: %v", err)
	}
	if published.TurnRunID != "run_1" {
		t.Fatalf("turn_run_id = %q, want run_1", published.TurnRunID)
	}
}

func TestOutboxRelayMarksPublishErrorOnPublisherFailure(t *testing.T) {
	store := &stubOutboxStore{
		items: []OutboxEvent{
			{
				ID:             "evt_2",
				EventType:      EventTurnContextReady,
				ConversationID: "conv_2",
				TurnID:         "turn_2",
			},
		},
	}

	relay := &OutboxRelay{
		settings: WorkflowSettings{OutboxBatchSize: 16},
		store:    store,
	}

	err := relay.Flush(context.Background(), func(ctx context.Context, event WorkflowEvent) error {
		return errors.New("kafka unavailable")
	})
	if err == nil {
		t.Fatal("expected flush to fail when publish fails")
	}
	if !strings.Contains(err.Error(), "evt_2") {
		t.Fatalf("expected event id in error, got %v", err)
	}
	if got := store.publishErrors["evt_2"]; got == "" {
		t.Fatal("expected publish error to be recorded")
	}
	if len(store.publishedIDs) != 0 {
		t.Fatalf("did not expect published marks, got %#v", store.publishedIDs)
	}
}
