package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

type stubRequestContextCompactor struct {
	calls []WorkflowEvent
}

func (s *stubRequestContextCompactor) CompactForRequest(_ context.Context, event WorkflowEvent) (bool, error) {
	s.calls = append(s.calls, event)
	return true, nil
}

func TestContextPreflightRebuildsHistoryAndRetainsContinuationSuffix(t *testing.T) {
	store := &stubRunnerContextStore{
		head: &domain.ContextHead{
			ConversationID: "conv-1", Version: 1, RawTailStartSeq: 1, LastSeq: 1,
		},
		messages: []domain.Message{{Seq: 1, Role: domain.RoleUser, ContentText: "retry prompt"}},
	}
	compactor := &stubRequestContextCompactor{}
	preflight := &ContextPreflight{
		settings:  WorkflowSettings{CompactTriggerTokens: 80},
		loader:    &ContextLoader{store: store},
		compactor: compactor,
	}
	state := &ScheduledRunState{
		InitialInputCount: 1,
		Scope:             tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{
			ContextWindowTokens: 1_000,
			Input: []llm.ModelItem{
				{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: strings.Repeat("x", 3_000)},
				{Type: llm.ModelItemFunctionCallOutput, CallID: "call-1", Output: "tool result"},
			},
		},
	}

	prepared, err := preflight.Prepare(t.Context(), state)
	if err != nil {
		t.Fatalf("prepare preflight context: %v", err)
	}
	if len(compactor.calls) != 1 || compactor.calls[0].EventType != EventTurnAccepted {
		t.Fatalf("compaction calls = %#v", compactor.calls)
	}
	if prepared.InitialInputCount != 1 || len(prepared.Request.Input) != 2 {
		t.Fatalf("prepared state = %#v", prepared)
	}
	if prepared.Request.Input[0].Content != "retry prompt" || prepared.Request.Input[1].CallID != "call-1" {
		t.Fatalf("prepared input = %#v", prepared.Request.Input)
	}
}

func TestContextPreflightCompactsForReservedOutputBudget(t *testing.T) {
	store := &stubRunnerContextStore{
		head:     &domain.ContextHead{ConversationID: "conv-1", Version: 1, RawTailStartSeq: 1, LastSeq: 1},
		messages: []domain.Message{{Seq: 1, Role: domain.RoleUser, ContentText: "retry prompt"}},
	}
	compactor := &stubRequestContextCompactor{}
	preflight := &ContextPreflight{
		settings:  WorkflowSettings{CompactTriggerTokens: 80},
		loader:    &ContextLoader{store: store},
		compactor: compactor,
	}
	state := &ScheduledRunState{
		InitialInputCount: 1,
		Scope:             tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{
			ContextWindowTokens: 1_000,
			MaxOutputTokens:     400,
			Input:               []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: strings.Repeat("x", 1_000)}},
		},
	}

	if _, err := preflight.Prepare(t.Context(), state); err != nil {
		t.Fatalf("prepare preflight context: %v", err)
	}
	if len(compactor.calls) != 1 {
		t.Fatalf("compaction calls = %d, want 1 for output reservation", len(compactor.calls))
	}
}

func TestContextPreflightPreservesAccountPersonalization(t *testing.T) {
	store := &stubRunnerContextStore{
		head:     &domain.ContextHead{ConversationID: "conv-1", Version: 1, RawTailStartSeq: 1, LastSeq: 1},
		messages: []domain.Message{{Seq: 1, Role: domain.RoleUser, ContentText: "retry prompt"}},
	}
	preflight := &ContextPreflight{
		settings:  WorkflowSettings{CompactTriggerTokens: 80},
		loader:    &ContextLoader{store: store},
		compactor: &stubRequestContextCompactor{},
	}
	personalization := `{"type":"account_personalization_context","preferences":"Use metric units"}`
	state := &ScheduledRunState{
		InitialInputCount: 2,
		Scope:             tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{
			ContextWindowTokens: 1_000,
			Input: []llm.ModelItem{
				{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: personalization},
				{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: strings.Repeat("x", 3_000)},
			},
		},
	}

	prepared, err := preflight.Prepare(t.Context(), state)
	if err != nil {
		t.Fatalf("prepare preflight context: %v", err)
	}
	if prepared.InitialInputCount != 2 || len(prepared.Request.Input) != 2 {
		t.Fatalf("prepared state = %#v", prepared)
	}
	if prepared.Request.Input[0].Content != personalization || prepared.Request.Input[1].Content != "retry prompt" {
		t.Fatalf("prepared input = %#v", prepared.Request.Input)
	}
}
