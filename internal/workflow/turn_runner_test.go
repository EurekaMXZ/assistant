package workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/EurekaMXZ/assistant/internal/cache"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

type stubConversationSandboxReader struct {
	conversationID string
	sandbox        *domain.ConversationSandbox
	err            error
}

type stubWorkflowConversationReader struct {
	conversation *domain.Conversation
	err          error
}

func (s *stubWorkflowConversationReader) GetConversation(context.Context, string) (*domain.Conversation, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.conversation == nil {
		return nil, domain.ErrNotFound
	}
	return s.conversation, nil
}

func ownedConversationReader() *stubWorkflowConversationReader {
	return &stubWorkflowConversationReader{conversation: &domain.Conversation{ID: "conv-1", OwnerUserID: "user-1"}}
}

type stubGeneratedAttachmentStore struct {
	params []assistantattachment.CreateAttachmentParams
}

type stubRunnerContextStore struct {
	head     *domain.ContextHead
	messages []domain.Message
	err      error
}

func (s *stubRunnerContextStore) GetContextHead(context.Context, string) (*domain.ContextHead, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.head, nil
}

func (s *stubRunnerContextStore) HasActiveRetry(context.Context, string) (bool, error) {
	return false, nil
}

func (s *stubRunnerContextStore) ListRawTailMessages(context.Context, string, int64, int64) ([]domain.Message, error) {
	return append([]domain.Message(nil), s.messages...), nil
}

func (s *stubRunnerContextStore) CompleteCompaction(context.Context, string, domain.AnchorObject, int64, int) (*domain.ContextHead, error) {
	return s.head, nil
}

type stubScheduledRunStore struct {
	started      int
	completed    int
	scheduled    int
	checkpoints  int
	awaited      int
	awaitInput   AwaitScheduledTurnRunInput
	runID        string
	run          *domain.TurnRun
	failErr      error
	failed       int
	parentFailed int
	cancelled    int
	artifacts    []RunArtifactMetadata
}

type blockingCancellationModel struct {
	started chan struct{}
	once    sync.Once
}

func (m *blockingCancellationModel) MarshalRequest(llm.ModelRequest) (json.RawMessage, error) {
	return json.RawMessage(`{"model":"gpt-test"}`), nil
}

