package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
	kafkago "github.com/segmentio/kafka-go"
)

type recoveryReader struct {
	mu       sync.Mutex
	messages []kafkago.Message
	commits  []kafkago.Message
	closed   bool
	onCommit func()
}

func (r *recoveryReader) FetchMessage(ctx context.Context) (kafkago.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.messages) > 0 {
		message := r.messages[0]
		r.messages = r.messages[1:]
		return message, nil
	}
	if r.closed {
		return kafkago.Message{}, errors.New("reader closed")
	}
	select {
	case <-ctx.Done():
		return kafkago.Message{}, ctx.Err()
	default:
		return kafkago.Message{}, errors.New("no more messages")
	}
}

func (r *recoveryReader) CommitMessages(_ context.Context, messages ...kafkago.Message) error {
	r.mu.Lock()
	r.commits = append(r.commits, messages...)
	onCommit := r.onCommit
	r.mu.Unlock()
	if onCommit != nil {
		onCommit()
	}
	return nil
}

func (r *recoveryReader) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	return nil
}

type recoveryEventStore struct {
	mu     sync.Mutex
	inputs []domain.ConversationEventInput
}

func (s *recoveryEventStore) AppendCompleteEvent(_ context.Context, input domain.ConversationEventInput) (*domain.ConversationEvent, error) {
	s.mu.Lock()
	s.inputs = append(s.inputs, input)
	s.mu.Unlock()
	return &domain.ConversationEvent{EventKey: input.EventKey, EventType: input.EventType}, nil
}

func (*recoveryEventStore) ListContextEvents(context.Context, string, int64, int64) ([]domain.ConversationEvent, error) {
	return nil, nil
}

func (*recoveryEventStore) ListConversationEvents(context.Context, string, int, int64, int64) ([]domain.ConversationEvent, error) {
	return nil, nil
}

func (s *recoveryEventStore) snapshot() []domain.ConversationEventInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.ConversationEventInput(nil), s.inputs...)
}

func TestStreamRecoveryPersistsBeforeCommit(t *testing.T) {
	makeEvent := func(event stream.Event) kafkago.Message {
		payload, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		return kafkago.Message{Value: payload}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &recoveryReader{}
	store := &recoveryEventStore{}
	reader.messages = []kafkago.Message{
		makeEvent(stream.Event{Type: stream.EventResponseStarted, ConversationID: "conv-1", TurnID: "turn-1", RunID: "run-1", ResponseID: "resp-1"}),
		makeEvent(stream.Event{Type: "response.output_text.delta", ConversationID: "conv-1", TurnID: "turn-1", RunID: "run-1", ItemID: "item-1", TransportSeq: 1, Delta: "hello"}),
		makeEvent(stream.Event{Type: "response.output_text.done", ConversationID: "conv-1", TurnID: "turn-1", RunID: "run-1", ItemID: "item-1", ResponseID: "resp-1"}),
		makeEvent(stream.Event{Type: stream.EventResponseCompleted, ConversationID: "conv-1", TurnID: "turn-1", RunID: "run-1", ResponseID: "resp-1"}),
	}
	reader.onCommit = func() {
		reader.mu.Lock()
		remaining := len(reader.messages)
		reader.mu.Unlock()
		if remaining == 0 {
			cancel()
		}
	}
	recovery := &StreamRecovery{events: store, newReader: func() streamReader { return reader }}

	if err := recovery.Run(ctx); err != nil {
		t.Fatalf("run stream recovery: %v", err)
	}

	inputs := store.snapshot()
	if len(inputs) != 2 {
		t.Fatalf("persisted inputs = %d, want 2", len(inputs))
	}
	if inputs[0].EventType != domain.ConversationEventOutputTextCompleted || inputs[1].EventType != domain.ConversationEventRunCompleted {
		t.Fatalf("unexpected recovered event types: %#v", inputs)
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if len(reader.commits) != 4 {
		t.Fatalf("commits = %d, want 4", len(reader.commits))
	}
}

func TestStreamRecoveryCommitsMalformedMessages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &recoveryReader{messages: []kafkago.Message{{Value: []byte("{")}}}
	reader.onCommit = cancel
	recovery := &StreamRecovery{events: &recoveryEventStore{}, newReader: func() streamReader { return reader }}

	if err := recovery.Run(ctx); err != nil {
		t.Fatalf("run malformed stream recovery: %v", err)
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if len(reader.commits) != 1 {
		t.Fatalf("commits = %d, want 1", len(reader.commits))
	}
}
