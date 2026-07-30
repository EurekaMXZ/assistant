package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

type stubModelClient struct {
	marshalRequests []llm.ModelRequest
	streamRequests  []llm.ModelRequest
	rawRequests     []json.RawMessage
	results         []*llm.ModelResult
	errs            []error
	marshalIndex    int
	streamIndex     int
}

func (s *stubModelClient) MarshalRequest(request llm.ModelRequest) (json.RawMessage, error) {
	s.marshalRequests = append(s.marshalRequests, request)
	if s.marshalIndex >= len(s.rawRequests) {
		return nil, nil
	}
	raw := append(json.RawMessage(nil), s.rawRequests[s.marshalIndex]...)
	s.marshalIndex++
	return raw, nil
}

func (s *stubModelClient) StreamResponse(_ context.Context, request llm.ModelRequest, _ llm.ModelEventHandler) (*llm.ModelResult, error) {
	s.streamRequests = append(s.streamRequests, request)
	var result *llm.ModelResult
	if s.streamIndex < len(s.results) {
		result = s.results[s.streamIndex]
	}
	var err error
	if s.streamIndex < len(s.errs) {
		err = s.errs[s.streamIndex]
	}
	s.streamIndex++
	return result, err
}

type stubToolCatalog struct {
	scope  tool.ToolScope
	scopes []tool.ToolScope
	tools  []llm.ModelTool
	err    error
}

func (s *stubToolCatalog) ListTools(_ context.Context, scope tool.ToolScope) ([]llm.ModelTool, error) {
	s.scope = scope
	s.scopes = append(s.scopes, scope)
	if s.err != nil {
		return nil, s.err
	}
	return append([]llm.ModelTool(nil), s.tools...), nil
}

type stubToolArtifactStore struct {
	putKeys []string
	data    map[string][]byte
}

func (s *stubToolArtifactStore) PutBytes(_ context.Context, key string, data []byte, _ string) error {
	s.putKeys = append(s.putKeys, key)
	if s.data == nil {
		s.data = map[string][]byte{}
	}
	s.data[key] = append([]byte(nil), data...)
	return nil
}

func (s *stubToolArtifactStore) PutImmutableBytes(_ context.Context, key string, data []byte, _ string) error {
	if s.data == nil {
		s.data = map[string][]byte{}
	}
	if existing, ok := s.data[key]; ok && string(existing) != string(data) {
		return domain.ErrConflict
	}
	s.data[key] = append([]byte(nil), data...)
	return nil
}

func (s *stubToolArtifactStore) GetBytes(_ context.Context, key string) ([]byte, error) {
	if s.data == nil {
		return nil, domain.ErrNotFound
	}
	data, ok := s.data[key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return append([]byte(nil), data...), nil
}

func (s *stubToolArtifactStore) TurnRunRequestKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("run-request:%s:%s:%d.json.zst", conversationID, turnID, stepIndex)
}

func (s *stubToolArtifactStore) TurnRunStateKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("run-state:%s:%s:%d.json.zst", conversationID, turnID, stepIndex)
}

func (s *stubToolArtifactStore) TurnRunResultKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("run-result:%s:%s:%d.json.zst", conversationID, turnID, stepIndex)
}

func (s *stubToolArtifactStore) ToolCallArgumentsKey(conversationID, turnID, callID string) string {
	return "tool-args:" + conversationID + ":" + turnID + ":" + callID
}

func (s *stubToolArtifactStore) ToolCallOutputKey(conversationID, turnID, callID string) string {
	return "tool-output:" + conversationID + ":" + turnID + ":" + callID
}

type stubToolCallStore struct {
	created     []string
	completed   []string
	failed      []string
	awaiting    []string
	recordsByID map[string]*domain.ToolCallRecord
	finalizeErr error
}

func (s *stubToolCallStore) MarkAwaitingInput(_ context.Context, recordID string) (*domain.ToolCallRecord, error) {
	s.awaiting = append(s.awaiting, recordID)
	record := s.recordsByID[recordID]
	if record == nil {
		return nil, domain.ErrNotFound
	}
	record.Status = domain.ToolCallStatusAwaitingInput
	return record, nil
}

