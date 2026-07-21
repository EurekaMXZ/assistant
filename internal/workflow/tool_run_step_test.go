package workflow

import (
	"encoding/json"
	"errors"
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

func TestScheduledRunRejectsAggregateInputAboveContextLimit(t *testing.T) {
	model := &stubModelClient{}
	orchestrator := NewToolOrchestrator(model, nil, nil, nil, nil, nil)
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

func TestScheduledRunTruncatesModelVisibleToolOutputButPersistsFullArtifact(t *testing.T) {
	fullOutput := `{"value":"` + strings.Repeat("x", 1_000) + `"}`
	functionTool := llm.ModelTool{Type: llm.ModelToolTypeFunction, Name: "lookup"}
	model := &stubModelClient{results: []*llm.ModelResult{{
		OutputItems: []llm.ModelItem{{
			Type: llm.ModelItemFunctionCall, CallID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{}`),
		}},
	}}}
	executor := &stubToolExecutor{result: &tool.ToolExecutionResult{OutputItem: llm.ModelItem{
		Type: llm.ModelItemFunctionCallOutput, CallID: "call-1", Output: fullOutput,
	}}}
	artifacts := &stubToolArtifactStore{}
	orchestrator := NewToolOrchestrator(model, &stubToolCatalog{tools: []llm.ModelTool{functionTool}}, executor, nil, artifacts, &stubToolCallStore{})
	orchestrator.modelToolOutputMaxTokens = 50
	state, _, err := orchestrator.PrepareScheduledRun(t.Context(), ToolRunInput{
		Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"}, Model: "gpt-test",
		Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "research"}},
	}, 1, 1)
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

	modelVisible := outcome.NextState.Request.Input[len(outcome.NextState.Request.Input)-1].Output
	if !strings.Contains(modelVisible, "Warning: truncated output") || modelVisible == fullOutput {
		t.Fatalf("model-visible output was not truncated: %q", modelVisible)
	}
	artifactKey := artifacts.ToolCallOutputKey("conv-1", "turn-1", "call-1")
	if got := string(artifacts.data[artifactKey]); got != fullOutput {
		t.Fatalf("persisted output was modified: got %q", got)
	}
}

func TestLoadScheduledRunStateTruncatesLegacyToolOutput(t *testing.T) {
	artifacts := &stubToolArtifactStore{}
	orchestrator := NewToolOrchestrator(&stubModelClient{}, nil, nil, nil, artifacts, nil)
	orchestrator.modelToolOutputMaxTokens = 50
	legacyRaw, err := json.Marshal(map[string]any{
		"type": llm.ModelItemFunctionCallOutput, "call_id": "call-1", "output": strings.Repeat("x", 1_000),
	})
	if err != nil {
		t.Fatalf("marshal legacy output: %v", err)
	}
	state := &ScheduledRunState{
		Version: scheduledRunStateVersion, StepIndex: 2, InitialInputCount: 1,
		Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{Input: []llm.ModelItem{
			{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "request"},
			{Type: llm.ModelItemFunctionCallOutput, Raw: legacyRaw},
		}},
	}
	stateKey, _, err := orchestrator.PersistScheduledRunState(t.Context(), state.Scope, state, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("persist legacy state: %v", err)
	}
	loaded, err := orchestrator.LoadScheduledRunState(t.Context(), stateKey)
	if err != nil {
		t.Fatalf("load legacy state: %v", err)
	}
	if !strings.Contains(loaded.Request.Input[1].Output, "Warning: truncated output") {
		t.Fatalf("legacy output was not truncated: %#v", loaded.Request.Input[1])
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

func TestScheduledRunContinuesAfterToolFailure(t *testing.T) {
	functionTool := llm.ModelTool{Type: llm.ModelToolTypeFunction, Name: "lookup"}
	model := &stubModelClient{rawRequests: []json.RawMessage{json.RawMessage(`{"step":1}`), json.RawMessage(`{"step":2}`)}, results: []*llm.ModelResult{{
		OutputItems: []llm.ModelItem{{Type: llm.ModelItemFunctionCall, CallID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"q":"x"}`)}},
	}}}
	orchestrator := NewToolOrchestrator(model, &stubToolCatalog{tools: []llm.ModelTool{functionTool}}, &stubToolExecutor{err: tool.RecoverableError(errors.New("search unavailable"))}, nil, &stubToolArtifactStore{}, &stubToolCallStore{})
	state, _, err := orchestrator.PrepareScheduledRun(t.Context(), ToolRunInput{
		Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"}, Model: "gpt-test",
		Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "research"}},
	}, 1, 1)
	if err != nil {
		t.Fatalf("prepare scheduled run: %v", err)
	}
	outcome, err := orchestrator.RequestScheduledRun(t.Context(), state, nil)
	if err != nil {
		t.Fatalf("request scheduled run: %v", err)
	}
	if err := orchestrator.PostprocessScheduledRun(t.Context(), &domain.TurnRun{ID: "run-1", TurnID: "turn-1", StepIndex: 1}, state, outcome); err != nil {
		t.Fatalf("tool failure ended scheduled run: %v", err)
	}
	if outcome.NextState == nil || len(outcome.NextState.Request.Input) != 3 {
		t.Fatalf("tool failure did not schedule model continuation: %#v", outcome.NextState)
	}
	if output := outcome.NextState.Request.Input[2].Output; !strings.Contains(output, `"recoverable":true`) {
		t.Fatalf("next model input does not contain recoverable failure: %s", output)
	}
}

