package workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/EurekaMXZ/assistant/internal/cache"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
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

func (s *stubRunnerContextStore) ListRawTailMessages(context.Context, string, int64, int64) ([]domain.Message, error) {
	return append([]domain.Message(nil), s.messages...), nil
}

func (s *stubRunnerContextStore) CompleteCompaction(context.Context, string, domain.AnchorObject, int64) (*domain.ContextHead, error) {
	return s.head, nil
}

type stubScheduledRunStore struct {
	started      int
	completed    int
	scheduled    int
	checkpoints  int
	runID        string
	run          *domain.TurnRun
	failErr      error
	failed       int
	parentFailed int
}

type stubTurnWorkflowStore struct {
	failures int
}

func (s *stubTurnWorkflowStore) MarkTurnContextReady(context.Context, string) (*domain.Turn, error) {
	return nil, nil
}

func (s *stubTurnWorkflowStore) FinalizeTurnSuccess(context.Context, string, []domain.AssistantMessageDraft, domain.TurnRunSummary, int) (*domain.Turn, []domain.Message, *domain.ContextHead, bool, error) {
	return nil, nil, nil, false, nil
}

func (s *stubTurnWorkflowStore) FinalizeTurnFailure(context.Context, string, string, string, string, string) error {
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

func (s *stubScheduledRunStore) CompleteScheduledTurnRun(context.Context, TurnRunLease, string, string, string, llm.ModelUsage) (*domain.TurnRun, error) {
	s.completed++
	if s.run != nil {
		s.run.Status = domain.TurnRunStatusCompleted
	}
	return s.run, nil
}

func (s *stubScheduledRunStore) FailScheduledTurnRun(context.Context, TurnRunLease, string, string, string, string, string, string, string, string) (*domain.TurnRun, error) {
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

func TestTurnRunnerToolScopeReflectsActiveSandbox(t *testing.T) {
	reader := &stubConversationSandboxReader{
		sandbox: &domain.ConversationSandbox{
			ID:             "sandbox-1",
			ConversationID: "conv-1",
			Status:         domain.SandboxStatusActive,
		},
	}
	runner := &TurnRunner{
		sandboxes: reader,
	}

	scope, err := runner.toolScope(context.Background(), "conv-1", "turn-1")
	if err != nil {
		t.Fatalf("tool scope: %v", err)
	}
	if !scope.HasSandbox {
		t.Fatalf("expected active sandbox in scope: %#v", scope)
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
	runner := &TurnRunner{
		settings: WorkflowSettings{AgentSystemPrompt: "system"},
		loader:   &ContextLoader{store: contextStore, cache: cacheStore},
		models:   &stubTurnExecutionReader{execution: testExecutionSnapshot()},
		tools:    NewToolOrchestrator(model, &stubToolCatalog{}, nil, nil, toolArtifacts, nil),
		blobs:    turnArtifacts,
		runs:     runs,
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
}

type stubTurnExecutionReader struct {
	execution *domain.ModelExecutionSnapshot
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
		settings: WorkflowSettings{AgentSystemPrompt: "system"},
		loader:   &ContextLoader{store: contextStore, cache: cacheStore},
		models:   &stubTurnExecutionReader{execution: execution},
		tools:    NewToolOrchestrator(model, &stubToolCatalog{}, nil, nil, artifacts, nil),
		blobs:    &stubTurnArtifactStore{},
		runs:     &stubScheduledRunStore{},
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
			Model: "gpt-test", Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "research"}},
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
		sandboxes: &stubConversationSandboxReader{},
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
	imageData := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
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
	if !strings.Contains(string(drafts[0].Metadata), `"display_kind":"assistant_image"`) || !strings.Contains(string(drafts[0].Metadata), `"attachment_ids"`) {
		t.Fatalf("unexpected draft metadata: %s", drafts[0].Metadata)
	}
}

var _ tool.ConversationSandboxReader = (*stubConversationSandboxReader)(nil)