func (s *stubToolCallStore) AcquireToolCall(_ context.Context, turnID string, turnRunID string, executionAttempt int, call tool.ToolCall, argumentsBlobKey string) (*domain.ToolCallRecord, bool, error) {
	s.created = append(s.created, turnID+":"+turnRunID+":"+call.Type+":"+call.Namespace+":"+call.Name+":"+call.CallID+":"+argumentsBlobKey)
	record := &domain.ToolCallRecord{ID: "record-" + call.CallID, TurnID: turnID, TurnRunID: turnRunID, CallID: call.CallID, Status: domain.ToolCallStatusRunning, ExecutionAttempt: executionAttempt}
	if s.recordsByID == nil {
		s.recordsByID = map[string]*domain.ToolCallRecord{}
	}
	if existing := s.recordsByID[record.ID]; existing != nil {
		return existing, false, nil
	}
	s.recordsByID[record.ID] = record
	return record, true, nil
}

func (s *stubToolCallStore) CompleteToolCall(_ context.Context, recordID string, outputBlobKey string) (*domain.ToolCallRecord, error) {
	s.completed = append(s.completed, recordID+":"+outputBlobKey)
	return &domain.ToolCallRecord{ID: recordID}, nil
}

func (s *stubToolCallStore) FailToolCall(_ context.Context, recordID string, outputBlobKey string, message string) (*domain.ToolCallRecord, error) {
	s.failed = append(s.failed, recordID+":"+outputBlobKey+":"+message)
	return &domain.ToolCallRecord{ID: recordID}, nil
}

func (s *stubToolCallStore) GetToolCallForAnswer(_ context.Context, _ string, _ string, toolCallID string) (*domain.ToolCallRecord, error) {
	if record := s.recordsByID[toolCallID]; record != nil {
		return record, nil
	}
	return nil, domain.ErrNotFound
}

func (s *stubToolCallStore) ClaimAwaitingInputAnswer(_ context.Context, _ string, _ string, toolCallID string, answerKey string, answerFingerprint string, answerOptionID string, outputBlobKey string) (*AskUserAnswerClaim, error) {
	record := s.recordsByID[toolCallID]
	if record == nil {
		return nil, domain.ErrNotFound
	}
	if record.Status == domain.ToolCallStatusCompleted {
		if record.AnswerKey != answerKey || record.AnswerFingerprint != answerFingerprint || record.AnswerOptionID != answerOptionID || record.OutputBlobKey != outputBlobKey {
			return nil, domain.ErrConflict
		}
		return &AskUserAnswerClaim{ToolCall: record, ConversationID: "conv-1", Finalized: true}, nil
	}
	if record.AnswerKey != "" && (record.AnswerKey != answerKey || record.AnswerFingerprint != answerFingerprint || record.AnswerOptionID != answerOptionID || record.OutputBlobKey != outputBlobKey) {
		return nil, domain.ErrConflict
	}
	record.AnswerKey = answerKey
	record.AnswerFingerprint = answerFingerprint
	record.AnswerOptionID = answerOptionID
	record.AnswerOutputPending = true
	record.OutputBlobKey = outputBlobKey
	return &AskUserAnswerClaim{ToolCall: record, ConversationID: "conv-1"}, nil
}

func (s *stubToolCallStore) FinalizeAwaitingInputAnswer(_ context.Context, _ string, _ string, toolCallID string, answerKey string, answerFingerprint string, answerOptionID string, outputBlobKey string, _ json.RawMessage) (*domain.ToolCallRecord, bool, error) {
	if s.finalizeErr != nil {
		return nil, false, s.finalizeErr
	}
	record := s.recordsByID[toolCallID]
	if record == nil {
		return nil, false, domain.ErrNotFound
	}
	if record.AnswerKey != answerKey || record.AnswerFingerprint != answerFingerprint || record.AnswerOptionID != answerOptionID || record.OutputBlobKey != outputBlobKey {
		return nil, false, domain.ErrConflict
	}
	if record.Status == domain.ToolCallStatusCompleted {
		return record, true, nil
	}
	record.Status = domain.ToolCallStatusCompleted
	record.AnswerOutputPending = false
	return record, false, nil
}