func TestScheduledRunPausesForAskUserWithoutPostprocessing(t *testing.T) {
	model := &stubModelClient{results: []*llm.ModelResult{{
		ResponseID: "resp-ask",
		OutputItems: []llm.ModelItem{{
			Type: llm.ModelItemFunctionCall, CallID: "call-ask", Name: tool.AskUser,
			Arguments: json.RawMessage(`{"prompt":"Continue?","kind":"single_choice","options":[{"id":"yes","label":"Yes","tone":"primary"},{"id":"cancel","label":"Cancel","tone":"neutral"}]}`),
		}},
	}}}
	calls := &stubToolCallStore{}
	orchestrator := NewToolOrchestrator(
		model,
		&stubToolCatalog{tools: []llm.ModelTool{{Type: llm.ModelToolTypeFunction, Name: tool.AskUser}}},
		&stubToolExecutor{result: &tool.ToolExecutionResult{AwaitingInput: &tool.AskUserPrompt{
			Prompt: "Continue?", Kind: tool.AskUserKindSingleChoice,
			Options: []tool.AskUserOption{{ID: "yes", Label: "Yes", Tone: tool.AskUserTonePrimary}, {ID: "cancel", Label: "Cancel", Tone: tool.AskUserToneNeutral}},
		}}},
		nil, &stubToolArtifactStore{}, calls,
	)
	state, _, err := orchestrator.PrepareScheduledRun(t.Context(), ToolRunInput{
		Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"}, Model: "gpt-test",
		Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "start"}},
	}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := orchestrator.RequestScheduledRun(t.Context(), state, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = orchestrator.PostprocessScheduledRun(t.Context(), &domain.TurnRun{ID: "run-1", TurnID: "turn-1", StepIndex: 1, Attempt: 1}, state, outcome)
	var waiting *AwaitingInputSignal
	if !errors.As(err, &waiting) || waiting.ToolCall == nil || waiting.Prompt == nil {
		t.Fatalf("postprocess error = %#v, want awaiting input signal", err)
	}
	if waiting.ToolCall.Status != domain.ToolCallStatusRunning || waiting.Prompt.ToolCallID != waiting.ToolCall.ID {
		t.Fatalf("waiting signal = %#v", waiting)
	}
	if len(calls.awaiting) != 0 {
		t.Fatalf("postprocess changed waiting state before checkpoint: %#v", calls.awaiting)
	}
	if outcome.Postprocessed || outcome.NextState != nil || len(outcome.ToolResults) != 0 || len(calls.completed) != 0 {
		t.Fatalf("awaiting outcome was finalized: outcome=%#v completed=%#v", outcome, calls.completed)
	}
}

func TestScheduledRunExecutesOtherCallsBeforePausingForAskUser(t *testing.T) {
	arguments := json.RawMessage(`{"prompt":"Continue?","kind":"single_choice","options":[{"id":"yes","label":"Yes","tone":"primary"},{"id":"cancel","label":"Cancel","tone":"neutral"}]}`)
	artifacts := &stubToolArtifactStore{}
	calls := &stubToolCallStore{}
	executor := &stubToolExecutor{results: []*tool.ToolExecutionResult{
		{OutputItem: llm.ModelItem{Type: llm.ModelItemFunctionCallOutput, CallID: "call-rename", Output: `{"renamed":true}`}},
		{AwaitingInput: &tool.AskUserPrompt{
			Prompt: "Continue?", Kind: tool.AskUserKindSingleChoice,
			Options: []tool.AskUserOption{{ID: "yes", Label: "Yes", Tone: tool.AskUserTonePrimary}, {ID: "cancel", Label: "Cancel", Tone: tool.AskUserToneNeutral}},
		}},
	}}
	orchestrator := NewToolOrchestrator(&stubModelClient{rawRequests: []json.RawMessage{json.RawMessage(`{"next":true}`)}}, nil, executor, nil, artifacts, calls)
	state := &ScheduledRunState{
		Version: scheduledRunStateVersion, StepIndex: 1, InitialInputCount: 1,
		Scope:   tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{Model: "gpt-test", Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "start"}}},
	}
	outcome := &ScheduledRunOutcome{Model: &llm.ModelResult{OutputItems: []llm.ModelItem{
		{Type: llm.ModelItemFunctionCall, CallID: "call-ask", Name: tool.AskUser, Arguments: arguments},
		{Type: llm.ModelItemFunctionCall, CallID: "call-rename", Name: "conversation_rename_title", Arguments: json.RawMessage(`{"title":"Updated"}`)},
	}}}
	run := &domain.TurnRun{ID: "run-1", TurnID: "turn-1", StepIndex: 1, Attempt: 1}

	err := orchestrator.PostprocessScheduledRun(t.Context(), run, state, outcome)
	var waiting *AwaitingInputSignal
	if !errors.As(err, &waiting) || waiting.ToolCall == nil || waiting.ToolCall.CallID != "call-ask" {
		t.Fatalf("postprocess error = %#v, want ask_user waiting signal", err)
	}
	if len(executor.calls) != 2 || executor.calls[0].CallID != "call-rename" || executor.calls[1].CallID != "call-ask" {
		t.Fatalf("tool execution order = %#v", executor.calls)
	}
	if len(outcome.ToolResults) != 1 || outcome.ToolResults[0].CallID != "call-rename" || outcome.ToolResults[0].Output != `{"renamed":true}` {
		t.Fatalf("checkpointed tool results = %#v", outcome.ToolResults)
	}

	answerKey := artifacts.ToolCallOutputKey("conv-1", "turn-1", "call-ask")
	if err := artifacts.PutBytes(t.Context(), answerKey, []byte(`{"status":"answered","option_id":"yes","label":"Yes","user_reported":true}`), "application/json"); err != nil {
		t.Fatal(err)
	}
	askRecord := calls.recordsByID["record-call-ask"]
	askRecord.Status = domain.ToolCallStatusCompleted
	askRecord.OutputBlobKey = answerKey

	if err := orchestrator.PostprocessScheduledRun(t.Context(), run, state, outcome); err != nil {
		t.Fatalf("resume mixed tool calls: %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("completed tools executed again: %#v", executor.calls)
	}
	if !outcome.Postprocessed || outcome.NextState == nil || len(outcome.ToolResults) != 2 {
		t.Fatalf("resumed outcome = %#v", outcome)
	}
}

