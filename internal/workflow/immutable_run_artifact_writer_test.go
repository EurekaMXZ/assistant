package workflow

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

type immutableArtifactStoreStub struct {
	stubTurnArtifactStore
}

type immutableConflictArtifactStoreStub struct {
	immutableArtifactStoreStub
}

func (s *immutableConflictArtifactStoreStub) PutImmutableBytes(_ context.Context, key string, data []byte, _ string) error {
	if s.data == nil {
		s.data = make(map[string][]byte)
	}
	if existing, ok := s.data[key]; ok && !bytes.Equal(existing, data) {
		return domain.ErrConflict
	}
	s.data[key] = append([]byte(nil), data...)
	return nil
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
	state := &ScheduledRunState{InitialInputCount: 1, Request: llm.ModelRequest{Input: []llm.ModelItem{{
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

func TestCompleteRunCheckpointItemsDoesNotDuplicateToolChain(t *testing.T) {
	functionCall := llm.ModelItem{Type: llm.ModelItemFunctionCall, CallID: "call-1", Name: "lookup"}
	functionOutput := llm.ModelItem{Type: llm.ModelItemFunctionCallOutput, CallID: "call-1", Output: "result"}
	state := &ScheduledRunState{
		InitialInputCount: 1,
		Request: llm.ModelRequest{Input: []llm.ModelItem{
			{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "research"},
			functionCall,
			functionOutput,
		}},
	}
	outcome := &ScheduledRunOutcome{ContextItems: []llm.ModelItem{
		functionCall,
		functionOutput,
		{Type: llm.ModelItemMessage, Role: domain.RoleAssistant, Content: "answer"},
	}}

	items, err := completeRunCheckpointItems(state, outcome)
	if err != nil {
		t.Fatalf("complete run checkpoint items: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("checkpoint item count = %d, want 4: %#v", len(items), items)
	}
	if items[0].Content != "research" || items[1].CallID != "call-1" || items[2].Output != "result" || items[3].Content != "answer" {
		t.Fatalf("checkpoint items = %#v", items)
	}
}

func TestCompleteRunCheckpointItemsRetainsReplacementContext(t *testing.T) {
	checkpoint := llm.ModelItem{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: formatConversationCheckpoint("model summary")}
	state := &ScheduledRunState{
		InitialInputCount: 1,
		Request:           llm.ModelRequest{Input: []llm.ModelItem{checkpoint}},
	}
	outcome := &ScheduledRunOutcome{ContextItems: []llm.ModelItem{{
		Type: llm.ModelItemMessage, Role: domain.RoleAssistant, Content: "answer",
	}}}

	items, err := completeRunCheckpointItems(state, outcome)
	if err != nil {
		t.Fatalf("complete checkpoint: %v", err)
	}
	if len(items) != 2 || items[0].Content != checkpoint.Content || items[1].Content != "answer" {
		t.Fatalf("checkpoint items = %#v", items)
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

func TestPersistImmutableRunRequestPreservesEarlierArtifactOnDispatchRewrite(t *testing.T) {
	artifacts := &immutableConflictArtifactStoreStub{}
	runs := &stubScheduledRunStore{}
	runner := &TurnRunner{blobs: artifacts, runs: runs}

	if err := runner.persistImmutableRunRequest(t.Context(), "conv-1", "turn-1", 1, "run-1", []byte(`{"input":"before"}`)); err != nil {
		t.Fatalf("persist initial request: %v", err)
	}
	if err := runner.persistImmutableRunRequest(t.Context(), "conv-1", "turn-1", 1, "run-1", []byte(`{"input":"after"}`)); err != nil {
		t.Fatalf("persist rewritten dispatch request: %v", err)
	}
	if len(runs.artifacts) != 2 || runs.artifacts[1].Name != immutableRunDispatchRequestArtifact {
		t.Fatalf("request artifact metadata = %#v", runs.artifacts)
	}
}

func TestExternalizedGeneratedImageIsExcludedFromImmutableRunArtifacts(t *testing.T) {
	imageData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Z4sUAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode image fixture: %v", err)
	}
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)
	artifacts := &immutableArtifactStoreStub{}
	runs := &stubScheduledRunStore{}
	runner := &TurnRunner{
		blobs:                artifacts,
		runs:                 runs,
		conversations:        ownedConversationReader(),
		generatedAttachments: &stubGeneratedAttachmentStore{},
		generatedImageAssets: &stubGeneratedImageAssetStore{},
	}
	run := &domain.TurnRun{ID: "run-1", TurnID: "turn-1", StepIndex: 1}
	outcome := &ScheduledRunOutcome{Model: &llm.ModelResult{
		ResponseID:  "response-1",
		RawResponse: json.RawMessage(`{"response":{"output":[{"id":"image-1","type":"image_generation_call","result":"` + imageBase64 + `"}]}}`),
		OutputItems: []llm.ModelItem{{
			ID: "image-1", Type: llm.ModelItemImageGenerationCall, Result: imageBase64,
		}},
	}}

	if err := runner.externalizeGeneratedImages(t.Context(), &domain.Turn{ID: "turn-1", ConversationID: "conv-1"}, run.ID, outcome); err != nil {
		t.Fatalf("externalize generated image: %v", err)
	}
	if strings.Contains(string(outcome.Model.RawResponse), imageBase64) || strings.Contains(string(outcome.Model.OutputItems[0].Raw), imageBase64) {
		t.Fatalf("generated image base64 remained in model result: %#v", outcome.Model)
	}
	if _, _, err := runner.persistImmutableRunSuccess(t.Context(), "conv-1", "turn-1", run, &ScheduledRunState{}, outcome); err != nil {
		t.Fatalf("persist immutable run success: %v", err)
	}
	for key, compressed := range artifacts.data {
		if !strings.Contains(key, "/runs/") {
			continue
		}
		payload, err := decompressImmutableRunPayload(compressed)
		if err != nil {
			t.Fatalf("decode immutable artifact %q: %v", key, err)
		}
		if strings.Contains(string(payload), imageBase64) {
			t.Fatalf("immutable artifact %q contains generated image base64", key)
		}
	}
}
