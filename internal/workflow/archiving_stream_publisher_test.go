package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
)

type stubArchiveStreamPublisher struct {
	events []stream.Event
	err    error
}

func (s *stubArchiveStreamPublisher) Publish(_ context.Context, event stream.Event) error {
	s.events = append(s.events, event)
	return s.err
}

type stubTurnArtifactStore struct {
	data   map[string][]byte
	getErr error
	putErr error
}

type stubTurnStreamEventStore struct {
	conversationID string
	turnID         string
	eventType      string
	payload        []byte
	eventIndex     int64
	err            error
}

func (s *stubTurnStreamEventStore) AppendTurnStreamEvent(_ context.Context, conversationID string, turnID string, eventType string, payload json.RawMessage) (*domain.TurnStreamEvent, error) {
	s.conversationID = conversationID
	s.turnID = turnID
	s.eventType = eventType
	s.payload = append([]byte(nil), payload...)
	if s.err != nil {
		return nil, s.err
	}
	return &domain.TurnStreamEvent{EventIndex: s.eventIndex}, nil
}

func (s *stubTurnArtifactStore) PutBytes(_ context.Context, key string, data []byte, _ string) error {
	if s.putErr != nil {
		return s.putErr
	}
	if s.data == nil {
		s.data = map[string][]byte{}
	}
	s.data[key] = append([]byte(nil), data...)
	return nil
}

func (s *stubTurnArtifactStore) GetBytes(_ context.Context, key string) ([]byte, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.data == nil {
		return nil, domain.ErrNotFound
	}
	value, ok := s.data[key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

func (s *stubTurnArtifactStore) TurnRequestKey(conversationID, turnID string) string {
	return "requests/" + conversationID + "/" + turnID + ".json"
}

func (s *stubTurnArtifactStore) TurnResponseKey(conversationID, turnID string) string {
	return "responses/" + conversationID + "/" + turnID + ".json"
}

func (s *stubTurnArtifactStore) TurnStreamKey(conversationID, turnID string) string {
	return "stream-events/" + conversationID + "/" + turnID + ".jsonl"
}

func (s *stubTurnArtifactStore) TurnModelContextKey(conversationID, turnID string) string {
	return "turn-model-context/" + conversationID + "/" + turnID + ".json"
}

func TestArchivingStreamPublisherPublishesAndArchivesEvents(t *testing.T) {
	next := &stubArchiveStreamPublisher{}
	store := &stubTurnArtifactStore{}
	events := &stubTurnStreamEventStore{eventIndex: 42}
	publisher := NewArchivingStreamPublisher(next, store, events)

	event := stream.Event{
		Type:           stream.EventToolCompleted,
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		ToolName:       "sandbox.exec",
		Payload:        `{"status":"completed"}`,
	}

	if err := publisher.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(next.events) != 1 || next.events[0].Type != stream.EventToolCompleted {
		t.Fatalf("unexpected forwarded events: %#v", next.events)
	}
	if next.events[0].EventIndex != 42 {
		t.Fatalf("forwarded event index = %d, want 42", next.events[0].EventIndex)
	}
	key := "stream-events/conv-1/turn-1.jsonl"
	got := string(store.data[key])
	if !strings.Contains(got, `"type":"tool.completed"`) || !strings.Contains(got, `"tool_name":"sandbox.exec"`) {
		t.Fatalf("unexpected archived stream payload: %q", got)
	}
	if events.conversationID != "conv-1" || events.turnID != "turn-1" || events.eventType != stream.EventToolCompleted {
		t.Fatalf("unexpected persisted event identity: %#v", events)
	}
	if !strings.Contains(string(events.payload), `"type":"tool.completed"`) {
		t.Fatalf("unexpected persisted event payload: %s", events.payload)
	}
}

func TestArchivingStreamPublisherAppendsJSONLines(t *testing.T) {
	store := &stubTurnArtifactStore{}
	publisher := NewArchivingStreamPublisher(nil, store, nil)

	if err := publisher.Publish(context.Background(), stream.Event{
		Type:           "response.output_text.delta",
		ConversationID: "conv-2",
		TurnID:         "turn-2",
		Delta:          "he",
	}); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if err := publisher.Publish(context.Background(), stream.Event{
		Type:           stream.EventResponseCompleted,
		ConversationID: "conv-2",
		TurnID:         "turn-2",
		Text:           "hello",
	}); err != nil {
		t.Fatalf("second publish: %v", err)
	}

	got := string(store.data["stream-events/conv-2/turn-2.jsonl"])
	if lines := strings.Count(got, "\n"); lines != 2 {
		t.Fatalf("expected two jsonl lines, got %d in %q", lines, got)
	}
	if !strings.Contains(got, `"type":"response.output_text.delta"`) || !strings.Contains(got, `"type":"response.completed"`) {
		t.Fatalf("unexpected jsonl archive: %q", got)
	}
}

func TestArchivingStreamPublisherSkipsArchiveWhenTurnIdentityMissing(t *testing.T) {
	next := &stubArchiveStreamPublisher{}
	store := &stubTurnArtifactStore{}
	events := &stubTurnStreamEventStore{}
	publisher := NewArchivingStreamPublisher(next, store, events)

	if err := publisher.Publish(context.Background(), stream.Event{
		Type:       stream.EventResponseStarted,
		ResponseID: "resp-1",
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(next.events) != 1 {
		t.Fatalf("expected event to be forwarded, got %#v", next.events)
	}
	if len(store.data) != 0 {
		t.Fatalf("expected archive to be skipped, got %#v", store.data)
	}
	if len(events.payload) != 0 {
		t.Fatalf("expected event store persistence to be skipped, got %#v", events)
	}
}

func TestArchivingStreamPublisherReturnsArchiveErrorsWithoutSkippingLivePublish(t *testing.T) {
	next := &stubArchiveStreamPublisher{}
	store := &stubTurnArtifactStore{putErr: errors.New("minio down")}
	publisher := NewArchivingStreamPublisher(next, store, nil)

	err := publisher.Publish(context.Background(), stream.Event{
		Type:           stream.EventResponseFailed,
		ConversationID: "conv-3",
		TurnID:         "turn-3",
		Error:          "boom",
	})
	if err == nil || !strings.Contains(err.Error(), "persist stream archive") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(next.events) != 1 || next.events[0].Type != stream.EventResponseFailed {
		t.Fatalf("expected live publish to still happen, got %#v", next.events)
	}
}

func TestArchivingStreamPublisherReturnsEventStoreErrorsWithoutSkippingLivePublish(t *testing.T) {
	next := &stubArchiveStreamPublisher{}
	store := &stubTurnArtifactStore{}
	events := &stubTurnStreamEventStore{err: errors.New("postgres down")}
	publisher := NewArchivingStreamPublisher(next, store, events)

	err := publisher.Publish(context.Background(), stream.Event{
		Type:           stream.EventResponseCompleted,
		ConversationID: "conv-4",
		TurnID:         "turn-4",
		Text:           "done",
	})
	if err == nil || !strings.Contains(err.Error(), "persist turn stream event") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(next.events) != 1 || next.events[0].Type != stream.EventResponseCompleted {
		t.Fatalf("expected live publish to still happen, got %#v", next.events)
	}
	if got := string(store.data["stream-events/conv-4/turn-4.jsonl"]); !strings.Contains(got, `"type":"response.completed"`) {
		t.Fatalf("expected archive to still be written, got %q", got)
	}
}
