package workflow

import (
	"context"
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

type stubTurnArtifactStore struct {
	data   map[string][]byte
	getErr error
	putErr error
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

func (s *stubArchiveStreamPublisher) Publish(_ context.Context, event stream.Event) error {
	s.events = append(s.events, event)
	return s.err
}

type stubCompleteEventStore struct {
	inputs []domain.ConversationEventInput
	err    error
}

func (s *stubCompleteEventStore) AppendCompleteEvent(_ context.Context, input domain.ConversationEventInput) (*domain.ConversationEvent, error) {
	s.inputs = append(s.inputs, input)
	if s.err != nil {
		return nil, s.err
	}
	return &domain.ConversationEvent{EventSeq: int64(len(s.inputs))}, nil
}

func (s *stubCompleteEventStore) ListContextEvents(context.Context, string, int64, int64) ([]domain.ConversationEvent, error) {
	return nil, nil
}

func (s *stubCompleteEventStore) ListConversationEvents(context.Context, string, int, int64, int64) ([]domain.ConversationEvent, error) {
	return nil, nil
}

func TestArchivingStreamPublisherForwardsLiveDeltasAndPersistsOnlyCompletedItems(t *testing.T) {
	next := &stubArchiveStreamPublisher{}
	complete := &stubCompleteEventStore{}
	publisher := NewArchivingStreamPublisher(next, nil, nil, complete)

	for _, event := range []stream.Event{
		{Type: "response.output_text.delta", ConversationID: "conv-1", TurnID: "turn-1", RunID: "run-1", ItemID: "item-1", TransportSeq: 1, Delta: "hel"},
		{Type: "response.output_text.delta", ConversationID: "conv-1", TurnID: "turn-1", RunID: "run-1", ItemID: "item-1", TransportSeq: 2, Delta: "lo"},
		{Type: "response.output_text.done", ConversationID: "conv-1", TurnID: "turn-1", RunID: "run-1", ItemID: "item-1"},
	} {
		if err := publisher.Publish(t.Context(), event); err != nil {
			t.Fatalf("publish %s: %v", event.Type, err)
		}
	}
	if len(next.events) != 3 {
		t.Fatalf("live events = %d, want 3", len(next.events))
	}
	if len(complete.inputs) != 1 {
		t.Fatalf("complete events = %d, want 1", len(complete.inputs))
	}
	if complete.inputs[0].EventType != domain.ConversationEventOutputTextCompleted || !strings.Contains(string(complete.inputs[0].Payload), `"text":"hello"`) {
		t.Fatalf("unexpected complete event: %#v", complete.inputs[0])
	}
	if next.events[2].EventIndex != 1 {
		t.Fatalf("forwarded durable event index = %d, want 1", next.events[2].EventIndex)
	}
}

func TestArchivingStreamPublisherFlushesInterruptedTextOnFailure(t *testing.T) {
	complete := &stubCompleteEventStore{}
	publisher := NewArchivingStreamPublisher(nil, nil, nil, complete)
	if err := publisher.Publish(t.Context(), stream.Event{
		Type: "response.output_text.delta", ConversationID: "conv-2", TurnID: "turn-2", RunID: "run-2", ItemID: "item-2", TransportSeq: 1, Delta: "partial",
	}); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(t.Context(), stream.Event{
		Type: stream.EventResponseFailed, ConversationID: "conv-2", TurnID: "turn-2", RunID: "run-2",
	}); err != nil {
		t.Fatal(err)
	}
	if len(complete.inputs) != 2 || complete.inputs[0].EventType != domain.ConversationEventOutputTextInterrupted || complete.inputs[1].EventType != domain.ConversationEventRunFailed {
		t.Fatalf("unexpected interrupted events: %#v", complete.inputs)
	}
}

func TestArchivingStreamPublisherReturnsCompleteStoreErrorsWithoutSkippingLivePublish(t *testing.T) {
	next := &stubArchiveStreamPublisher{}
	complete := &stubCompleteEventStore{err: errors.New("postgres down")}
	publisher := NewArchivingStreamPublisher(next, nil, nil, complete)

	err := publisher.Publish(t.Context(), stream.Event{
		Type: "response.output_text.done", ConversationID: "conv-3", TurnID: "turn-3", RunID: "run-3", ItemID: "item-3", Text: "done",
	})
	if err == nil || !strings.Contains(err.Error(), "persist complete stream event") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(next.events) != 1 {
		t.Fatalf("live events = %d, want 1", len(next.events))
	}
}

func TestArchivingStreamPublisherSkipsAccumulatorWithoutTurnIdentity(t *testing.T) {
	next := &stubArchiveStreamPublisher{}
	complete := &stubCompleteEventStore{}
	publisher := NewArchivingStreamPublisher(next, nil, nil, complete)
	if err := publisher.Publish(t.Context(), stream.Event{Type: "response.output_text.done", Text: "done"}); err != nil {
		t.Fatal(err)
	}
	if len(next.events) != 1 || len(complete.inputs) != 0 {
		t.Fatalf("unexpected publish state: live=%d complete=%d", len(next.events), len(complete.inputs))
	}
}
