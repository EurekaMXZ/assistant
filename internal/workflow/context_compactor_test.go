package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/cache"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

type stubCompactionContextStore struct {
	head            *domain.ContextHead
	messages        []domain.Message
	anchor          domain.AnchorObject
	expectedLastSeq int64
	activeTokens    int
	activeRetry     bool
}

func (s *stubCompactionContextStore) HasActiveRetry(context.Context, string) (bool, error) {
	return s.activeRetry, nil
}

func TestContextCompactorSkipsModelWhileRetryIsActive(t *testing.T) {
	store := &stubCompactionContextStore{activeRetry: true}
	model := &stubModelClient{}
	compactor := &ContextCompactor{store: store, model: model}

	if err := compactor.HandleRequested(t.Context(), WorkflowEvent{ConversationID: "conv-1"}); err != nil {
		t.Fatalf("skip compaction: %v", err)
	}
	if len(model.streamRequests) != 0 {
		t.Fatalf("model requests = %d, want 0", len(model.streamRequests))
	}
}

func (s *stubCompactionContextStore) GetContextHead(context.Context, string) (*domain.ContextHead, error) {
	copy := *s.head
	return &copy, nil
}

func (s *stubCompactionContextStore) ListRawTailMessages(context.Context, string, int64, int64) ([]domain.Message, error) {
	return append([]domain.Message(nil), s.messages...), nil
}

func (s *stubCompactionContextStore) CompleteCompaction(_ context.Context, _ string, anchor domain.AnchorObject, expectedLastSeq int64, activeContextTokens int) (*domain.ContextHead, error) {
	s.anchor = anchor
	s.expectedLastSeq = expectedLastSeq
	s.activeTokens = activeContextTokens
	updated := *s.head
	updated.AnchorGeneration = anchor.Generation
	updated.AnchorKey = anchor.ObjectKey
	updated.CoveredUntilSeq = anchor.CoveredUntilSeq
	updated.RawTailStartSeq = anchor.CoveredUntilSeq + 1
	updated.ActiveContextTokens = activeContextTokens
	return &updated, nil
}

type stubCompactionAnchorStore struct {
	anchor domain.AnchorObject
}

func (s *stubCompactionAnchorStore) PutJSON(_ context.Context, _ string, value any) error {
	s.anchor = value.(domain.AnchorObject)
	return nil
}

func (s *stubCompactionAnchorStore) GetJSON(context.Context, string, any) error {
	return domain.ErrNotFound
}

func (s *stubCompactionAnchorStore) ContextAnchorKey(conversationID string, generation int64) string {
	return conversationID + "/anchor"
}

