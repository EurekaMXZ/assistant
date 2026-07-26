package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

type canonicalPreflightCompactor struct {
	limit       int
	calls       int
	received    *ScheduledRunState
	replacement *ScheduledRunState
}

func (c *canonicalPreflightCompactor) CanonicalInputLimit(context.Context, *ScheduledRunState) (int, error) {
	return c.limit, nil
}

func (c *canonicalPreflightCompactor) Compact(_ context.Context, state *ScheduledRunState) (*ScheduledRunState, bool, error) {
	c.calls++
	c.received = state
	if c.replacement != nil {
		return c.replacement, true, nil
	}
	return state, false, nil
}

func TestContextPreflightReplacesCompleteCanonicalContext(t *testing.T) {
	items := []llm.ModelItem{
		{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: strings.Repeat("x", 3_000)},
		{Type: llm.ModelItemReasoning, Raw: []byte(`{"type":"reasoning","encrypted_content":"cipher"}`)},
		{Type: llm.ModelItemFunctionCall, CallID: "call-1", Name: "lookup"},
		{Type: llm.ModelItemFunctionCallOutput, CallID: "call-1", Output: "result"},
		{Type: llm.ModelItemMCPCall, Raw: []byte(`{"type":"mcp_call","name":"remote"}`)},
	}
	state := &ScheduledRunState{
		InitialInputCount: 1,
		Scope:             tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request:           llm.ModelRequest{ContextWindowTokens: 1_000, Input: cloneModelItems(items)},
	}
	replacement := &ScheduledRunState{
		InitialInputCount: 1,
		Scope:             state.Scope,
		Request: llm.ModelRequest{ContextWindowTokens: 1_000, Input: []llm.ModelItem{{
			Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "model summary",
		}}},
	}
	compactor := &canonicalPreflightCompactor{limit: 10_000, replacement: replacement}
	preflight := &ContextPreflight{settings: WorkflowSettings{CompactTriggerTokens: 80}, compactor: compactor}

	prepared, err := preflight.Prepare(t.Context(), state)
	if err != nil {
		t.Fatalf("prepare canonical context: %v", err)
	}
	if compactor.calls != 1 || compactor.received != state {
		t.Fatalf("compaction calls=%d received=%#v", compactor.calls, compactor.received)
	}
	if prepared != replacement {
		t.Fatalf("prepared state = %#v, want replacement", prepared)
	}
}

func TestContextPreflightReservesWorstCaseContinuationGrowth(t *testing.T) {
	state := &ScheduledRunState{
		Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{
			ContextWindowTokens: 32_000,
			MaxOutputTokens:     12_800,
			Tools:               []llm.ModelTool{{Type: llm.ModelToolTypeFunction, Name: "lookup"}},
		},
	}
	preflight := &ContextPreflight{
		settings:  WorkflowSettings{CompactTriggerTokens: 0},
		compactor: &canonicalPreflightCompactor{limit: 30_000},
	}
	target, err := preflight.compactionTarget(t.Context(), state)
	if err != nil {
		t.Fatalf("calculate compaction target: %v", err)
	}
	if target != 13_104 {
		t.Fatalf("compaction target = %d, want 13104", target)
	}
}

func TestContextPreflightCompactsBeforeStaticEightyPercentThreshold(t *testing.T) {
	state := &ScheduledRunState{
		InitialInputCount: 1,
		Scope:             tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{
			ContextWindowTokens: 32_000,
			MaxOutputTokens:     12_800,
			Tools:               []llm.ModelTool{{Type: llm.ModelToolTypeFunction, Name: "lookup"}},
			Input:               []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: strings.Repeat("x", 40_000)}},
		},
	}
	replacement := &ScheduledRunState{
		InitialInputCount: 1, Scope: state.Scope,
		Request: llm.ModelRequest{ContextWindowTokens: 32_000, MaxOutputTokens: 12_800, Input: []llm.ModelItem{{
			Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "model summary",
		}}},
	}
	compactor := &canonicalPreflightCompactor{limit: 30_000, replacement: replacement}
	preflight := &ContextPreflight{settings: WorkflowSettings{CompactTriggerTokens: 0}, compactor: compactor}

	if _, err := preflight.Prepare(t.Context(), state); err != nil {
		t.Fatalf("prepare canonical context: %v", err)
	}
	if compactor.calls != 1 {
		t.Fatalf("compaction calls = %d, want 1 before 80%% threshold", compactor.calls)
	}
}
