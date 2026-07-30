package workflow

import (
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

func TestBuildToolExecutionPlanParallelGroups(t *testing.T) {
	parallel := true
	plan, err := buildToolExecutionPlan(&domain.TurnRun{ID: "run-1", TurnID: "turn-1", Attempt: 2}, []tool.ToolCall{
		{CallID: "call-ask", Name: tool.AskUser},
		{CallID: "call-read", Name: "read"},
		{CallID: "call-write", Name: "write"},
	}, ToolExecutionPlanOptions{ParallelToolCalls: &parallel})
	if err != nil {
		t.Fatal(err)
	}

	if !plan.ParallelToolCalls || len(plan.Groups) != 2 {
		t.Fatalf("unexpected parallel plan: %#v", plan)
	}
	if len(plan.Groups[0].Calls) != 2 || plan.Groups[1].Calls[0].Call.CallID != "call-ask" {
		t.Fatalf("unexpected group layout: %#v", plan.Groups)
	}
	if len(plan.Groups[1].DependsOn) != 1 || plan.Groups[1].DependsOn[0] != 0 {
		t.Fatalf("ask_user group dependencies = %#v", plan.Groups[1].DependsOn)
	}
	if plan.Groups[0].Calls[0].StableOperationID != "run-1:call-read" || plan.Groups[0].Calls[1].StableOperationID != "run-1:call-write" {
		t.Fatalf("stable operation IDs = %#v", plan.Groups[0].Calls)
	}
}

func TestBuildToolExecutionPlanSerialGroups(t *testing.T) {
	parallel := false
	plan, err := buildToolExecutionPlan(&domain.TurnRun{ID: "run-1"}, []tool.ToolCall{
		{CallID: "call-1", Name: "first"},
		{CallID: "call-2", Name: "second"},
		{CallID: "call-ask", Name: tool.AskUser},
	}, ToolExecutionPlanOptions{ParallelToolCalls: &parallel})
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Groups) != 3 {
		t.Fatalf("serial group count = %d, want 3: %#v", len(plan.Groups), plan.Groups)
	}
	for index, group := range plan.Groups {
		if len(group.Calls) != 1 || group.Index != index {
			t.Fatalf("group %d = %#v", index, group)
		}
		if index > 0 && (len(group.DependsOn) != 1 || group.DependsOn[0] != index-1) {
			t.Fatalf("group %d dependencies = %#v", index, group.DependsOn)
		}
	}
	if plan.Groups[2].Calls[0].Call.CallID != "call-ask" {
		t.Fatalf("ask_user was not the final group: %#v", plan.Groups)
	}
}

func TestBuildToolExecutionPlanSerializesResourceConflicts(t *testing.T) {
	parallel := true
	plan, err := buildToolExecutionPlan(nil, []tool.ToolCall{
		{CallID: "call-a", Name: "mutate-a"},
		{CallID: "call-b", Name: "mutate-a"},
		{CallID: "call-c", Name: "read-c"},
	}, ToolExecutionPlanOptions{
		ParallelToolCalls: &parallel,
		ResourceKey: func(call tool.ToolCall) string {
			if call.Name == "mutate-a" {
				return "conversation:conv-1"
			}
			return ""
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Groups) != 2 {
		t.Fatalf("resource-constrained groups = %#v", plan.Groups)
	}
	if len(plan.Groups[0].Calls) != 1 || plan.Groups[0].Calls[0].Call.CallID != "call-a" {
		t.Fatalf("first resource group = %#v", plan.Groups[0])
	}
	if len(plan.Groups[1].Calls) != 2 || plan.Groups[1].Calls[0].Call.CallID != "call-b" || plan.Groups[1].Calls[1].Call.CallID != "call-c" {
		t.Fatalf("second resource group = %#v", plan.Groups[1])
	}
}

func TestBuildToolExecutionPlanRejectsMultipleAskUserCalls(t *testing.T) {
	_, err := buildToolExecutionPlan(nil, []tool.ToolCall{
		{CallID: "ask-1", Name: tool.AskUser},
		{CallID: "ask-2", Name: tool.AskUser},
	}, ToolExecutionPlanOptions{})
	if err == nil {
		t.Fatal("multiple ask_user calls were accepted")
	}
}