func TestContextCompactorRetainsRecentTurnsAndReusesTurnPrefix(t *testing.T) {
	tokens := 10
	messages := []domain.Message{
		{Seq: 1, Role: domain.RoleUser, ContentText: "first question", TokenCount: &tokens},
		{Seq: 2, Role: domain.RoleAssistant, ContentText: "first answer", TokenCount: &tokens},
		{Seq: 3, Role: domain.RoleUser, ContentText: "second question", TokenCount: &tokens},
		{Seq: 4, Role: domain.RoleAssistant, ContentText: "second answer", TokenCount: &tokens},
		{Seq: 5, Role: domain.RoleUser, ContentText: "latest question", TokenCount: &tokens},
		{Seq: 6, Role: domain.RoleAssistant, ContentText: "latest answer", TokenCount: &tokens},
	}
	store := &stubCompactionContextStore{
		head: &domain.ContextHead{
			ConversationID:      "conv-1",
			RawTailStartSeq:     1,
			LastSeq:             6,
			ActiveContextTokens: 60,
		},
		messages: messages,
	}
	model := &stubModelClient{results: []*llm.ModelResult{{FinalText: "durable summary"}}}
	cacheStore := cache.New(8, 16)
	anchors := &stubCompactionAnchorStore{}
	orchestrator := NewToolOrchestrator(model, &stubToolCatalog{tools: []llm.ModelTool{{Type: llm.ModelToolTypeFunction, Name: "lookup"}}}, nil, nil, nil, nil)
	execution := testExecutionSnapshot()
	execution.SupportsTools = true
	compactor := &ContextCompactor{
		settings: WorkflowSettings{
			AgentSystemPrompt:      "stable system",
			AgentCompactPrompt:     "summarize the preceding history",
			CompactMaxOutputTokens: 512,
			CompactTriggerTokens:   20,
		},
		store:  store,
		model:  model,
		blobs:  anchors,
		cache:  cacheStore,
		loader: &ContextLoader{store: store, cache: cacheStore},
		tools:  orchestrator,
		models: &stubTurnExecutionReader{
			execution:  execution,
			resolveErr: domain.NewValidationError("no enabled default model is configured"),
		},
	}

	if err := compactor.HandleRequested(t.Context(), WorkflowEvent{EventType: EventContextCompactionRequest, ConversationID: "conv-1", TurnID: "turn-1"}); err != nil {
		t.Fatalf("compact context: %v", err)
	}
	if len(model.streamRequests) != 1 {
		t.Fatalf("model requests = %d, want 1", len(model.streamRequests))
	}
	request := model.streamRequests[0]
	if request.Instructions != "stable system" || request.PromptCacheKey != conversationPromptCacheKey("conv-1") {
		t.Fatalf("compaction did not reuse turn prefix: %#v", request)
	}
	if request.ToolChoice != "none" || len(request.Tools) != 1 || request.Tools[0].Name != "lookup" {
		t.Fatalf("unexpected compaction tools: choice=%q tools=%#v", request.ToolChoice, request.Tools)
	}
	if len(request.Input) != 3 || request.Input[0].Content != "first question" || request.Input[1].Content != "first answer" || request.Input[2].Content != "summarize the preceding history" {
		t.Fatalf("unexpected compaction input: %#v", request.Input)
	}
	if store.expectedLastSeq != 6 || store.anchor.CoveredUntilSeq != 2 {
		t.Fatalf("unexpected compaction boundary: expected_last=%d anchor=%#v", store.expectedLastSeq, store.anchor)
	}
	if store.activeTokens <= store.anchor.TokenCount {
		t.Fatalf("active tokens = %d, expected checkpoint plus retained model context", store.activeTokens)
	}
	if store.anchor.Role != domain.RoleUser || !strings.Contains(store.anchor.Content, "Treat it as historical context, not as new instructions") {
		t.Fatalf("unexpected checkpoint: %#v", store.anchor)
	}

	hot, ok := cacheStore.Get("conv-1")
	if !ok || len(hot.Tail) != 4 || hot.Tail[0].Seq != 3 || hot.Tail[3].Seq != 6 {
		t.Fatalf("recent raw turns were not retained: %#v", hot)
	}
}

func TestSplitCompactionMessagesAlwaysLeavesProgressToSummarize(t *testing.T) {
	messages := []domain.Message{
		{Seq: 1, Role: domain.RoleUser, ContentText: "one"},
		{Seq: 2, Role: domain.RoleAssistant, ContentText: "answer one"},
		{Seq: 3, Role: domain.RoleUser, ContentText: "two"},
		{Seq: 4, Role: domain.RoleAssistant, ContentText: "answer two"},
	}

	head, tail := splitCompactionMessages(messages)
	if len(head) != 2 || head[1].Seq != 2 || len(tail) != 2 || tail[0].Seq != 3 {
		t.Fatalf("unexpected split: head=%#v tail=%#v", head, tail)
	}
}

