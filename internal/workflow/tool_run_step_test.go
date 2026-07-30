package workflow

import (
	"encoding/json"
	"strings"
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
	orchestrator := NewToolOrchestrator(model, &stubToolCatalog{}, nil, &stubToolArtifactStore{}, &stubToolCallStore{})
	state, _, err := orchestrator.PrepareScheduledRun(t.Context(), ToolRunInput{
		Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"}, Model: "gpt-test",
		Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "hello"}},
	}, 1, 1)
	if err != nil {
		t.Fatalf("prepare scheduled run: %v", err)
	}
	outcome, err := orchestrator.RequestScheduledRun(t.Context(), state, nil)
	if err != nil {
		t.Fatalf("request scheduled run: %v", err)
	}
	if err := orchestrator.PostprocessScheduledRunPlan(t.Context(), &domain.TurnRun{ID: "run-1", TurnID: "turn-1", StepIndex: 1}, state, outcome); err != nil {
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

func TestScheduledRunPlanPostprocessingDoesNotExecuteTools(t *testing.T) {
	parallel := true
	artifacts := &stubToolArtifactStore{}
	orchestrator := NewToolOrchestrator(&stubModelClient{}, &stubToolCatalog{}, nil, artifacts, &stubToolCallStore{})
	state := &ScheduledRunState{
		Version: scheduledRunStateVersion, StepIndex: 1, InitialInputCount: 1,
		Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{
			Input:             []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "find"}},
			Tools:             []llm.ModelTool{{Type: llm.ModelToolTypeFunction, Name: "lookup"}},
			ParallelToolCalls: &parallel,
		},
	}
	outcome := &ScheduledRunOutcome{Model: &llm.ModelResult{OutputItems: []llm.ModelItem{
		{Type: llm.ModelItemFunctionCall, CallID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"q":"one"}`)},
		{Type: llm.ModelItemFunctionCall, CallID: "call-2", Name: "lookup", Arguments: json.RawMessage(`{"q":"two"}`)},
	}}}
	if err := orchestrator.PostprocessScheduledRunPlan(t.Context(), &domain.TurnRun{ID: "run-1", TurnID: "turn-1", Attempt: 1}, state, outcome); err != nil {
		t.Fatalf("plan postprocessing: %v", err)
	}
	if outcome.ExecutionPlan == nil || len(outcome.ExecutionPlan.Groups) != 1 || len(outcome.ExecutionPlan.Groups[0].Calls) != 2 {
		t.Fatalf("execution plan = %#v", outcome.ExecutionPlan)
	}
	if err := orchestrator.PersistToolExecutionPlanArtifacts(t.Context(), state.Scope, outcome.ExecutionPlan); err != nil {
		t.Fatalf("persist plan arguments: %v", err)
	}
	for _, planned := range outcome.ExecutionPlan.Groups[0].Calls {
		if planned.ArgumentsBlobKey == "" || string(artifacts.data[planned.ArgumentsBlobKey]) == "" {
			t.Fatalf("planned arguments were not persisted: %#v", planned)
		}
	}
}

func TestPrepareNextScheduledRunFromDurableToolCalls(t *testing.T) {
	artifacts := &stubToolArtifactStore{}
	if err := artifacts.PutBytes(t.Context(), "output-1", []byte(`{"value":1}`), "application/json"); err != nil {
		t.Fatal(err)
	}
	orchestrator := NewToolOrchestrator(&stubModelClient{}, nil, nil, artifacts, nil)
	state := &ScheduledRunState{
		Version: scheduledRunStateVersion, StepIndex: 1, InitialInputCount: 1,
		Scope:   tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{Model: "gpt-test", Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "find"}}},
	}
	plan, err := buildToolExecutionPlan(&domain.TurnRun{ID: "run-1", TurnID: "turn-1", Attempt: 1}, []tool.ToolCall{{
		Type: llm.ModelItemFunctionCall, CallID: "call-1", Name: "lookup",
	}}, ToolExecutionPlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outcome := &ScheduledRunOutcome{
		Model:         &llm.ModelResult{OutputItems: []llm.ModelItem{{Type: llm.ModelItemFunctionCall, CallID: "call-1", Name: "lookup"}}},
		ExecutionPlan: plan,
	}
	if err := orchestrator.PrepareNextScheduledRunFromToolCalls(t.Context(), state, outcome, []domain.ToolCallRecord{{
		TurnID: "turn-1", TurnRunID: "run-1", CallID: "call-1", ToolName: "lookup",
		Status: domain.ToolCallStatusCompleted, OutputBlobKey: "output-1", StableOperationID: plan.Groups[0].Calls[0].StableOperationID,
	}}); err != nil {
		t.Fatalf("prepare next run: %v", err)
	}
	if outcome.NextState == nil || outcome.NextState.StepIndex != 2 {
		t.Fatalf("next state = %#v", outcome.NextState)
	}
	if len(outcome.NextState.Request.Input) != 3 || outcome.NextState.Request.Input[2].Output != `{"value":1}` {
		t.Fatalf("next request input = %#v", outcome.NextState.Request.Input)
	}
}

func TestPrepareNextScheduledRunPreservesUncertainToolOutcome(t *testing.T) {
	artifacts := &stubToolArtifactStore{}
	if err := artifacts.PutBytes(t.Context(), "output-1", []byte(`{"ok":false,"error":{"type":"tool_execution_uncertain"}}`), "application/json"); err != nil {
		t.Fatal(err)
	}
	orchestrator := NewToolOrchestrator(&stubModelClient{}, nil, nil, artifacts, nil)
	state := &ScheduledRunState{Version: scheduledRunStateVersion, StepIndex: 1, InitialInputCount: 1, Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"}, Request: llm.ModelRequest{Model: "gpt-test"}}
	plan, err := buildToolExecutionPlan(&domain.TurnRun{ID: "run-1", TurnID: "turn-1", Attempt: 1}, []tool.ToolCall{{CallID: "call-1", Name: "side-effect"}}, ToolExecutionPlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outcome := &ScheduledRunOutcome{Model: &llm.ModelResult{OutputItems: []llm.ModelItem{{Type: llm.ModelItemFunctionCall, CallID: "call-1", Name: "side-effect"}}}, ExecutionPlan: plan}
	if err := orchestrator.PrepareNextScheduledRunFromToolCalls(t.Context(), state, outcome, []domain.ToolCallRecord{{CallID: "call-1", ToolName: "side-effect", Status: domain.ToolCallStatusUncertain, OutputBlobKey: "output-1", StableOperationID: plan.Groups[0].Calls[0].StableOperationID}}); err != nil {
		t.Fatalf("prepare next run: %v", err)
	}
	if got := outcome.NextState.Request.Input[len(outcome.NextState.Request.Input)-1].Output; got != `{"ok":false,"error":{"type":"tool_execution_uncertain"}}` {
		t.Fatalf("uncertain output = %q", got)
	}
}

func TestScheduledRunRejectsAggregateInputAboveContextLimit(t *testing.T) {
	model := &stubModelClient{}
	orchestrator := NewToolOrchestrator(model, nil, nil, nil, nil)
	state := &ScheduledRunState{Request: llm.ModelRequest{
		Model: "gpt-test", ContextWindowTokens: 100,
		Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: strings.Repeat("x", 1_000)}},
	}}
	if _, err := orchestrator.RequestScheduledRun(t.Context(), state, nil); err == nil {
		t.Fatal("expected oversized aggregate input to be rejected")
	}
	if len(model.streamRequests) != 0 {
		t.Fatalf("oversized request reached model: %#v", model.streamRequests)
	}
}

func TestLoadScheduledRunStatePreservesRawToolOutput(t *testing.T) {
	artifacts := &stubToolArtifactStore{}
	orchestrator := NewToolOrchestrator(&stubModelClient{}, nil, nil, artifacts, nil)
	rawOutput, err := json.Marshal(map[string]any{
		"type": llm.ModelItemFunctionCallOutput, "call_id": "call-1", "output": "value",
	})
	if err != nil {
		t.Fatalf("marshal raw output: %v", err)
	}
	state := &ScheduledRunState{
		Version: scheduledRunStateVersion, StepIndex: 2, InitialInputCount: 1,
		Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{Input: []llm.ModelItem{
			{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "request"},
			{Type: llm.ModelItemFunctionCallOutput, Raw: rawOutput},
		}},
	}
	stateKey, _, err := orchestrator.PersistScheduledRunState(t.Context(), state.Scope, state, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("persist state: %v", err)
	}
	loaded, err := orchestrator.LoadScheduledRunState(t.Context(), stateKey)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if string(loaded.Request.Input[1].Raw) != string(rawOutput) {
		t.Fatalf("raw output was modified: %#v", loaded.Request.Input[1])
	}
}
