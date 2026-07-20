package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/segmentio/kafka-go"
)

type stubWorkflowEngine struct {
	mu sync.Mutex

	flushCalls   int
	requeueCalls int
	handled      []workflow.WorkflowEvent

	flushErr   error
	requeueErr error
	handleErr  error

	onFlush   func()
	onRequeue func()
	onHandle  func(workflow.WorkflowEvent)
}

func (s *stubWorkflowEngine) FlushOutbox(ctx context.Context, publish workflow.WorkflowEventPublisher) error {
	s.mu.Lock()
	s.flushCalls++
	onFlush := s.onFlush
	err := s.flushErr
	s.mu.Unlock()

	if onFlush != nil {
		onFlush()
	}
	return err
}

func (s *stubWorkflowEngine) RequeueStaleTurns(ctx context.Context) (int, error) {
	s.mu.Lock()
	s.requeueCalls++
	onRequeue := s.onRequeue
	err := s.requeueErr
	s.mu.Unlock()

	if onRequeue != nil {
		onRequeue()
	}
	return 0, err
}

func (s *stubWorkflowEngine) HandleWorkflowEvent(ctx context.Context, event workflow.WorkflowEvent) error {
	s.mu.Lock()
	s.handled = append(s.handled, event)
	onHandle := s.onHandle
	err := s.handleErr
	s.mu.Unlock()

	if onHandle != nil {
		onHandle(event)
	}
	return err
}

func (s *stubWorkflowEngine) handledEvents() []workflow.WorkflowEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]workflow.WorkflowEvent(nil), s.handled...)
}

type stubWorkflowWriter struct {
	mu       sync.Mutex
	messages []kafka.Message
	writeErr error
	closed   bool
}

type blockingWorkflowWriter struct {
	started chan struct{}
	release chan struct{}
}

func (w *blockingWorkflowWriter) WriteMessages(context.Context, ...kafka.Message) error {
	return nil
}

func (w *blockingWorkflowWriter) Close() error {
	close(w.started)
	<-w.release
	return nil
}

func (s *stubWorkflowWriter) WriteMessages(ctx context.Context, messages ...kafka.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, messages...)
	return s.writeErr
}

func (s *stubWorkflowWriter) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	return nil
}

func (s *stubWorkflowWriter) snapshot() ([]kafka.Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]kafka.Message(nil), s.messages...), s.closed
}

type fetchResult struct {
	message kafka.Message
	err     error
}

type stubWorkflowReader struct {
	mu sync.Mutex

	fetches    []fetchResult
	fetchIndex int
	committed  []kafka.Message
	commitErr  error
	closed     bool
	commitHook func()
}

type closeUnblocksWorkflowReader struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func (r *closeUnblocksWorkflowReader) FetchMessage(context.Context) (kafka.Message, error) {
	r.startOnce.Do(func() { close(r.started) })
	<-r.closed
	return kafka.Message{}, errors.New("reader closed")
}

func (r *closeUnblocksWorkflowReader) CommitMessages(context.Context, ...kafka.Message) error {
	return nil
}

func (r *closeUnblocksWorkflowReader) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}

func (s *stubWorkflowReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	s.mu.Lock()
	if s.fetchIndex < len(s.fetches) {
		result := s.fetches[s.fetchIndex]
		s.fetchIndex++
		s.mu.Unlock()
		return result.message, result.err
	}
	s.mu.Unlock()

	<-ctx.Done()
	return kafka.Message{}, ctx.Err()
}

func (s *stubWorkflowReader) CommitMessages(ctx context.Context, messages ...kafka.Message) error {
	s.mu.Lock()
	s.committed = append(s.committed, messages...)
	hook := s.commitHook
	err := s.commitErr
	s.mu.Unlock()

	if hook != nil {
		hook()
	}
	return err
}

func (s *stubWorkflowReader) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	return nil
}

func (s *stubWorkflowReader) snapshot() ([]kafka.Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]kafka.Message(nil), s.committed...), s.closed
}

func testLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

