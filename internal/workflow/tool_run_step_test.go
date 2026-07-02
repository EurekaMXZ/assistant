package workflow

import (
	"encoding/json"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

func TestScheduledRunExecutesExactlyOneModelRequest(t *testing.T) {
	model := &stubModelClient{
		rawRequests: []json.RawMessage{json.RawMessage(`{"model":"gpt-test"}`)},
		results: []*llm.ModelResult{{
			ResponseID: "resp-1", FinalText: "done",
			OutputItems: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleAssistant, Content: "done"}},
		}},
	}
	orchestrator := NewToolOrchestrator(model, &stubToolCatalog{}, nil, nil, &stubToolArtifactStore{}, &stubToolCallStore{})
	input := ToolRunInput{
		Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Model: "gpt-test", Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "hello"}},
	}
	state, _, err := orchestrator.PrepareScheduledRun(t.Context(), input, 1, 1)
	if err != nil {
		t.Fatalf("prepare scheduled run: %v", err)
	}
	outcome, err := orchestrator.RequestScheduledRun(t.Context(), state, nil)
	if err != nil {
		t.Fatalf("request scheduled run: %v", err)
	}
	if err := orchestrator.PostprocessScheduledRun(t.Context(), &domain.TurnRun{ID: "run-1", TurnID: "turn-1", StepIndex: 1}, state, outcome); err != nil {
		t.Fatalf("postprocess scheduled run: %v", err)
	}
	if len(model.streamRequests) != 1 {
		t.Fatalf("model request count = %d, want 1", len(model.streamRequests))
	}
	if outcome.NextState != nil {
		t.Fatalf("terminal response unexpectedly scheduled another run: %#v", outcome.NextState)
	}
	if len(outcome.ContextItems) != 1 || outcome.ContextItems[0].Content != "done" {
		t.Fatalf("terminal context items = %#v", outcome.ContextItems)
	}
}

func TestScheduledRunPersistsContinuationForNextRequest(t *testing.T) {
	functionTool := llm.ModelTool{Type: llm.ModelToolTypeFunction, Name: "lookup"}
	model := &stubModelClient{
		rawRequests: []json.RawMessage{json.RawMessage(`{"step":1}`), json.RawMessage(`{"step":2}`)},
		results: []*llm.ModelResult{{
			ResponseID: "resp-1",
			OutputItems: []llm.ModelItem{{
				Type: llm.ModelItemFunctionCall, CallID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"q":"x"}`),
			}},
		}},
	}
	executor := &stubToolExecutor{result: &tool.ToolExecutionResult{OutputItem: llm.ModelItem{
		Type: llm.ModelItemFunctionCallOutput, CallID: "call-1", Output: `{"value":1}`,
	}}}
	artifacts := &stubToolArtifactStore{}
	orchestrator := NewToolOrchestrator(model, &stubToolCatalog{tools: []llm.ModelTool{functionTool}}, executor, nil, artifacts, &stubToolCallStore{})
	input := ToolRunInput{
		Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"}, Model: "gpt-test",
		PromptCacheKey: "assistant-conversation-cache",
		Input:          []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "research"}},
	}
	state, _, err := orchestrator.PrepareScheduledRun(t.Context(), input, 1, 1)
	if err != nil {
		t.Fatalf("prepare scheduled run: %v", err)
	}
	outcome, err := orchestrator.RequestScheduledRun(t.Context(), state, nil)
	if err != nil {
		t.Fatalf("request scheduled run: %v", err)
	}
	if err := orchestrator.PostprocessScheduledRun(t.Context(), &domain.TurnRun{ID: "run-1", TurnID: "turn-1", StepIndex: 1}, state, outcome); err != nil {
		t.Fatalf("postprocess scheduled run: %v", err)
	}
	if len(model.streamRequests) != 1 {
		t.Fatalf("model request count = %d, want 1", len(model.streamRequests))
	}
	if outcome.NextState == nil || outcome.NextState.StepIndex != 2 {
		t.Fatalf("next state = %#v, want step 2", outcome.NextState)
	}
	if outcome.NextState.Request.PromptCacheKey != input.PromptCacheKey {
		t.Fatalf("next request prompt cache key = %q, want %q", outcome.NextState.Request.PromptCacheKey, input.PromptCacheKey)
	}
	if len(outcome.NextState.Request.Input) != 3 {
		t.Fatalf("next request input = %#v", outcome.NextState.Request.Input)
	}
	stateKey, _, err := orchestrator.PersistScheduledRunState(t.Context(), outcome.NextState.Scope, outcome.NextState, outcome.NextRequest)
	if err != nil {
		t.Fatalf("persist continuation: %v", err)
	}
	loaded, err := orchestrator.LoadScheduledRunState(t.Context(), stateKey)
	if err != nil {
		t.Fatalf("load continuation: %v", err)
	}
	if loaded.StepIndex != 2 || len(loaded.Request.Input) != 3 || loaded.Request.PromptCacheKey != input.PromptCacheKey {
		t.Fatalf("loaded continuation = %#v", loaded)
	}
}

func TestCompletedToolCallReplaysPersistedOutput(t *testing.T) {
	artifacts := &stubToolArtifactStore{}
	if err := artifacts.PutBytes(t.Context(), "tool-output", []byte(`{"value":1}`), "application/json"); err != nil {
		t.Fatalf("persist tool output: %v", err)
	}
	executor := &stubToolExecutor{}
	orchestrator := NewToolOrchestrator(&stubModelClient{}, nil, executor, nil, artifacts, nil)
	result, err := orchestrator.executeRecordedLocalToolCall(t.Context(), tool.ToolScope{}, &domain.ToolCallRecord{
		Status: domain.ToolCallStatusCompleted, OutputBlobKey: "tool-output",
	}, false, tool.ToolCall{CallID: "call-1", Name: "lookup"})
	if err != nil {
		t.Fatalf("replay completed tool call: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("completed tool call executed again: %#v", executor.calls)
	}
	if result == nil || result.OutputItem.Output != `{"value":1}` {
		t.Fatalf("replayed output = %#v", result)
	}
}
