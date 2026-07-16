package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

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
	return fmt.Sprintf("run-request:%s:%s:%d", conversationID, turnID, stepIndex)
}

func (s *stubToolArtifactStore) TurnRunStateKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("run-state:%s:%s:%d", conversationID, turnID, stepIndex)
}

func (s *stubToolArtifactStore) TurnRunResultKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("run-result:%s:%s:%d", conversationID, turnID, stepIndex)
}

func (s *stubToolArtifactStore) TurnRunResponseKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("run-response:%s:%s:%d", conversationID, turnID, stepIndex)
}

func (s *stubToolArtifactStore) TurnRunOutputItemsKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("run-output-items:%s:%s:%d", conversationID, turnID, stepIndex)
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
	ambiguous   []string
	recordsByID map[string]*domain.ToolCallRecord
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

func (s *stubToolCallStore) MarkToolCallAmbiguous(_ context.Context, recordID string, message string) (*domain.ToolCallRecord, error) {
	s.ambiguous = append(s.ambiguous, recordID+":"+message)
	return &domain.ToolCallRecord{ID: recordID, Status: domain.ToolCallStatusAmbiguous}, nil
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

func TestToolRetryDoesNotExecuteAmbiguousSideEffect(t *testing.T) {
	executor := &stubToolExecutor{}
	orchestrator := NewToolOrchestrator(nil, nil, executor, nil, &stubToolArtifactStore{}, nil)
	record := &domain.ToolCallRecord{Status: domain.ToolCallStatusAmbiguous}

	if _, err := orchestrator.executeRecordedLocalToolCall(t.Context(), tool.ToolScope{}, record, false, tool.ToolCall{CallID: "call-1"}); err == nil {
		t.Fatal("expected ambiguous tool call to fail closed")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("ambiguous tool call executed %d times", len(executor.calls))
	}
}

func TestToolExecutionReceivesStableRequestKey(t *testing.T) {
	executor := &stubToolExecutor{result: &tool.ToolExecutionResult{}}
	store := &stubToolCallStore{}
	orchestrator := NewToolOrchestrator(nil, nil, executor, nil, &stubToolArtifactStore{}, store)
	run := &domain.TurnRun{ID: "run-1", TurnID: "turn-1", Attempt: 2}
	call := tool.ToolCall{CallID: "call-1", Name: "side-effect"}

	if _, _, err := orchestrator.executeLocalToolCalls(t.Context(), run, nil, tool.ToolScope{TurnID: "turn-1"}, []tool.ToolCall{call}, -1); err != nil {
		t.Fatalf("execute tool call: %v", err)
	}
	if len(executor.calls) != 1 || executor.calls[0].RequestKey != "run-1:call-1" {
		t.Fatalf("request key not propagated: %#v", executor.calls)
	}
}

func TestToolFailureReturnsRecoverableModelOutputAndReplaysIt(t *testing.T) {
	executor := &stubToolExecutor{err: tool.RecoverableError(fmt.Errorf("temporary lookup failure"))}
	artifacts := &stubToolArtifactStore{}
	store := &stubToolCallStore{}
	orchestrator := NewToolOrchestrator(nil, nil, executor, nil, artifacts, store)
	run := &domain.TurnRun{ID: "run-1", TurnID: "turn-1", Attempt: 1}
	call := tool.ToolCall{CallID: "call-1", Namespace: "sandbox", Name: "create"}

	input, scope, err := orchestrator.executeLocalToolCalls(t.Context(), run, nil, tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"}, []tool.ToolCall{call}, -1)
	if err != nil {
		t.Fatalf("execute failed tool call: %v", err)
	}
	if len(input) != 1 || input[0].Type != llm.ModelItemFunctionCallOutput || !strings.Contains(input[0].Output, `"recoverable":true`) {
		t.Fatalf("model-visible failure output = %#v", input)
	}
	if scope.HasSandbox {
		t.Fatal("failed sandbox.create changed sandbox scope")
	}
	if len(store.failed) != 1 || store.recordsByID["record-call-1"].Status != domain.ToolCallStatusFailed {
		t.Fatalf("failed tool call was not persisted: store=%#v", store)
	}

	executor.calls = nil
	replayed, _, err := orchestrator.executeLocalToolCalls(t.Context(), run, nil, tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"}, []tool.ToolCall{call}, -1)
	if err != nil {
		t.Fatalf("replay failed tool call: %v", err)
	}
	if len(executor.calls) != 0 || len(replayed) != 1 || replayed[0].Output != input[0].Output {
		t.Fatalf("failed tool replay = %#v, executor calls = %#v", replayed, executor.calls)
	}
}

func TestToolFailureWithUncertainOutcomeFailsClosed(t *testing.T) {
	executor := &stubToolExecutor{err: errors.New("connection dropped after request")}
	store := &stubToolCallStore{}
	orchestrator := NewToolOrchestrator(nil, nil, executor, nil, &stubToolArtifactStore{}, store)
	_, _, err := orchestrator.executeLocalToolCalls(t.Context(), &domain.TurnRun{ID: "run-1", TurnID: "turn-1", Attempt: 1}, nil, tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"}, []tool.ToolCall{{CallID: "call-1", Namespace: "sandbox", Name: "create"}}, -1)
	if err == nil || !strings.Contains(err.Error(), "uncertain outcome") {
		t.Fatalf("uncertain tool error = %v", err)
	}
	if len(store.failed) != 0 || len(store.ambiguous) != 1 || store.recordsByID["record-call-1"].Status != domain.ToolCallStatusAmbiguous {
		t.Fatalf("uncertain tool outcome was recorded as definitive: %#v", store)
	}
}