type stubToolExecutor struct {
	calls   []tool.ToolCall
	scope   tool.ToolScope
	result  *tool.ToolExecutionResult
	err     error
	results []*tool.ToolExecutionResult
}

func (s *stubToolExecutor) Execute(_ context.Context, scope tool.ToolScope, call tool.ToolCall) (*tool.ToolExecutionResult, error) {
	s.scope = scope
	s.calls = append(s.calls, call)
	if s.err != nil {
		return nil, s.err
	}
	if len(s.results) > 0 {
		result := s.results[0]
		s.results = s.results[1:]
		return result, nil
	}
	return s.result, nil
}

type recordingPublisher struct {
	events []stream.Event
}

func (p *recordingPublisher) Publish(_ context.Context, event stream.Event) error {
	p.events = append(p.events, event)
	return nil
}

func TestNormalizeFunctionCallItemsLeavesAmbiguousBareNameUnchanged(t *testing.T) {
	items := []llm.ModelItem{{Type: llm.ModelItemFunctionCall, Name: "replace"}}
	tools := []llm.ModelTool{
		{Type: llm.ModelToolTypeNamespace, Name: "inventory", Tools: []llm.ModelTool{{Type: llm.ModelToolTypeFunction, Name: "replace"}}},
		{Type: llm.ModelToolTypeNamespace, Name: "document", Tools: []llm.ModelTool{{Type: llm.ModelToolTypeFunction, Name: "replace"}}},
	}

	normalized := normalizeFunctionCallItems(items, tools)
	if len(normalized) != 1 || normalized[0].Namespace != "" || normalized[0].Name != "replace" {
		t.Fatalf("expected ambiguous bare name to remain unchanged, got %#v", normalized)
	}
}

type stubToolExecutionStore struct {
	scheduled *ToolExecutionScheduleInput
	completed *ToolCallSettlement
	failed    *ToolCallSettlement
	uncertain *ToolCallSettlement
	renewErr  error
}

type blockingToolExecutor struct {
	started chan struct{}
}