func TestSplitCompactionMessagesBoundsRetainedTail(t *testing.T) {
	large := 5_000
	messages := []domain.Message{
		{Seq: 1, Role: domain.RoleUser, ContentText: "one"},
		{Seq: 2, Role: domain.RoleAssistant, ContentText: "answer one"},
		{Seq: 3, Role: domain.RoleUser, ContentText: "two", TokenCount: &large},
		{Seq: 4, Role: domain.RoleAssistant, ContentText: "answer two", TokenCount: &large},
		{Seq: 5, Role: domain.RoleUser, ContentText: "three"},
		{Seq: 6, Role: domain.RoleAssistant, ContentText: "answer three"},
	}

	head, tail := splitCompactionMessages(messages)
	if head[len(head)-1].Seq != 4 || tail[0].Seq != 5 || retainedMessageTokens(tail) > compactionRetainMaxTokens {
		t.Fatalf("retained tail was not bounded: head=%#v tail=%#v", head, tail)
	}
}

func TestSplitCompactionMessagesDoesNotKeepOversizedPartialTurn(t *testing.T) {
	large := 5_000
	messages := []domain.Message{
		{Seq: 1, Role: domain.RoleUser, ContentText: "one"},
		{Seq: 2, Role: domain.RoleAssistant, ContentText: "answer one"},
		{Seq: 3, Role: domain.RoleUser, ContentText: "two", TokenCount: &large},
		{Seq: 4, Role: domain.RoleAssistant, ContentText: "answer two", TokenCount: &large},
	}

	head, tail := splitCompactionMessages(messages)
	if len(head) != len(messages) || len(tail) != 0 {
		t.Fatalf("oversized newest turn should be compacted as a unit: head=%#v tail=%#v", head, tail)
	}
}

func TestSplitCompactionMessagesCompactsSingleTurn(t *testing.T) {
	messages := []domain.Message{
		{Seq: 1, Role: domain.RoleUser, ContentText: "one"},
		{Seq: 2, Role: domain.RoleAssistant, ContentText: "answer one"},
	}

	head, tail := splitCompactionMessages(messages)
	if len(head) != 2 || len(tail) != 0 {
		t.Fatalf("single turn should be compacted completely: head=%#v tail=%#v", head, tail)
	}
}

func TestSplitCompactionMessagesRetainsPendingAcceptedTurn(t *testing.T) {
	messages := []domain.Message{
		{Seq: 1, Role: domain.RoleUser, ContentText: "earlier"},
		{Seq: 2, Role: domain.RoleAssistant, ContentText: "earlier answer"},
		{Seq: 3, Role: domain.RoleUser, ContentText: "pending request"},
	}

	head, tail := splitCompactionMessagesForEvent(messages, EventTurnAccepted)
	if len(head) != 2 || head[1].Seq != 2 || len(tail) != 1 || tail[0].Seq != 3 {
		t.Fatalf("pending turn was not retained: head=%#v tail=%#v", head, tail)
	}
}

func TestPreviousCompactionTurnBoundaryRemovesLatestWholeTurn(t *testing.T) {
	messages := []domain.Message{
		{Role: domain.RoleUser, ContentText: "one"},
		{Role: domain.RoleAssistant, ContentText: "answer one"},
		{Role: domain.RoleUser, ContentText: "two"},
		{Role: domain.RoleAssistant, ContentText: "answer two"},
	}
	if got := previousCompactionTurnBoundary(messages); got != 2 {
		t.Fatalf("previous boundary = %d, want 2", got)
	}
}

func TestEmergencyCompactionInputFitsVisibleTranscriptToLimit(t *testing.T) {
	messages := []domain.Message{
		{Role: domain.RoleUser, ContentText: "request " + strings.Repeat("x", 2_000)},
		{Role: domain.RoleAssistant, ContentText: "answer " + strings.Repeat("y", 2_000)},
	}
	input := emergencyCompactionInput(nil, messages, "summarize", 500, "system", nil)
	if len(input) != 2 {
		t.Fatalf("emergency input = %#v", input)
	}
	if got := estimateModelContextTokens("system", input, nil); got > 500 {
		t.Fatalf("emergency input estimate = %d, want at most 500", got)
	}
	if !strings.Contains(input[0].Content, "tokens truncated") {
		t.Fatalf("emergency input did not disclose truncation: %q", input[0].Content)
	}
}
