package workflow

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/cache"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

type canonicalCompactionStore struct {
	head *domain.ContextHead
}

func (s *canonicalCompactionStore) GetContextHead(context.Context, string) (*domain.ContextHead, error) {
	copy := *s.head
	return &copy, nil
}

func (*canonicalCompactionStore) HasActiveRetry(context.Context, string) (bool, error) {
	return false, nil
}

func (*canonicalCompactionStore) ListRawTailMessages(context.Context, string, int64, int64) ([]domain.Message, error) {
	return nil, nil
}

func (s *canonicalCompactionStore) CompleteCompaction(_ context.Context, _ string, anchor domain.AnchorObject, expectedLastSeq int64, activeContextTokens int) (*domain.ContextHead, error) {
	return s.complete(anchor, expectedLastSeq, activeContextTokens)
}

func (s *canonicalCompactionStore) CompletePreflightCompaction(_ context.Context, _ string, _ string, anchor domain.AnchorObject, expectedLastSeq int64, activeContextTokens int) (*domain.ContextHead, error) {
	return s.complete(anchor, expectedLastSeq, activeContextTokens)
}

func (s *canonicalCompactionStore) complete(anchor domain.AnchorObject, expectedLastSeq int64, activeContextTokens int) (*domain.ContextHead, error) {
	if s.head.LastSeq != expectedLastSeq {
		return nil, domain.ErrConflict
	}
	s.head.AnchorGeneration = anchor.Generation
	s.head.AnchorKey = anchor.ObjectKey
	s.head.CoveredUntilSeq = anchor.CoveredUntilSeq
	s.head.RawTailStartSeq = anchor.CoveredUntilSeq + 1
	s.head.ActiveContextTokens = activeContextTokens
	s.head.Version++
	copy := *s.head
	return &copy, nil
}

type canonicalAnchorStore struct {
	anchor domain.AnchorObject
}

func (s *canonicalAnchorStore) PutJSON(_ context.Context, _ string, value any) error {
	s.anchor = value.(domain.AnchorObject)
	return nil
}

func (*canonicalAnchorStore) GetJSON(context.Context, string, any) error {
	return domain.ErrNotFound
}

func (*canonicalAnchorStore) ContextAnchorKey(conversationID string, generation int64) string {
	return conversationID + "/anchor/" + string(rune(generation))
}

func TestContextCompactorPassesCanonicalItemsUnchangedToModel(t *testing.T) {
	items := []llm.ModelItem{
		{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "request"},
		{Type: llm.ModelItemReasoning, Raw: json.RawMessage(`{"type":"reasoning","encrypted_content":"cipher"}`)},
		{Type: llm.ModelItemFunctionCall, CallID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"q":"x"}`)},
		{Type: llm.ModelItemFunctionCallOutput, CallID: "call-1", Output: "result"},
		{Type: llm.ModelItemMCPCall, Raw: json.RawMessage(`{"type":"mcp_call","name":"remote"}`)},
		{Type: llm.ModelItemMessage, Role: domain.RoleAssistant, Content: "working"},
	}
	store := &canonicalCompactionStore{head: &domain.ContextHead{ConversationID: "conv-1", LastSeq: 1, RawTailStartSeq: 1}}
	anchors := &canonicalAnchorStore{}
	model := &stubModelClient{results: []*llm.ModelResult{{FinalText: "The task queried lookup and remote MCP, then remains in progress."}}}
	execution := testExecutionSnapshot()
	compactor := &ContextCompactor{
		settings: WorkflowSettings{AgentSystemPrompt: "system", AgentCompactPrompt: "Summarize this complete context.", CompactMaxOutputTokens: 256},
		store:    store, model: model, blobs: anchors, cache: cache.New(4, 4),
		models: &stubTurnExecutionReader{execution: execution, resolveErr: domain.NewValidationError("no default compaction model")},
	}
	state := &ScheduledRunState{
		StepIndex: 1, InitialInputCount: len(items), Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{ContextWindowTokens: execution.ContextWindowTokens, Input: cloneModelItems(items)},
	}

	compacted, didCompact, err := compactor.Compact(t.Context(), state)
	if err != nil {
		t.Fatalf("compact canonical context: %v", err)
	}
	if !didCompact || len(model.streamRequests) != 1 {
		t.Fatalf("didCompact=%t requests=%d", didCompact, len(model.streamRequests))
	}
	request := model.streamRequests[0]
	if !reflect.DeepEqual(request.Input[:len(items)], items) {
		t.Fatalf("compaction model did not receive original canonical items: %#v", request.Input)
	}
	if len(compacted.Request.Input) != 1 || compacted.InitialInputCount != 1 {
		t.Fatalf("replacement state = %#v", compacted)
	}
	if anchors.anchor.CoveredUntilSeq != 1 || anchors.anchor.Content == "" {
		t.Fatalf("replacement anchor = %#v", anchors.anchor)
	}
}

func TestContextCompactorHydratesExternalizedGeneratedImage(t *testing.T) {
	store := &canonicalCompactionStore{head: &domain.ContextHead{ConversationID: "conv-1", LastSeq: 1, RawTailStartSeq: 1}}
	anchors := &canonicalAnchorStore{}
	model := &stubModelClient{results: []*llm.ModelResult{{FinalText: "Image summarized."}}}
	execution := testExecutionSnapshot()
	execution.ContextWindowTokens = 10_000
	compactor := &ContextCompactor{
		settings: WorkflowSettings{AgentSystemPrompt: "system", AgentCompactPrompt: "Summarize this complete context.", CompactMaxOutputTokens: 256},
		store:    store, model: model, blobs: anchors, cache: cache.New(4, 4),
		models: &stubTurnExecutionReader{execution: execution},
		loader: &ContextLoader{attachmentBlobs: &stubContextLoaderArtifactStore{
			data: map[string][]byte{"generated.png": providerTestPNG(t, 2, 2)},
		}},
	}
	state := &ScheduledRunState{
		StepIndex: 1, InitialInputCount: 1, Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{ContextWindowTokens: execution.ContextWindowTokens, Input: []llm.ModelItem{{
			ID: "image-1", Type: llm.ModelItemImageGenerationCall,
			Raw: json.RawMessage(`{"type":"image_generation_call","result_ref":{"attachment_id":"attachment-1","object_key":"generated.png","content_type":"image/png"}}`),
		}}},
	}

	if _, didCompact, err := compactor.Compact(t.Context(), state); err != nil || !didCompact {
		t.Fatalf("compact image context: didCompact=%t err=%v", didCompact, err)
	}
	request := model.streamRequests[0]
	if request.Input[0].Type != llm.ModelItemMessage || !strings.Contains(string(request.Input[0].Raw), `data:image/png;base64,`) {
		t.Fatalf("compaction request image = %#v", request.Input[0])
	}
}
