package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

type immutableArtifactStoreStub struct {
	stubTurnArtifactStore
}

func (s *immutableArtifactStoreStub) PutImmutableBytes(_ context.Context, key string, data []byte, _ string) error {
	if s.data == nil {
		s.data = make(map[string][]byte)
	}
	s.data[key] = append([]byte(nil), data...)
	return nil
}

func (*immutableArtifactStoreStub) ImmutableRunArtifactKey(conversationID, turnID string, stepIndex int, runID string, artifact string) string {
	return "conversations/" + conversationID + "/turns/" + turnID + "/runs/" + runID + "/" + artifact
}

func TestPersistImmutableRunSuccessWritesCompleteCheckpointAndMetadata(t *testing.T) {
	artifacts := &immutableArtifactStoreStub{}
	runs := &stubScheduledRunStore{}
	runner := &TurnRunner{blobs: artifacts, runs: runs}
	run := &domain.TurnRun{ID: "run-1", TurnID: "turn-1", StepIndex: 1}
	state := &ScheduledRunState{Request: llm.ModelRequest{Input: []llm.ModelItem{{
		Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "history",
	}}}}
	outcome := &ScheduledRunOutcome{
		Model: &llm.ModelResult{RawResponse: json.RawMessage(`{"id":"response-1"}`)},
		ContextItems: []llm.ModelItem{{
			Type: llm.ModelItemMessage, Role: domain.RoleAssistant, Content: "answer",
		}},
		ToolResults: []llm.ModelItem{{
			Type: llm.ModelItemFunctionCallOutput, CallID: "call-1", Output: "result",
		}},
	}

	_, checkpointKey, err := runner.persistImmutableRunSuccess(t.Context(), "conv-1", "turn-1", run, state, outcome)
	if err != nil {
		t.Fatalf("persist immutable run success: %v", err)
	}
	compressed := artifacts.data[checkpointKey]
	payload, err := decompressImmutableRunPayload(compressed)
	if err != nil {
		t.Fatalf("decode checkpoint: %v", err)
	}
	var checkpoint immutableContextCheckpoint
	if err := json.Unmarshal(payload, &checkpoint); err != nil {
		t.Fatalf("unmarshal checkpoint: %v", err)
	}
	if len(checkpoint.ModelItems) != 2 || checkpoint.ModelItems[0].Content != "history" || checkpoint.ModelItems[1].Content != "answer" {
		t.Fatalf("checkpoint model items = %#v", checkpoint.ModelItems)
	}

	indexed := make(map[string]RunArtifactMetadata)
	for _, artifact := range runs.artifacts {
		indexed[artifact.Name] = artifact
	}
	for _, name := range []string{
		immutableRunResponseArtifact,
		immutableRunOutputItemsArtifact,
		immutableRunToolResultsArtifact,
		immutableRunPresentationArtifact,
		immutableRunCheckpointArtifact,
	} {
		metadata, ok := indexed[name]
		if !ok || metadata.ObjectKey == "" || metadata.SHA256 == "" || metadata.CompressedSize <= 0 || metadata.UncompressedSize <= 0 {
			t.Fatalf("metadata for %s = %#v", name, metadata)
		}
	}
}

func TestPersistImmutableRunFailureIndexesFailureArtifact(t *testing.T) {
	artifacts := &immutableArtifactStoreStub{}
	runs := &stubScheduledRunStore{}
	runner := &TurnRunner{blobs: artifacts, runs: runs}
	run := &domain.TurnRun{ID: "run-1", TurnID: "turn-1", StepIndex: 1}

	if err := runner.persistImmutableRunFailure(t.Context(), "conv-1", "turn-1", run, context.Canceled); err != nil {
		t.Fatalf("persist immutable run failure: %v", err)
	}
	if len(runs.artifacts) != 1 || runs.artifacts[0].Name != immutableRunFailureArtifact || !strings.HasSuffix(runs.artifacts[0].ObjectKey, "/failure.json.zst") {
		t.Fatalf("failure metadata = %#v", runs.artifacts)
	}
}