func (m *blockingCancellationModel) StreamResponse(ctx context.Context, _ llm.ModelRequest, _ llm.ModelEventHandler) (*llm.ModelResult, error) {
	m.once.Do(func() { close(m.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestTurnRunnerCancelActiveRun(t *testing.T) {
	runner := &TurnRunner{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	unregister := runner.registerActiveRun("turn-1", "run-1", cancel)

	if got := runner.cancelActiveRun("turn-1"); got != "run-1" {
		t.Fatalf("cancelled run id = %q, want run-1", got)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("active run context was not cancelled")
	}
	unregister()
	if got := runner.cancelActiveRun("turn-1"); got != "" {
		t.Fatalf("run id after unregister = %q, want empty", got)
	}
}

func TestTurnRunnerCancellationPublishesTerminalStreamEvent(t *testing.T) {
	runs := &stubScheduledRunStore{}
	publisher := &recordingPublisher{}
	runner := &TurnRunner{runs: runs, streamHub: publisher}

	if err := runner.HandleCancellationRequested(t.Context(), WorkflowEvent{
		ConversationID: "conv-1", TurnID: "turn-1",
	}); err != nil {
		t.Fatalf("handle cancellation: %v", err)
	}
	if runs.cancelled != 1 {
		t.Fatalf("cancellation finalizations = %d, want 1", runs.cancelled)
	}
	if len(publisher.events) != 2 || publisher.events[0].Type != domain.ConversationEventRunCancelled || publisher.events[1].Type != stream.EventTurnDone {
		t.Fatalf("cancellation stream events = %#v", publisher.events)
	}
}

func TestTurnRunnerCancellationCancelsProviderContext(t *testing.T) {
	model := &blockingCancellationModel{started: make(chan struct{})}
	artifacts := &stubToolArtifactStore{}
	orchestrator := NewToolOrchestrator(model, &stubToolCatalog{}, nil, nil, artifacts, nil)
	state := &ScheduledRunState{
		Version: scheduledRunStateVersion, StepIndex: 1, InitialInputCount: 1,
		Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{
			Model: "gpt-test", ContextWindowTokens: 1_000,
			Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "hello"}},
		},
	}
	stateKey, _, err := orchestrator.PersistScheduledRunState(t.Context(), state.Scope, state, json.RawMessage(`{"step":1}`))
	if err != nil {
		t.Fatalf("persist scheduled state: %v", err)
	}
	runs := &stubScheduledRunStore{run: &domain.TurnRun{
		ID: "run-1", TurnID: "turn-1", StepIndex: 1, Status: domain.TurnRunStatusQueued, StateBlobKey: stateKey,
	}}
	runner := &TurnRunner{
		settings:  WorkflowSettings{WorkerLeaseTimeout: time.Hour},
		tools:     orchestrator,
		runs:      runs,
		streamHub: &recordingPublisher{},
		blobs:     &stubTurnArtifactStore{},
	}
	done := make(chan error, 1)
	go func() {
		done <- runner.HandleTurnRunRequested(t.Context(), WorkflowEvent{
			EventType: EventTurnRunRequested, ConversationID: "conv-1", TurnID: "turn-1", TurnRunID: "run-1",
		})
	}()
	select {
	case <-model.started:
	case <-time.After(time.Second):
		t.Fatal("provider request did not start")
	}
	if got := runner.cancelActiveRun("turn-1"); got != "run-1" {
		t.Fatalf("cancelled run id = %q, want run-1", got)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancelled run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("provider request did not stop after cancellation")
	}
}

func TestTurnRunnerPersistsCheckpointAndPausesForAskUser(t *testing.T) {
	arguments := json.RawMessage(`{"prompt":"Continue?","kind":"single_choice","options":[{"id":"yes","label":"Yes","tone":"primary"},{"id":"cancel","label":"Cancel","tone":"neutral"}]}`)
	model := &stubModelClient{results: []*llm.ModelResult{{
		ResponseID: "resp-ask",
		Usage:      llm.ModelUsage{InputTokens: 7, OutputTokens: 3, TotalTokens: 10},
		OutputItems: []llm.ModelItem{
			{Type: llm.ModelItemFunctionCall, CallID: "call-rename", Name: "conversation_rename_title", Arguments: json.RawMessage(`{"title":"Updated"}`)},
			{Type: llm.ModelItemFunctionCall, CallID: "call-ask", Name: tool.AskUser, Arguments: arguments},
		},
	}}}
	artifacts := &stubToolArtifactStore{}
	executor := &stubToolExecutor{results: []*tool.ToolExecutionResult{
		{OutputItem: llm.ModelItem{Type: llm.ModelItemFunctionCallOutput, CallID: "call-rename", Output: `{"renamed":true}`}},
		{AwaitingInput: &tool.AskUserPrompt{
			Prompt: "Continue?", Kind: tool.AskUserKindSingleChoice,
			Options: []tool.AskUserOption{{ID: "yes", Label: "Yes", Tone: tool.AskUserTonePrimary}, {ID: "cancel", Label: "Cancel", Tone: tool.AskUserToneNeutral}},
		}},
	}}
	orchestrator := NewToolOrchestrator(
		model, &stubToolCatalog{tools: []llm.ModelTool{
			{Type: llm.ModelToolTypeFunction, Name: "conversation_rename_title"},
			{Type: llm.ModelToolTypeFunction, Name: tool.AskUser},
		}}, executor, nil, artifacts, &stubToolCallStore{},
	)
	state := &ScheduledRunState{
		Version: scheduledRunStateVersion, StepIndex: 1, InitialInputCount: 1,
		Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{
			Model: "gpt-test", ContextWindowTokens: 1_000,
			Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "start"}},
			Tools: []llm.ModelTool{
				{Type: llm.ModelToolTypeFunction, Name: "conversation_rename_title"},
				{Type: llm.ModelToolTypeFunction, Name: tool.AskUser},
			},
		},
	}
	stateKey, _, err := orchestrator.PersistScheduledRunState(t.Context(), state.Scope, state, json.RawMessage(`{"step":1}`))
	if err != nil {
		t.Fatal(err)
	}
	runs := &stubScheduledRunStore{run: &domain.TurnRun{
		ID: "run-1", TurnID: "turn-1", StepIndex: 1, Attempt: 1,
		Status: domain.TurnRunStatusQueued, StateBlobKey: stateKey,
	}}
	publisher := &recordingPublisher{}
	runner := &TurnRunner{
		settings: WorkflowSettings{WorkerLeaseTimeout: time.Hour}, tools: orchestrator,
		runs: runs, streamHub: publisher, blobs: &stubTurnArtifactStore{},
	}
	if err := runner.HandleTurnRunRequested(t.Context(), WorkflowEvent{
		EventType: EventTurnRunRequested, ConversationID: "conv-1", TurnID: "turn-1", TurnRunID: "run-1",
	}); err != nil {
		t.Fatalf("handle ask_user run: %v", err)
	}
	if runs.run.Status != domain.TurnRunStatusAwaitingInput || runs.completed != 0 || runs.scheduled != 0 || runs.checkpoints < 2 {
		t.Fatalf("run state=%#v completed=%d scheduled=%d checkpoints=%d", runs.run, runs.completed, runs.scheduled, runs.checkpoints)
	}
	if runs.awaited != 1 || runs.awaitInput.ToolCallID != "record-call-ask" || runs.awaitInput.Usage.TotalTokens != 10 {
		t.Fatalf("await input settlement = %#v count=%d", runs.awaitInput, runs.awaited)
	}
	resultKey := artifacts.TurnRunResultKey("conv-1", "turn-1", 1)
	outcome, err := orchestrator.LoadScheduledRunOutcome(t.Context(), resultKey)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Postprocessed || outcome.NextState != nil {
		t.Fatalf("persisted awaiting outcome = %#v", outcome)
	}
	if len(outcome.ToolResults) != 1 || outcome.ToolResults[0].CallID != "call-rename" || outcome.ToolResults[0].Output != `{"renamed":true}` {
		t.Fatalf("persisted ordinary tool results = %#v", outcome.ToolResults)
	}
	if len(executor.calls) != 2 || executor.calls[0].CallID != "call-rename" || executor.calls[1].CallID != "call-ask" {
		t.Fatalf("mixed tool executions = %#v", executor.calls)
	}
	lastEvent := publisher.events[len(publisher.events)-1]
	if lastEvent.Type != stream.EventInteractionAwaiting || lastEvent.ItemID != "ask-user:record-call-ask" {
		t.Fatalf("awaiting presentation events = %#v", publisher.events)
	}
}

func TestTurnRunnerIgnoresAwaitingInputRunUntilAnswered(t *testing.T) {
	runs := &stubScheduledRunStore{run: &domain.TurnRun{ID: "run-1", TurnID: "turn-1", Status: domain.TurnRunStatusAwaitingInput}}
	runner := &TurnRunner{runs: runs}
	if err := runner.HandleTurnRunRequested(t.Context(), WorkflowEvent{TurnRunID: "run-1"}); err != nil {
		t.Fatal(err)
	}
	if runs.run.Status != domain.TurnRunStatusAwaitingInput || runs.completed != 0 || runs.checkpoints != 0 {
		t.Fatalf("awaiting run was processed: %#v", runs)
	}
}

type stubTurnWorkflowStore struct {
	failures    int
	turn        *domain.Turn
	userMessage *domain.Message
}

func (s *stubTurnWorkflowStore) GetTurn(_ context.Context, turnID string) (*domain.Turn, error) {
	if s.turn != nil {
		clone := *s.turn
		return &clone, nil
	}
	return &domain.Turn{ID: turnID}, nil
}

func (s *stubTurnWorkflowStore) GetUserMessageByTurn(context.Context, string) (*domain.Message, error) {
	if s.userMessage != nil {
		clone := *s.userMessage
		return &clone, nil
	}
	return &domain.Message{Role: domain.RoleUser, ContentText: "retry"}, nil
}

func (s *stubTurnWorkflowStore) MarkTurnContextReady(context.Context, string) (*domain.Turn, error) {
	return nil, nil
}

func (s *stubTurnWorkflowStore) FinalizeTurnSuccess(context.Context, string, []domain.AssistantMessageDraft, domain.TurnRunSummary, int) (*domain.Turn, []domain.Message, *domain.ContextHead, bool, error) {
	return nil, nil, nil, false, nil
}

func (s *stubTurnWorkflowStore) FinalizeTurnFailure(context.Context, string, string, string, string, string, int) error {
	s.failures++
	return nil
}

func (s *stubScheduledRunStore) StartTurnRun(context.Context, string, string, string, string, string) (string, error) {
	s.started++
	if s.runID == "" {
		s.runID = "run-1"
	}
	return s.runID, nil
}

func (s *stubScheduledRunStore) ScheduleNextTurnRun(context.Context, string, string, int, string, string, string, string) (string, error) {
	s.scheduled++
	return "run-next", nil
}

func (s *stubScheduledRunStore) GetTurnRun(context.Context, string) (*domain.TurnRun, error) {
	if s.run == nil {
		return nil, domain.ErrNotFound
	}
	clone := *s.run
	return &clone, nil
}

func (s *stubScheduledRunStore) ClaimTurnRun(context.Context, string) (*domain.TurnRun, TurnRunLease, error) {
	if s.run == nil || s.run.Status != domain.TurnRunStatusQueued {
		return nil, TurnRunLease{}, domain.ErrConflict
	}
	clone := *s.run
	clone.Status = domain.TurnRunStatusRunning
	s.run.Status = domain.TurnRunStatusRunning
	return &clone, TurnRunLease{TurnID: clone.TurnID, RunID: clone.ID, Token: "lease-1"}, nil
}

func (s *stubScheduledRunStore) RenewTurnRunLease(context.Context, TurnRunLease) error {
	return nil
}

func (s *stubScheduledRunStore) CheckpointScheduledTurnRun(context.Context, TurnRunLease, string, string, string) error {
	s.checkpoints++
	return nil
}

func (s *stubScheduledRunStore) AwaitScheduledTurnRunInput(_ context.Context, input AwaitScheduledTurnRunInput) (*domain.TurnRun, error) {
	s.awaited++
	s.awaitInput = input
	if s.run != nil {
		s.run.Status = domain.TurnRunStatusAwaitingInput
	}
	return s.run, nil
}

func (s *stubScheduledRunStore) CompleteScheduledTurnRun(context.Context, TurnRunLease, string, string, string, llm.ModelUsage, int, int) (*domain.TurnRun, error) {
	s.completed++
	if s.run != nil {
		s.run.Status = domain.TurnRunStatusCompleted
	}
	return s.run, nil
}

func (s *stubScheduledRunStore) FailScheduledTurnRun(context.Context, TurnRunLease, string, string, string, string, string, string, string, string, int) (*domain.TurnRun, error) {
	s.failed++
	if s.failErr != nil {
		return nil, s.failErr
	}
	if s.run != nil {
		s.run.Status = domain.TurnRunStatusFailed
	}
	s.parentFailed++
	return s.run, nil
}

func (s *stubScheduledRunStore) FinalizeTurnCancellation(context.Context, string, string) error {
	s.cancelled++
	return nil
}

func (s *stubScheduledRunStore) SetTurnRunArtifactMetadata(_ context.Context, _ string, artifacts []RunArtifactMetadata) error {
	s.artifacts = append(s.artifacts, artifacts...)
	return nil
}

func (s *stubGeneratedAttachmentStore) UpsertAttachment(_ context.Context, params assistantattachment.CreateAttachmentParams) (*domain.Attachment, error) {
	s.params = append(s.params, params)
	return &domain.Attachment{
		ID:               params.ID,
		ConversationID:   params.ConversationID,
		UploadedByUserID: params.UploadedByUserID,
		Filename:         params.Filename,
		ContentType:      params.ContentType,
		Category:         params.Category,
		SizeBytes:        params.SizeBytes,
		SHA256:           params.SHA256,
		ObjectKey:        params.ObjectKey,
		Metadata:         params.Metadata,
	}, nil
}

func (s *stubConversationSandboxReader) GetActiveConversationSandbox(_ context.Context, conversationID string) (*domain.ConversationSandbox, error) {
	s.conversationID = conversationID
	if s.err != nil {
		return nil, s.err
	}
	if s.sandbox == nil {
		return nil, domain.ErrNotFound
	}
	return s.sandbox, nil
}

func (s *stubConversationSandboxReader) GetUsableConversationSandbox(ctx context.Context, conversationID string) (*domain.ConversationSandbox, error) {
	return s.GetActiveConversationSandbox(ctx, conversationID)
}

func TestTurnRunnerToolScopeReflectsActiveSandbox(t *testing.T) {
	reader := &stubConversationSandboxReader{
		sandbox: &domain.ConversationSandbox{
			ID:             "sandbox-1",
			ConversationID: "conv-1",
			Status:         domain.SandboxStatusActive,
		},
	}
	runner := &TurnRunner{
		sandboxes:     reader,
		conversations: ownedConversationReader(),
	}

	scope, err := runner.toolScope(context.Background(), "conv-1", "turn-1")
	if err != nil {
		t.Fatalf("tool scope: %v", err)
	}
	if !scope.HasSandbox {
		t.Fatalf("expected active sandbox in scope: %#v", scope)
	}
	if scope.OwnerUserID != "user-1" {
		t.Fatalf("owner user ID = %q, want user-1", scope.OwnerUserID)
	}
}

func TestTurnRunnerContextReadySchedulesWithoutCallingModel(t *testing.T) {
	contextStore := &stubRunnerContextStore{
		head:     &domain.ContextHead{ConversationID: "conv-1", AnchorGeneration: 3, RawTailStartSeq: 1, LastSeq: 1, ActiveContextTokens: 2},
		messages: []domain.Message{{ConversationID: "conv-1", Seq: 1, Role: domain.RoleUser, ContentText: "hello"}},
	}
	cacheStore := cache.New(8, 8)
	model := &stubModelClient{rawRequests: []json.RawMessage{json.RawMessage(`{"model":"gpt-test"}`)}}
	toolArtifacts := &stubToolArtifactStore{}
	turnArtifacts := &stubTurnArtifactStore{}
	runs := &stubScheduledRunStore{}
	profiles := &personalizationReaderStub{
		preferences: &domain.UserPreferences{PreferencesText: "Prefer short answers.", LocationEnabledForModel: true},
		location: &domain.UserLocation{
			Latitude: 30.290846, Longitude: 120.212605, CoordinateSystem: domain.CoordinateSystemGCJ02,
			FormattedAddress: "Hangzhou East Railway Station", POIID: "poi-1", POIName: "Hangzhou East Station",
			Province: "Zhejiang", City: "Hangzhou", District: "Shangcheng", Adcode: "330102",
		},
	}
	runner := &TurnRunner{
		settings:      WorkflowSettings{AgentSystemPrompt: "system"},
		loader:        &ContextLoader{store: contextStore, cache: cacheStore},
		conversations: ownedConversationReader(),
		profiles:      profiles,
		models:        &stubTurnExecutionReader{execution: testExecutionSnapshot()},
		tools:         NewToolOrchestrator(model, &stubToolCatalog{}, nil, nil, toolArtifacts, nil),
		blobs:         turnArtifacts,
		runs:          runs,
	}

	err := runner.HandleContextReady(t.Context(), WorkflowEvent{ConversationID: "conv-1", TurnID: "turn-1"})
	if err != nil {
		t.Fatalf("handle context ready: %v", err)
	}
	if runs.started != 1 {
		t.Fatalf("scheduled run count = %d, want 1", runs.started)
	}
	if len(model.streamRequests) != 0 {
		t.Fatalf("context-ready handler called model %d times", len(model.streamRequests))
	}
	state, err := runner.tools.LoadScheduledRunState(t.Context(), toolArtifacts.TurnRunStateKey("conv-1", "turn-1", 1))
	if err != nil {
		t.Fatalf("load scheduled state: %v", err)
	}
	if state.Request.PromptCacheKey != conversationPromptCacheKey("conv-1") {
		t.Fatalf("prompt cache key = %q", state.Request.PromptCacheKey)
	}
	if state.Scope.OwnerUserID != "user-1" || state.Request.Instructions != "system" {
		t.Fatalf("scheduled state instructions are not pure system instructions: scope=%#v instructions=%q", state.Scope, state.Request.Instructions)
	}
	if len(state.Request.Input) != 2 || !isAccountPersonalizationContext(state.Request.Input[0]) || state.Request.Input[1].Content != "hello" {
		t.Fatalf("scheduled state did not place personalization before the user request: %#v", state.Request.Input)
	}
	for _, expected := range []string{
		`"latitude":30.290846`, `"longitude":120.212605`, `"coordinate_system":"gcj02"`,
		`"formatted_address":"Hangzhou East Railway Station"`, `"poi_name":"Hangzhou East Station"`,
	} {
		if !strings.Contains(state.Request.Input[0].Content, expected) {
			t.Fatalf("scheduled model request is missing %q: %s", expected, state.Request.Input[0].Content)
		}
	}
}

func TestTurnRunnerRetryInputReplacesOriginalUserPrompt(t *testing.T) {
	artifacts := &stubTurnArtifactStore{data: map[string][]byte{
		"requests/conv-1/root-turn.json": []byte(`{"input":[{"type":"message","role":"assistant","content":"context"},{"type":"message","role":"user","content":"original"}]}`),
	}}
	runner := &TurnRunner{
		blobs: artifacts,
		store: &stubTurnWorkflowStore{userMessage: &domain.Message{
			Role: domain.RoleUser, ContentText: "edited prompt", ContextExcluded: true,
		}},
		loader: &ContextLoader{},
	}

	items, err := runner.retryModelInput(t.Context(), "conv-1", "root-turn", "variant-turn")
	if err != nil {
		t.Fatalf("retryModelInput() error = %v", err)
	}
	if len(items) != 2 || items[0].Role != domain.RoleAssistant || items[1].Role != domain.RoleUser {
		t.Fatalf("retry input = %#v", items)
	}
	if items[1].Content != "edited prompt" || strings.Contains(string(items[1].Raw), "original") {
		t.Fatalf("retry prompt was not replaced: %#v", items[1])
	}
}

func TestTurnRunnerRetryInputTruncatesRawToolOutput(t *testing.T) {
	toolOutput := strings.Repeat("x", 1_000)
	request, err := json.Marshal(map[string]any{"input": []any{
		map[string]any{"type": "message", "role": "assistant", "content": "context"},
		map[string]any{"type": llm.ModelItemFunctionCallOutput, "call_id": "call-1", "output": toolOutput},
		map[string]any{"type": "message", "role": "user", "content": "original"},
	}})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	artifacts := &stubTurnArtifactStore{data: map[string][]byte{"requests/conv-1/root-turn.json": request}}
	orchestrator := NewToolOrchestrator(&stubModelClient{}, nil, nil, nil, nil, nil)
	orchestrator.modelToolOutputMaxTokens = 50
	runner := &TurnRunner{
		blobs: artifacts,
		store: &stubTurnWorkflowStore{userMessage: &domain.Message{
			Role: domain.RoleUser, ContentText: "edited prompt", ContextExcluded: true,
		}},
		loader: &ContextLoader{},
		tools:  orchestrator,
	}

	items, err := runner.retryModelInput(t.Context(), "conv-1", "root-turn", "variant-turn")
	if err != nil {
		t.Fatalf("retryModelInput() error = %v", err)
	}
	if len(items) != 3 || !strings.Contains(items[1].Output, "Warning: truncated output") || len(items[1].Raw) != 0 {
		t.Fatalf("retry tool output was not bounded: %#v", items)
	}
}

func TestTurnRunnerHydratesLegacyScheduledRunContextWindow(t *testing.T) {
	execution := testExecutionSnapshot()
	execution.ContextWindowTokens = 128_000
	runner := &TurnRunner{models: &stubTurnExecutionReader{execution: execution}}
	state := &ScheduledRunState{Request: llm.ModelRequest{Model: "gpt-test"}}

	if err := runner.hydrateScheduledRunState(t.Context(), "turn-1", state); err != nil {
		t.Fatalf("hydrate scheduled state: %v", err)
	}
	if state.Request.ContextWindowTokens != 128_000 {
		t.Fatalf("context window = %d, want 128000", state.Request.ContextWindowTokens)
	}
}

func TestTurnRunnerFailedTurnRetryFallsBackToHotContext(t *testing.T) {
	contextStore := &stubRunnerContextStore{
		head: &domain.ContextHead{ConversationID: "conv-1", RawTailStartSeq: 1, LastSeq: 3, ActiveContextTokens: 3},
		messages: []domain.Message{
			{Seq: 1, Role: domain.RoleUser, ContentText: "earlier"},
			{Seq: 2, Role: domain.RoleAssistant, ContentText: "earlier answer"},
			{Seq: 3, Role: domain.RoleUser, ContentText: "failed prompt"},
		},
	}
	store := &stubTurnWorkflowStore{
		turn: &domain.Turn{ID: "root-turn", Status: domain.TurnStatusFailed},
		userMessage: &domain.Message{
			Role: domain.RoleUser, ContentText: "edited failed prompt", ContextExcluded: true,
		},
	}
	runner := &TurnRunner{
		blobs:  &stubTurnArtifactStore{},
		store:  store,
		loader: &ContextLoader{store: contextStore, cache: cache.New(8, 8)},
	}

	items, err := runner.retryModelInput(t.Context(), "conv-1", "root-turn", "variant-turn")
	if err != nil {
		t.Fatalf("retryModelInput() fallback error = %v", err)
	}
	if len(items) != 3 || items[2].Role != domain.RoleUser || items[2].Content != "edited failed prompt" {
		t.Fatalf("failed retry input = %#v", items)
	}
	for _, item := range items[2:] {
		if item.Role == domain.RoleAssistant {
			t.Fatalf("old answer followed edited failed prompt: %#v", items)
		}
	}
}

func TestVariantSourceTurnIDUsesSelectedVariant(t *testing.T) {
	turn := &domain.Turn{
		RetryOfTurnID: "root-turn",
		Metadata:      json.RawMessage(`{"variant_source_turn_id":"selected-variant"}`),
	}
	if got := variantSourceTurnID(turn); got != "selected-variant" {
		t.Fatalf("variantSourceTurnID() = %q, want selected-variant", got)
	}
}

type stubTurnExecutionReader struct {
	execution  *domain.ModelExecutionSnapshot
	resolveErr error
}

func testExecutionSnapshot() *domain.ModelExecutionSnapshot {
	return &domain.ModelExecutionSnapshot{
		ModelID: "model-1", ModelRevision: 1, ModelPriceID: "price-1",
		CredentialID: "credential-1", BaseURL: "https://api.example.com/v1", UpstreamModel: "gpt-test",
		ContextWindowTokens: 1000, MaxOutputTokens: 100,
		PricingSnapshot: json.RawMessage(`{"currency":"USD","input_per_million_nanos":1,"cache_read_input_per_million_nanos":1,"cache_creation_input_per_million_nanos":1,"output_per_million_nanos":1}`),
	}
}

func (s *stubTurnExecutionReader) ResolveExecution(context.Context, string, bool) (*domain.ModelExecutionSnapshot, error) {
	if s.resolveErr != nil {
		return nil, s.resolveErr
	}
	return s.execution, nil
}

func (s *stubTurnExecutionReader) GetTurnExecution(context.Context, string) (*domain.ModelExecutionSnapshot, error) {
	return s.execution, nil
}

func TestTurnRunnerContextReadyUsesSnapshottedReasoningEffort(t *testing.T) {
	contextStore := &stubRunnerContextStore{
		head:     &domain.ContextHead{ConversationID: "conv-1", RawTailStartSeq: 1, LastSeq: 1, ActiveContextTokens: 2},
		messages: []domain.Message{{ConversationID: "conv-1", Seq: 1, Role: domain.RoleUser, ContentText: "hello"}},
	}
	cacheStore := cache.New(8, 8)
	model := &stubModelClient{rawRequests: []json.RawMessage{json.RawMessage(`{"model":"gpt-test"}`)}}
	artifacts := &stubToolArtifactStore{}
	execution := testExecutionSnapshot()
	execution.ReasoningEffort = "high"
	execution.DefaultParameters = json.RawMessage(`{"reasoning_effort":"low"}`)
	runner := &TurnRunner{
		settings:      WorkflowSettings{AgentSystemPrompt: "system"},
		loader:        &ContextLoader{store: contextStore, cache: cacheStore},
		conversations: ownedConversationReader(),
		models:        &stubTurnExecutionReader{execution: execution},
		tools:         NewToolOrchestrator(model, &stubToolCatalog{}, nil, nil, artifacts, nil),
		blobs:         &stubTurnArtifactStore{},
		runs:          &stubScheduledRunStore{},
	}
	if err := runner.HandleContextReady(t.Context(), WorkflowEvent{ConversationID: "conv-1", TurnID: "turn-1"}); err != nil {
		t.Fatalf("handle context ready: %v", err)
	}
	state, err := runner.tools.LoadScheduledRunState(t.Context(), artifacts.TurnRunStateKey("conv-1", "turn-1", 1))
	if err != nil {
		t.Fatalf("load scheduled state: %v", err)
	}
	if state.Request.ReasoningEffort != "high" {
		t.Fatalf("reasoning effort = %q, want high", state.Request.ReasoningEffort)
	}
}

func TestConversationPromptCacheKeyIsStableAcrossAnchorGenerations(t *testing.T) {
	first := conversationPromptCacheKey("conv-1")
	if first == "" || first != conversationPromptCacheKey("conv-1") {
		t.Fatalf("prompt cache key is not stable: %q", first)
	}
	if first == conversationPromptCacheKey("conv-2") {
		t.Fatalf("prompt cache key did not partition conversations: %q", first)
	}
}

func TestTurnRunnerCommitsTerminalFailureWithoutRetryingEvent(t *testing.T) {
	store := &stubTurnWorkflowStore{}
	runner := &TurnRunner{
		store:     store,
		loader:    &ContextLoader{store: &stubRunnerContextStore{err: errors.New("database unavailable")}},
		streamHub: &recordingPublisher{},
	}
	err := runner.HandleContextReady(t.Context(), WorkflowEvent{ConversationID: "conv-1", TurnID: "turn-1"})
	if err != nil {
		t.Fatalf("terminally failed event returned retryable error: %v", err)
	}
	if store.failures != 1 {
		t.Fatalf("failure finalizations = %d, want 1", store.failures)
	}
}

func TestScheduledRunFailureRetriesAtomicRunAndTurnFinalization(t *testing.T) {
	runs := &stubScheduledRunStore{
		run:     &domain.TurnRun{ID: "run-1", TurnID: "turn-1", Status: domain.TurnRunStatusRunning},
		failErr: errors.New("commit failed"),
	}
	runner := &TurnRunner{
		runs: runs, blobs: &stubTurnArtifactStore{}, streamHub: &recordingPublisher{},
	}
	event := WorkflowEvent{ConversationID: "conv-1", TurnID: "turn-1", TurnRunID: "run-1"}
	lease := TurnRunLease{TurnID: "turn-1", RunID: "run-1", Token: "lease-1"}

	if err := runner.failScheduledTurnRun(t.Context(), event, runs.run, lease, nil, errors.New("upstream failed")); err == nil {
		t.Fatal("expected atomic failure commit error")
	}
	if runs.parentFailed != 0 || runs.run.Status != domain.TurnRunStatusRunning {
		t.Fatalf("partial failure escaped rollback: parent=%d run=%q", runs.parentFailed, runs.run.Status)
	}
	runs.failErr = nil
	if err := runner.failScheduledTurnRun(t.Context(), event, runs.run, lease, nil, errors.New("upstream failed")); err != nil {
		t.Fatalf("retry atomic failure: %v", err)
	}
	if runs.parentFailed != 1 || runs.run.Status != domain.TurnRunStatusFailed || runs.failed != 2 {
		t.Fatalf("retry did not converge: parent=%d run=%q attempts=%d", runs.parentFailed, runs.run.Status, runs.failed)
	}
}

func TestTurnRunnerRequestedEventExecutesOneRequestThenReschedules(t *testing.T) {
	model := &stubModelClient{
		rawRequests: []json.RawMessage{json.RawMessage(`{"step":2}`)},
		results: []*llm.ModelResult{{
			ResponseID:  "resp-1",
			OutputItems: []llm.ModelItem{{Type: llm.ModelItemFunctionCall, CallID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{}`)}},
		}},
	}
	executor := &stubToolExecutor{result: &tool.ToolExecutionResult{OutputItem: llm.ModelItem{
		Type: llm.ModelItemFunctionCallOutput, CallID: "call-1", Output: `{"ok":true}`,
	}}}
	artifacts := &stubToolArtifactStore{}
	orchestrator := NewToolOrchestrator(model, &stubToolCatalog{tools: []llm.ModelTool{{Type: llm.ModelToolTypeFunction, Name: "lookup"}}}, executor, &recordingPublisher{}, artifacts, nil)
	state := &ScheduledRunState{
		Version: scheduledRunStateVersion, StepIndex: 1, InitialInputCount: 1,
		Scope: tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
		Request: llm.ModelRequest{
			Model: "gpt-test", ContextWindowTokens: 1_000,
			Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "research"}},
			Tools: []llm.ModelTool{{Type: llm.ModelToolTypeFunction, Name: "lookup"}},
		},
	}
	stateKey, _, err := orchestrator.PersistScheduledRunState(t.Context(), state.Scope, state, json.RawMessage(`{"step":1}`))
	if err != nil {
		t.Fatalf("persist initial state: %v", err)
	}
	runs := &stubScheduledRunStore{run: &domain.TurnRun{
		ID: "run-1", TurnID: "turn-1", StepIndex: 1, Status: domain.TurnRunStatusQueued, StateBlobKey: stateKey,
	}}
	runner := &TurnRunner{
		settings: WorkflowSettings{WorkerLeaseTimeout: time.Hour},
		tools:    orchestrator, runs: runs, streamHub: &recordingPublisher{}, blobs: &stubTurnArtifactStore{},
	}

	err = runner.HandleTurnRunRequested(t.Context(), WorkflowEvent{
		EventType: EventTurnRunRequested, ConversationID: "conv-1", TurnID: "turn-1", TurnRunID: "run-1",
	})
	if err != nil {
		t.Fatalf("handle requested run: %v", err)
	}
	if len(model.streamRequests) != 1 {
		t.Fatalf("model request count = %d, want 1", len(model.streamRequests))
	}
	if runs.completed != 1 || runs.scheduled != 1 || runs.checkpoints != 2 {
		t.Fatalf("completed=%d scheduled=%d checkpoints=%d, want 1, 1, 2", runs.completed, runs.scheduled, runs.checkpoints)
	}
}

func TestTurnRunnerResumesCheckpointWithoutCallingModel(t *testing.T) {
	model := &stubModelClient{}
	artifacts := &stubToolArtifactStore{}
	orchestrator := NewToolOrchestrator(model, &stubToolCatalog{}, nil, nil, artifacts, nil)
	outcome := &ScheduledRunOutcome{
		Model: &llm.ModelResult{ResponseID: "resp-1"}, Postprocessed: true,
		NextState: &ScheduledRunState{
			Version: scheduledRunStateVersion, StepIndex: 2, InitialInputCount: 1,
			Scope:   tool.ToolScope{ConversationID: "conv-1", TurnID: "turn-1"},
			Request: llm.ModelRequest{Model: "gpt-test", Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "hello"}}},
		},
		NextRequest: json.RawMessage(`{"step":2}`),
	}
	resultKey := artifacts.TurnRunResultKey("conv-1", "turn-1", 1)
	payload, err := json.Marshal(outcome)
	if err != nil {
		t.Fatalf("marshal checkpoint: %v", err)
	}
	if err := artifacts.PutBytes(t.Context(), resultKey, payload, "application/json"); err != nil {
		t.Fatalf("persist checkpoint: %v", err)
	}
	runs := &stubScheduledRunStore{run: &domain.TurnRun{
		ID: "run-1", TurnID: "turn-1", StepIndex: 1, Status: domain.TurnRunStatusQueued, ResultBlobKey: resultKey,
	}}
	runner := &TurnRunner{
		settings: WorkflowSettings{WorkerLeaseTimeout: time.Hour},
		tools:    orchestrator, runs: runs, streamHub: &recordingPublisher{}, blobs: &stubTurnArtifactStore{},
	}
	if err := runner.HandleTurnRunRequested(t.Context(), WorkflowEvent{
		EventType: EventTurnRunRequested, ConversationID: "conv-1", TurnID: "turn-1", TurnRunID: "run-1",
	}); err != nil {
		t.Fatalf("resume checkpointed run: %v", err)
	}
	if len(model.streamRequests) != 0 {
		t.Fatalf("checkpoint recovery called model %d times", len(model.streamRequests))
	}
	if runs.completed != 1 || runs.scheduled != 1 {
		t.Fatalf("completed=%d scheduled=%d, want 1 each", runs.completed, runs.scheduled)
	}
}

func TestTurnRunnerToolScopeHandlesMissingSandbox(t *testing.T) {
	runner := &TurnRunner{
		sandboxes:     &stubConversationSandboxReader{},
		conversations: ownedConversationReader(),
	}

	scope, err := runner.toolScope(context.Background(), "conv-1", "turn-1")
	if err != nil {
		t.Fatalf("tool scope: %v", err)
	}
	if scope.HasSandbox {
		t.Fatalf("expected no sandbox in scope: %#v", scope)
	}
}

func TestClassifyInitialToolRunFailureTreatsMissingModelResultAsPrepareFailure(t *testing.T) {
	code, message := classifyInitialToolRunFailure(errors.New("marshal failed"), nil)
	if code != domain.TurnErrorRequestPrepareFailed {
		t.Fatalf("code = %q, want %q", code, domain.TurnErrorRequestPrepareFailed)
	}
	if message != domain.TurnPublicErrorRequestProcessing {
		t.Fatalf("message = %q, want %q", message, domain.TurnPublicErrorRequestProcessing)
	}
}

func TestClassifyInitialToolRunFailureSanitizesUpstreamErrors(t *testing.T) {
	code, message := classifyInitialToolRunFailure(
		fmt.Errorf("%w: provider secret", llm.ErrUpstreamRequestFailed),
		&llm.ModelResult{},
	)
	if code != domain.TurnErrorUpstreamRequestFailed {
		t.Fatalf("code = %q, want %q", code, domain.TurnErrorUpstreamRequestFailed)
	}
	if message != domain.TurnPublicErrorUpstreamRequestFailed {
		t.Fatalf("message = %q, want %q", message, domain.TurnPublicErrorUpstreamRequestFailed)
	}
}

func TestClassifyInitialToolRunFailureUsesBackendCategoryForInternalErrors(t *testing.T) {
	code, message := classifyInitialToolRunFailure(errors.New("tool execution failed"), &llm.ModelResult{})
	if code != domain.TurnErrorBackendRequestFailed {
		t.Fatalf("code = %q, want %q", code, domain.TurnErrorBackendRequestFailed)
	}
	if message != domain.TurnPublicErrorRequestProcessing {
		t.Fatalf("message = %q, want %q", message, domain.TurnPublicErrorRequestProcessing)
	}
}

func TestTurnRunnerPersistsTurnModelContext(t *testing.T) {
	store := &stubTurnArtifactStore{}
	runner := &TurnRunner{
		blobs: store,
	}

	err := runner.persistTurnModelContext(context.Background(), &domain.Turn{
		ID:             "turn-1",
		ConversationID: "conv-1",
	}, &ToolRunResult{
		ContextItems: []llm.ModelItem{
			{
				Type: llm.ModelItemReasoning,
				Raw:  json.RawMessage(`{"type":"reasoning","encrypted_content":"ciphertext"}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("persist model context: %v", err)
	}

	data := string(store.data["turn-model-context/conv-1/turn-1.json"])
	if !strings.Contains(data, "encrypted_content") || !strings.Contains(data, "ciphertext") {
		t.Fatalf("expected encrypted reasoning to be stored in model context artifact, got %q", data)
	}
}

func TestTurnRunnerGeneratedImageDraftsPersistAttachments(t *testing.T) {
	imageData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Z4sUAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode image fixture: %v", err)
	}
	blobs := &stubTurnArtifactStore{}
	attachments := &stubGeneratedAttachmentStore{}
	runner := &TurnRunner{
		blobs:                blobs,
		generatedAttachments: attachments,
		conversations: &stubWorkflowConversationReader{
			conversation: &domain.Conversation{ID: "conv-1", OwnerUserID: "user-1"},
		},
	}

	drafts, err := runner.generatedImageDrafts(context.Background(), &domain.Turn{ID: "turn-1", ConversationID: "conv-1"}, &llm.ModelResult{
		ResponseID: "resp-1",
		OutputItems: []llm.ModelItem{
			{
				ID:            "ig_1",
				Type:          llm.ModelItemImageGenerationCall,
				Status:        "generating",
				RevisedPrompt: "A red circle.",
				Result:        base64.StdEncoding.EncodeToString(imageData),
			},
		},
	})
	if err != nil {
		t.Fatalf("generated image drafts: %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("draft count = %d, want 1", len(drafts))
	}
	if len(attachments.params) != 1 {
		t.Fatalf("attachment count = %d, want 1", len(attachments.params))
	}
	params := attachments.params[0]
	if params.ConversationID != "conv-1" || params.UploadedByUserID != "user-1" || params.ContentType != "image/png" || params.Category != domain.AttachmentCategoryImage {
		t.Fatalf("unexpected attachment params: %#v", params)
	}
	if !strings.Contains(params.ObjectKey, "generated-images/conv-1/turn-1/ig_1.png") {
		t.Fatalf("unexpected object key: %q", params.ObjectKey)
	}
	if string(blobs.data[params.ObjectKey]) != string(imageData) {
		t.Fatalf("stored image bytes mismatch")
	}
	var attachmentMetadata struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	if err := json.Unmarshal(params.Metadata, &attachmentMetadata); err != nil {
		t.Fatalf("decode attachment metadata: %v", err)
	}
	if attachmentMetadata.Width != 1 || attachmentMetadata.Height != 1 {
		t.Fatalf("attachment dimensions = %dx%d, want 1x1", attachmentMetadata.Width, attachmentMetadata.Height)
	}
	if !strings.Contains(string(drafts[0].Metadata), `"display_kind":"assistant_image"`) || !strings.Contains(string(drafts[0].Metadata), `"attachment_ids"`) {
		t.Fatalf("unexpected draft metadata: %s", drafts[0].Metadata)
	}
	if !strings.Contains(string(drafts[0].Metadata), `"width":1`) || !strings.Contains(string(drafts[0].Metadata), `"height":1`) {
		t.Fatalf("draft metadata is missing image dimensions: %s", drafts[0].Metadata)
	}
}

var _ tool.ConversationSandboxReader = (*stubConversationSandboxReader)(nil)
