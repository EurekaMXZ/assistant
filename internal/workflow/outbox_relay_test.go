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
	if err := relay.Flush(context.Background(), func(ctx context.Context, events []WorkflowEvent) error {
		published = append(published, events...)
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

func TestOutboxRelayPublishesClaimedEventsAsBatch(t *testing.T) {
	store := &stubOutboxStore{
		items: []OutboxEvent{
			{ID: "evt_1", EventType: EventTurnAccepted},
			{ID: "evt_2", EventType: EventTurnContextReady},
		},
	}
	relay := &OutboxRelay{
		settings: WorkflowSettings{OutboxBatchSize: 16},
		store:    store,
	}

	var publishCalls int
	var published []WorkflowEvent
	if err := relay.Flush(context.Background(), func(_ context.Context, events []WorkflowEvent) error {
		publishCalls++
		published = append(published, events...)
		return nil
	}); err != nil {
		t.Fatalf("flush outbox: %v", err)
	}

	if publishCalls != 1 {
		t.Fatalf("publish calls = %d, want 1", publishCalls)
	}
	if len(published) != 2 || published[0].ID != "evt_1" || published[1].ID != "evt_2" {
		t.Fatalf("unexpected published events: %#v", published)
	}
	if len(store.publishedIDs) != 2 || store.publishedIDs[0] != "evt_1" || store.publishedIDs[1] != "evt_2" {
		t.Fatalf("expected both events marked published, got %#v", store.publishedIDs)
	}
}

func TestOutboxRelayPublishesExplicitTurnRunID(t *testing.T) {
	store := &stubOutboxStore{items: []OutboxEvent{{
		ID: "evt_run", TurnRunID: "run_1",
	}}}
	relay := &OutboxRelay{settings: WorkflowSettings{OutboxBatchSize: 1}, store: store}

	var published WorkflowEvent
	if err := relay.Flush(context.Background(), func(_ context.Context, events []WorkflowEvent) error {
		published = events[0]
		return nil
	}); err != nil {
		t.Fatalf("flush outbox: %v", err)
	}
	if published.TurnRunID != "run_1" {
		t.Fatalf("turn_run_id = %q, want run_1", published.TurnRunID)
	}
}

func TestOutboxRelayPublishesToolExecutionReferences(t *testing.T) {
	store := &stubOutboxStore{items: []OutboxEvent{{
		ID: "evt-tool", EventType: EventToolCallRequested, ConversationID: "conv-1",
		TurnID: "turn-1", TurnRunID: "run-1", ToolCallID: "call-1", ExecutionGroup: 2,
	}}}
	relay := &OutboxRelay{settings: WorkflowSettings{OutboxBatchSize: 1}, store: store}
	var published WorkflowEvent
	if err := relay.Flush(context.Background(), func(_ context.Context, events []WorkflowEvent) error {
		published = events[0]
		return nil
	}); err != nil {
		t.Fatalf("flush outbox: %v", err)
	}
	if published.ToolCallID != "call-1" || published.ExecutionGroup != 2 {
		t.Fatalf("tool execution references = %#v", published)
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
			{
				ID:             "evt_3",
				EventType:      EventTurnRunRequested,
				ConversationID: "conv_2",
			},
		},
	}

	relay := &OutboxRelay{
		settings: WorkflowSettings{OutboxBatchSize: 16},
		store:    store,
	}

	err := relay.Flush(context.Background(), func(ctx context.Context, events []WorkflowEvent) error {
		return errors.New("kafka unavailable")
	})
	if err == nil {
		t.Fatal("expected flush to fail when publish fails")
	}
	if !strings.Contains(err.Error(), "evt_2") {
		t.Fatalf("expected event id in error, got %v", err)
	}
	if got := store.publishErrors["evt_2"]; got == "" {
		t.Fatal("expected publish error for evt_2 to be recorded")
	}
	if got := store.publishErrors["evt_3"]; got == "" {
		t.Fatal("expected publish error for evt_3 to be recorded")
	}
	if len(store.publishedIDs) != 0 {
		t.Fatalf("did not expect published marks, got %#v", store.publishedIDs)
	}
}