func TestPublishWorkflowEventWritesKafkaMessage(t *testing.T) {
	writer := &stubWorkflowWriter{}
	service := &Service{
		writer: writer,
	}

	event := workflow.WorkflowEvent{
		ID:             "evt_1",
		EventType:      workflow.EventTurnAccepted,
		ConversationID: "conv_1",
		CreatedAt:      time.Unix(7, 0).UTC(),
	}

	if err := service.publishWorkflowEvent(context.Background(), event); err != nil {
		t.Fatalf("publish workflow event: %v", err)
	}

	messages, _ := writer.snapshot()
	if len(messages) != 1 {
		t.Fatalf("expected 1 kafka message, got %d", len(messages))
	}
	if got := string(messages[0].Key); got != "conv_1" {
		t.Fatalf("unexpected message key: %q", got)
	}
	if !messages[0].Time.Equal(event.CreatedAt) {
		t.Fatalf("unexpected message time: %v", messages[0].Time)
	}

	var published workflow.WorkflowEvent
	if err := json.Unmarshal(messages[0].Value, &published); err != nil {
		t.Fatalf("decode kafka payload: %v", err)
	}
	if published.ID != event.ID || published.EventType != event.EventType || published.ConversationID != event.ConversationID {
		t.Fatalf("unexpected published event: %#v", published)
	}
}

func TestCloseWriterHasBoundedWait(t *testing.T) {
	writer := &blockingWorkflowWriter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := &Service{
		logger:    testLogger(),
		writer:    writer,
		closeWait: 10 * time.Millisecond,
	}

	started := time.Now()
	service.closeWriter()
	elapsed := time.Since(started)
	close(writer.release)

	if elapsed >= 100*time.Millisecond {
		t.Fatalf("writer close elapsed = %v, want bounded wait", elapsed)
	}
	select {
	case <-writer.started:
	default:
		t.Fatal("writer close was not started")
	}
}

func TestConsumeCommitsInvalidPayloadAndSkipsEngine(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &stubWorkflowReader{
		fetches: []fetchResult{
			{message: kafka.Message{Value: []byte("{")}},
		},
		commitHook: cancel,
	}

	service := &Service{
		logger:    testLogger(),
		engine:    &stubWorkflowEngine{},
		newReader: func() WorkflowReader { return reader },
		sleep: func(time.Duration) {
			t.Fatal("consume loop should not sleep for invalid payload")
		},
	}

	service.consume(ctx, 1)

	committed, closed := reader.snapshot()
	if !closed {
		t.Fatal("expected reader to be closed")
	}
	if len(committed) != 1 {
		t.Fatalf("expected 1 committed message, got %d", len(committed))
	}

	engine := service.engine.(*stubWorkflowEngine)
	if handled := engine.handledEvents(); len(handled) != 0 {
		t.Fatalf("expected no handled events, got %#v", handled)
	}
}

func TestConsumeClosesReaderImmediatelyOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &closeUnblocksWorkflowReader{
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
	service := &Service{
		logger:    testLogger(),
		engine:    &stubWorkflowEngine{},
		newReader: func() WorkflowReader { return reader },
	}
	done := make(chan struct{})
	go func() {
		service.consume(ctx, 1)
		close(done)
	}()

	select {
	case <-reader.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reader fetch")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop after reader close")
	}
}

func TestDecodeWorkflowTaskKeepsRoutingMismatchRetryable(t *testing.T) {
	payload, err := json.Marshal(workflow.WorkflowEvent{ID: "evt-1", ConversationID: "conversation-a"})
	if err != nil {
		t.Fatalf("marshal workflow event: %v", err)
	}
	task, err := decodeWorkflowTask(kafka.Message{Key: []byte("conversation-b"), Value: payload}, &trackedOffset{})
	if err == nil {
		t.Fatal("expected routing mismatch")
	}
	if task == nil || task.event.ConversationID != "conversation-a" {
		t.Fatalf("retryable routing task = %#v", task)
	}
}