func TestScheduledRunRejectsMultipleAskUserCalls(t *testing.T) {
	orchestrator := NewToolOrchestrator(&stubModelClient{}, nil, &stubToolExecutor{}, nil, &stubToolArtifactStore{}, &stubToolCallStore{})
	state := &ScheduledRunState{Version: scheduledRunStateVersion, StepIndex: 1, Scope: tool.ToolScope{}}
	outcome := &ScheduledRunOutcome{Model: &llm.ModelResult{OutputItems: []llm.ModelItem{
		{Type: llm.ModelItemFunctionCall, CallID: "ask-1", Name: tool.AskUser},
		{Type: llm.ModelItemFunctionCall, CallID: "ask-2", Name: tool.AskUser},
	}}}
	if err := orchestrator.PostprocessScheduledRun(t.Context(), &domain.TurnRun{ID: "run-1"}, state, outcome); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("error = %v, want validation error", err)
	}
}

func TestAnsweredAskUserReplaysOutputAndBuildsNextState(t *testing.T) {
	artifacts := &stubToolArtifactStore{}
	outputKey := artifacts.ToolCallOutputKey("conv-1", "turn-1", "call-ask")
	if err := artifacts.PutBytes(t.Context(), outputKey, []byte(`{"status":"answered","option_id":"yes","label":"Yes","user_reported":true}`), "application/json"); err != nil {
		t.Fatal(err)
	}
	calls := &stubToolCallStore{recordsByID: map[string]*domain.ToolCallRecord{
		"record-call-ask": {
			ID: "record-call-ask", TurnID: "turn-1", TurnRunID: "run-1", CallID: "call-ask",
			ToolName: tool.AskUser, Status: domain.ToolCallStatusCompleted, OutputBlobKey: outputKey,
		},
	}}
	model := &stubModelClient{rawRequests: []json.RawMessage{json.RawMessage(`{"next":true}`)}}
	orchestrator := NewToolOrchestrator(model, nil, &stubToolExecutor{}, nil, artifacts, calls)
	state := &ScheduledRunState{
		Version: scheduledRunStateVersion, StepIndex: 1, InitialInputCount: 1,
		Scope:   tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{Model: "gpt-test", Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "start"}}},
	}
	outcome := &ScheduledRunOutcome{Model: &llm.ModelResult{OutputItems: []llm.ModelItem{{
		Type: llm.ModelItemFunctionCall, CallID: "call-ask", Name: tool.AskUser,
		Arguments: json.RawMessage(`{"prompt":"Continue?","kind":"single_choice","options":[{"id":"yes","label":"Yes","tone":"primary"},{"id":"cancel","label":"Cancel","tone":"neutral"}]}`),
	}}}}
	if err := orchestrator.PostprocessScheduledRun(t.Context(), &domain.TurnRun{ID: "run-1", TurnID: "turn-1", StepIndex: 1, Attempt: 1}, state, outcome); err != nil {
		t.Fatalf("replay answered ask_user: %v", err)
	}
	if !outcome.Postprocessed || outcome.NextState == nil || len(outcome.ToolResults) != 1 {
		t.Fatalf("replayed outcome = %#v", outcome)
	}
	if got := outcome.ToolResults[0].Output; !strings.Contains(got, `"option_id":"yes"`) {
		t.Fatalf("replayed output = %q", got)
	}
}
