package workflow

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

func TestPrepareNextScheduledRunAppendsSandboxImageAfterToolOutputs(t *testing.T) {
	artifacts := &stubToolArtifactStore{}
	imageData := providerTestPNG(t, 2, 2)
	digest := sha256.Sum256(imageData)
	objectKey := "assistant-attachments/conv-1/turn-1/call-1/chart.png"
	output := fmt.Sprintf(`{"conversation_id":"conv-1","turn_id":"turn-1","file":{"path":"/workspace/chart.png","content_type":"image/png","size_bytes":%d,"image":{"attachment_id":"attachment-1","object_key":"%s","content_type":"image/png","size_bytes":%d,"sha256":"%x"}}}`,
		len(imageData), objectKey, len(imageData), digest)
	if err := artifacts.PutBytes(t.Context(), "output-1", []byte(output), "application/json"); err != nil {
		t.Fatal(err)
	}
	if err := artifacts.PutBytes(t.Context(), "output-2", []byte(`{"value":2}`), "application/json"); err != nil {
		t.Fatal(err)
	}
	orchestrator := NewToolOrchestrator(&stubModelClient{}, nil, nil, artifacts, nil)
	state := &ScheduledRunState{
		Version: scheduledRunStateVersion, StepIndex: 1, InitialInputCount: 1,
		Scope:   tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{Model: "gpt-test", Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "inspect"}}},
	}
	parallel := true
	plan, err := buildToolExecutionPlan(&domain.TurnRun{ID: "run-1", TurnID: "turn-1", Attempt: 1}, []tool.ToolCall{{
		Type: llm.ModelItemFunctionCall, Namespace: "sandbox", Name: "read_file", CallID: "call-1",
	}, {
		Type: llm.ModelItemFunctionCall, Name: "lookup", CallID: "call-2",
	}}, ToolExecutionPlanOptions{ParallelToolCalls: &parallel})
	if err != nil {
		t.Fatal(err)
	}
	outcome := &ScheduledRunOutcome{
		Model: &llm.ModelResult{OutputItems: []llm.ModelItem{
			{Type: llm.ModelItemFunctionCall, Namespace: "sandbox", Name: "read_file", CallID: "call-1"},
			{Type: llm.ModelItemFunctionCall, Name: "lookup", CallID: "call-2"},
		}},
		ExecutionPlan: plan,
	}
	if err := orchestrator.PrepareNextScheduledRunFromToolCalls(t.Context(), state, outcome, []domain.ToolCallRecord{{
		CallID: "call-1", Namespace: "sandbox", ToolName: "read_file", Status: domain.ToolCallStatusCompleted,
		OutputBlobKey: "output-1", StableOperationID: plan.Groups[0].Calls[0].StableOperationID,
	}, {
		CallID: "call-2", ToolName: "lookup", Status: domain.ToolCallStatusCompleted,
		OutputBlobKey: "output-2", StableOperationID: plan.Groups[0].Calls[1].StableOperationID,
	}}); err != nil {
		t.Fatalf("prepare next run: %v", err)
	}
	input := outcome.NextState.Request.Input
	if len(input) != 6 || input[3].Type != llm.ModelItemFunctionCallOutput || input[4].Type != llm.ModelItemFunctionCallOutput || input[5].Type != llm.ModelItemMessage || input[5].Role != domain.RoleUser {
		t.Fatalf("unexpected next input: %#v", input)
	}
	raw := string(input[5].Raw)
	if !strings.Contains(raw, `"type":"input_image"`) || !strings.Contains(raw, `"image_ref"`) || strings.Contains(raw, "data:image") {
		t.Fatalf("unexpected sandbox image continuation: %s", raw)
	}
	if !strings.Contains(raw, objectKey) {
		t.Fatalf("image object reference missing: %s", raw)
	}
	if outputRaw, err := json.Marshal(input[3]); err != nil || strings.Contains(string(outputRaw), "data:image") {
		t.Fatalf("tool output unexpectedly contains image data: %s", outputRaw)
	}
	loader := &ContextLoader{attachmentBlobs: &stubContextLoaderArtifactStore{data: map[string][]byte{objectKey: imageData}}}
	if err := loader.hydrateScheduledRunImages(t.Context(), outcome.NextState); err != nil {
		t.Fatalf("hydrate sandbox image continuation: %v", err)
	}
	hydrated := string(outcome.NextState.Request.Input[5].Raw)
	if !strings.Contains(hydrated, "data:image/png;base64,") || strings.Contains(hydrated, `"image_ref"`) {
		t.Fatalf("sandbox image continuation was not hydrated: %s", hydrated)
	}
}