func TestConsumeHandlesEventAndCommitsMessage(t *testing.T) {
	event := workflow.WorkflowEvent{
		ID:             "evt_2",
		EventType:      workflow.EventTurnContextReady,
		ConversationID: "conv_2",
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal workflow event: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	reader := &stubWorkflowReader{
		fetches: []fetchResult{
			{message: kafka.Message{Value: payload}},
		},
		commitHook: cancel,
	}
	engine := &stubWorkflowEngine{}
	service := &Service{
		logger:    testLogger(),
		engine:    engine,
		newReader: func() WorkflowReader { return reader },
		sleep: func(time.Duration) {
			t.Fatal("consume loop should not sleep on success")
		},
	}

	service.consume(ctx, 2)

	handled := engine.handledEvents()
	if len(handled) != 1 {
		t.Fatalf("expected 1 handled event, got %#v", handled)
	}
	if handled[0].ID != event.ID || handled[0].EventType != event.EventType {
		t.Fatalf("unexpected handled event: %#v", handled[0])
	}

	committed, closed := reader.snapshot()
	if !closed {
		t.Fatal("expected reader to be closed")
	}
	if len(committed) != 1 {
		t.Fatalf("expected 1 committed message, got %d", len(committed))
	}
}

func TestConsumePrioritizesCancellationForRunningConversation(t *testing.T) {
	makeMessage := func(eventType string) kafka.Message {
		payload, err := json.Marshal(workflow.WorkflowEvent{
			ID:             eventType,
			EventType:      eventType,
			ConversationID: "conversation-1",
			TurnID:         "turn-1",
		})
		if err != nil {
			t.Fatalf("marshal workflow event: %v", err)
		}
		return kafka.Message{Topic: "workflow", Partition: 0, Key: []byte("conversation-1"), Value: payload}
	}

	first := makeMessage(workflow.EventTurnRunRequested)
	first.Offset = 1
	second := makeMessage(workflow.EventTurnCancellationRequested)
	second.Offset = 2
	reader := &stubWorkflowReader{fetches: []fetchResult{{message: first}, {message: second}}}
	started := make(chan struct{})
	cancellationHandled := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	var cancellationOnce sync.Once
	engine := &stubWorkflowEngine{onHandle: func(event workflow.WorkflowEvent) {
		switch event.EventType {
		case workflow.EventTurnRunRequested:
			startOnce.Do(func() { close(started) })
			<-release
		case workflow.EventTurnCancellationRequested:
			cancellationOnce.Do(func() { close(cancellationHandled) })
		}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		logger:    testLogger(),
		engine:    engine,
		newReader: func() WorkflowReader { return reader },
		sleep:     time.Sleep,
	}
	done := make(chan struct{})
	go func() {
		service.consume(ctx, 2)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for running workflow event")
	}
	select {
	case <-cancellationHandled:
	case <-time.After(time.Second):
		t.Fatalf("cancellation did not bypass the running conversation lane: handled=%#v", engine.handledEvents())
	}
	close(release)

	deadline := time.Now().Add(time.Second)
	for {
		committed, _ := reader.snapshot()
		if len(committed) == 1 && committed[0].Offset == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for ordered commit: %#v", committed)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop")
	}
}

func TestConsumeSleepsAndSkipsCommitOnHandlerError(t *testing.T) {
	event := workflow.WorkflowEvent{
		ID:             "evt_3",
		EventType:      workflow.EventTurnAccepted,
		ConversationID: "conv_3",
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal workflow event: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	reader := &stubWorkflowReader{
		fetches: []fetchResult{
			{message: kafka.Message{Value: payload}},
		},
	}

	var slept []time.Duration
	engine := &stubWorkflowEngine{
		handleErr: errors.New("boom"),
	}
	service := &Service{
		logger:    testLogger(),
		engine:    engine,
		newReader: func() WorkflowReader { return reader },
		sleep: func(delay time.Duration) {
			slept = append(slept, delay)
			cancel()
		},
	}

	service.consume(ctx, 3)

	if len(slept) != 1 || slept[0] != time.Second {
		t.Fatalf("expected one retry sleep of 1s, got %#v", slept)
	}

	committed, closed := reader.snapshot()
	if !closed {
		t.Fatal("expected reader to be closed")
	}
	if len(committed) != 0 {
		t.Fatalf("expected no committed messages, got %d", len(committed))
	}

	handled := engine.handledEvents()
	if len(handled) != 1 || handled[0].ID != event.ID {
		t.Fatalf("unexpected handled events: %#v", handled)
	}
}

func TestConsumerSerializesConversationWithoutBlockingOthers(t *testing.T) {
	makeMessage := func(id string, conversationID string, partition int, offset int64) kafka.Message {
		payload, err := json.Marshal(workflow.WorkflowEvent{
			ID: id, EventType: workflow.EventTurnRunRequested, ConversationID: conversationID,
		})
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		return kafka.Message{Topic: "workflow", Partition: partition, Offset: offset, Key: []byte(conversationID), Value: payload}
	}
	reader := &stubWorkflowReader{fetches: []fetchResult{
		{message: makeMessage("a-1", "conversation-a", 0, 1)},
		{message: makeMessage("a-2", "conversation-a", 0, 2)},
		{message: makeMessage("b-1", "conversation-b", 1, 1)},
	}}
	started := make(chan string, 3)
	releaseFirst := make(chan struct{})
	engine := &stubWorkflowEngine{onHandle: func(event workflow.WorkflowEvent) {
		started <- event.ID
		if event.ID == "a-1" {
			<-releaseFirst
		}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		logger: testLogger(), engine: engine,
		newReader: func() WorkflowReader { return reader }, sleep: time.Sleep,
	}
	done := make(chan struct{})
	go func() {
		service.consume(ctx, 2)
		close(done)
	}()

	firstTwo := map[string]bool{}
	for len(firstTwo) < 2 {
		select {
		case id := <-started:
			firstTwo[id] = true
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for concurrent conversations")
		}
	}
	if !firstTwo["a-1"] || !firstTwo["b-1"] || firstTwo["a-2"] {
		t.Fatalf("started before release = %#v, want a-1 and b-1 only", firstTwo)
	}
	close(releaseFirst)
	select {
	case id := <-started:
		if id != "a-2" {
			t.Fatalf("next same-conversation event = %q, want a-2", id)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second conversation-a event")
	}

	deadline := time.Now().Add(time.Second)
	for {
		committed, _ := reader.snapshot()
		if len(committed) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for partition commits: %#v", committed)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out stopping affinity consumer")
	}
}

func TestRunDefaultsToOneConsumerAndClosesWriter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	flushCalled := make(chan struct{}, 1)
	requeueCalled := make(chan struct{}, 1)
	writer := &stubWorkflowWriter{}
	engine := &stubWorkflowEngine{
		onFlush: func() {
			select {
			case flushCalled <- struct{}{}:
			default:
			}
		},
		onRequeue: func() {
			select {
			case requeueCalled <- struct{}{}:
			default:
			}
		},
	}

	var readersMu sync.Mutex
	var readers []*stubWorkflowReader
	service := &Service{
		logger: testLogger(),
		settings: Settings{
			WorkerConcurrency:  0,
			WorkerPollInterval: time.Hour,
			WorkerLeaseTimeout: 2 * time.Hour,
		},
		engine: engine,
		writer: writer,
		newReader: func() WorkflowReader {
			reader := &stubWorkflowReader{}
			readersMu.Lock()
			readers = append(readers, reader)
			readersMu.Unlock()
			return reader
		},
		sleep: func(time.Duration) {},
	}

	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx)
	}()

	waitForSignal := func(name string, ch <-chan struct{}) {
		t.Helper()

		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %s", name)
		}
	}

	waitForSignal("flush loop", flushCalled)
	waitForSignal("requeue loop", requeueCalled)

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run worker service: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker service to stop")
	}

	_, closed := writer.snapshot()
	if !closed {
		t.Fatal("expected writer to be closed")
	}

	readersMu.Lock()
	defer readersMu.Unlock()

	if len(readers) != 1 {
		t.Fatalf("expected exactly one consumer when concurrency <= 0, got %d", len(readers))
	}
	if _, readerClosed := readers[0].snapshot(); !readerClosed {
		t.Fatal("expected consumer reader to be closed")
	}
}
