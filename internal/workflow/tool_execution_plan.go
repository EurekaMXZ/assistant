package workflow

import (
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

const toolExecutionPlanVersion = 1

type ToolExecutionPlan struct {
	Version           int                  `json:"version"`
	RunID             string               `json:"run_id,omitempty"`
	TurnID            string               `json:"turn_id,omitempty"`
	Attempt           int                  `json:"attempt,omitempty"`
	ParallelToolCalls bool                 `json:"parallel_tool_calls"`
	Groups            []ToolExecutionGroup `json:"groups"`
}

type ToolExecutionGroup struct {
	Index     int                     `json:"index"`
	DependsOn []int                   `json:"depends_on,omitempty"`
	Calls     []ToolExecutionPlanCall `json:"calls"`
}

type ToolExecutionPlanCall struct {
	Ordinal           int           `json:"ordinal"`
	Call              tool.ToolCall `json:"call"`
	ToolCallRecordID  string        `json:"tool_call_record_id,omitempty"`
	ArgumentsBlobKey  string        `json:"arguments_blob_key,omitempty"`
	ResourceKey       string        `json:"resource_key,omitempty"`
	StableOperationID string        `json:"stable_operation_id"`
}

type ToolExecutionPlanOptions struct {
	ParallelToolCalls *bool
	ResourceKey       func(tool.ToolCall) string
}

func buildToolExecutionPlan(run *domain.TurnRun, calls []tool.ToolCall, options ToolExecutionPlanOptions) (*ToolExecutionPlan, error) {
	plan := &ToolExecutionPlan{Version: toolExecutionPlanVersion}
	if run != nil {
		plan.RunID = run.ID
		plan.TurnID = run.TurnID
		plan.Attempt = run.Attempt
	}
	if options.ParallelToolCalls != nil {
		plan.ParallelToolCalls = *options.ParallelToolCalls
	}

	ordinary := make([]ToolExecutionPlanCall, 0, len(calls))
	var awaiting *ToolExecutionPlanCall
	for ordinal, call := range calls {
		planned := ToolExecutionPlanCall{
			Ordinal:           ordinal,
			Call:              call,
			ResourceKey:       toolExecutionResourceKey(options.ResourceKey, call),
			StableOperationID: stableToolOperationID(run, call, ordinal),
		}
		if normalizedToolName(call) == tool.AskUser {
			if awaiting != nil {
				return nil, domain.NewValidationError("ask_user may only be called once per response")
			}
			copy := planned
			awaiting = &copy
			continue
		}
		ordinary = append(ordinary, planned)
	}

	if plan.ParallelToolCalls {
		for _, planned := range ordinary {
			appendParallelPlanCall(&plan.Groups, planned)
		}
	} else {
		for _, planned := range ordinary {
			appendPlanGroup(&plan.Groups, planned)
		}
	}
	if awaiting != nil {
		appendPlanGroup(&plan.Groups, *awaiting)
	}

	return plan, nil
}

func appendParallelPlanCall(groups *[]ToolExecutionGroup, planned ToolExecutionPlanCall) {
	if groups == nil {
		return
	}
	if len(*groups) == 0 {
		appendPlanGroup(groups, planned)
		return
	}

	current := &(*groups)[len(*groups)-1]
	resourceKey := strings.TrimSpace(planned.ResourceKey)
	if resourceKey != "" {
		for _, existing := range current.Calls {
			if strings.TrimSpace(existing.ResourceKey) == resourceKey {
				appendPlanGroup(groups, planned)
				return
			}
		}
	}
	current.Calls = append(current.Calls, planned)
}

func appendPlanGroup(groups *[]ToolExecutionGroup, planned ToolExecutionPlanCall) {
	if groups == nil {
		return
	}
	index := len(*groups)
	group := ToolExecutionGroup{
		Index: index,
		Calls: []ToolExecutionPlanCall{planned},
	}
	if index > 0 {
		group.DependsOn = []int{index - 1}
	}
	*groups = append(*groups, group)
}

func toolExecutionResourceKey(resolver func(tool.ToolCall) string, call tool.ToolCall) string {
	if resolver == nil {
		return ""
	}
	return strings.TrimSpace(resolver(call))
}

func stableToolOperationID(run *domain.TurnRun, call tool.ToolCall, ordinal int) string {
	prefix := "run"
	if run != nil && strings.TrimSpace(run.ID) != "" {
		prefix = strings.TrimSpace(run.ID)
	}
	callID := strings.TrimSpace(call.CallID)
	if callID == "" {
		callID = strings.TrimSpace(normalizedToolName(call))
	}
	if callID == "" {
		return fmt.Sprintf("%s:ordinal-%d", prefix, ordinal)
	}
	return prefix + ":" + callID
}