func (e *blockingToolExecutor) Execute(ctx context.Context, _ tool.ToolScope, _ tool.ToolCall) (*tool.ToolExecutionResult, error) {
	close(e.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *stubToolExecutionStore) ScheduleToolExecutionPlan(_ context.Context, input ToolExecutionScheduleInput) ([]domain.ToolCallRecord, error) {
	s.scheduled = &input
	return nil, nil
}

func (s *stubToolExecutionStore) ClaimQueuedToolCall(context.Context, string, time.Duration) (*domain.ToolCallRecord, ToolCallLease, error) {
	return nil, ToolCallLease{}, domain.ErrConflict
}

func (s *stubToolExecutionStore) RenewQueuedToolCallLease(context.Context, ToolCallLease) error {
	return s.renewErr
}

func (s *stubToolExecutionStore) CompleteQueuedToolCall(_ context.Context, lease ToolCallLease, outputBlobKey string) (*ToolCallSettlement, error) {
	s.completed = &ToolCallSettlement{Record: &domain.ToolCallRecord{ID: lease.ToolCallID, Status: domain.ToolCallStatusCompleted, OutputBlobKey: outputBlobKey}}
	return s.completed, nil
}

func (s *stubToolExecutionStore) FailQueuedToolCall(_ context.Context, lease ToolCallLease, outputBlobKey string, message string) (*ToolCallSettlement, error) {
	s.failed = &ToolCallSettlement{Record: &domain.ToolCallRecord{ID: lease.ToolCallID, Status: domain.ToolCallStatusFailed, OutputBlobKey: outputBlobKey, ErrorMessage: message}}
	return s.failed, nil
}

func (s *stubToolExecutionStore) MarkQueuedToolCallUncertain(_ context.Context, lease ToolCallLease, outputBlobKey string, message string) (*ToolCallSettlement, error) {
	s.uncertain = &ToolCallSettlement{Record: &domain.ToolCallRecord{ID: lease.ToolCallID, Status: domain.ToolCallStatusUncertain, OutputBlobKey: outputBlobKey, ErrorMessage: message}}
	return s.uncertain, nil
}

func (s *stubToolExecutionStore) AdvanceToolExecutionGroup(context.Context, string, int) (*ToolGroupAdvance, error) {
	return nil, nil
}

func (s *stubToolExecutionStore) ListToolCallsByRun(context.Context, string) ([]domain.ToolCallRecord, error) {
	return nil, nil
}

func TestQueuedToolExecutionReceivesStableRequestKey(t *testing.T) {
	executor := &stubToolExecutor{result: &tool.ToolExecutionResult{}}
	artifacts := &stubToolArtifactStore{data: map[string][]byte{"arguments": []byte(`{}`)}}
	store := &stubToolExecutionStore{}
	runner := NewToolExecutionRunner(executor, artifacts, store, nil)
	record := &domain.ToolCallRecord{
		ID: "tool-call-1", TurnRunID: "run-1", CallID: "call-1", ToolName: "side-effect",
		ArgumentsBlobKey: "arguments", StableOperationID: "run-1:call-1",
	}
	if _, _, err := runner.ExecuteQueuedToolCall(t.Context(), tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"}, record, ToolCallLease{ToolCallID: record.ID, TurnRunID: record.TurnRunID, Token: "lease-1"}, time.Minute); err != nil {
		t.Fatalf("execute queued tool call: %v", err)
	}
	if len(executor.calls) != 1 || executor.calls[0].RequestKey != record.StableOperationID {
		t.Fatalf("request key not propagated: %#v", executor.calls)
	}
	if store.completed == nil {
		t.Fatal("completed settlement was not recorded")
	}
}

func TestQueuedToolExecutionMarksUncertainOutcome(t *testing.T) {
	executor := &stubToolExecutor{err: fmt.Errorf("%w: connection dropped after request", tool.ErrOutcomeUncertain)}
	artifacts := &stubToolArtifactStore{data: map[string][]byte{"arguments": []byte(`{}`)}}
	store := &stubToolExecutionStore{}
	runner := NewToolExecutionRunner(executor, artifacts, store, nil)
	record := &domain.ToolCallRecord{ID: "tool-call-1", TurnRunID: "run-1", CallID: "call-1", ToolName: "side-effect", ArgumentsBlobKey: "arguments", StableOperationID: "run-1:call-1"}
	if _, _, err := runner.ExecuteQueuedToolCall(t.Context(), tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"}, record, ToolCallLease{ToolCallID: record.ID, TurnRunID: record.TurnRunID, Token: "lease-1"}, time.Minute); err != nil {
		t.Fatalf("execute uncertain tool call: %v", err)
	}
	if store.uncertain == nil || store.failed != nil || store.uncertain.Record.Status != domain.ToolCallStatusUncertain {
		t.Fatalf("uncertain settlement = %#v failed=%#v", store.uncertain, store.failed)
	}
	output := string(artifacts.data[store.uncertain.Record.OutputBlobKey])
	if !strings.Contains(output, `"type":"tool_execution_uncertain"`) {
		t.Fatalf("uncertain model output = %s", output)
	}
}

func TestQueuedToolExecutionCancelsWhenLeaseIsFenced(t *testing.T) {
	executor := &blockingToolExecutor{started: make(chan struct{})}
	artifacts := &stubToolArtifactStore{data: map[string][]byte{"arguments": []byte(`{}`)}}
	store := &stubToolExecutionStore{renewErr: domain.ErrConflict}
	runner := NewToolExecutionRunner(executor, artifacts, store, nil)
	record := &domain.ToolCallRecord{ID: "tool-call-1", TurnRunID: "run-1", CallID: "call-1", ToolName: "side-effect", ArgumentsBlobKey: "arguments", StableOperationID: "run-1:call-1"}
	_, _, err := runner.ExecuteQueuedToolCall(t.Context(), tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"}, record, ToolCallLease{ToolCallID: record.ID, TurnRunID: record.TurnRunID, Token: "lease-1"}, 30*time.Millisecond)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("fenced execution error = %v, want lease conflict", err)
	}
	if store.completed != nil || store.failed != nil || store.uncertain != nil {
		t.Fatalf("fenced execution settled tool call: %#v", store)
	}
}
