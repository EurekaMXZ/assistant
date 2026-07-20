package tool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/EurekaMXZ/assistant/internal/tavily"
)

func mustLocalExecutor(t *testing.T, handlers ...LocalToolHandler) LocalExecutor {
	t.Helper()

	executor, err := NewLocalExecutor(handlers...)
	if err != nil {
		t.Fatalf("new local executor: %v", err)
	}

	return executor
}

type stubConversationTitleUpdater struct {
	conversationID string
	title          string
	result         *domain.Conversation
	err            error
}

func (s *stubConversationTitleUpdater) UpdateConversationTitle(_ context.Context, conversationID string, title string) (*domain.Conversation, error) {
	s.conversationID = conversationID
	s.title = title
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

type stubConversationSandboxStore struct {
	conversationID      string
	created             *domain.ConversationSandbox
	destroyed           *domain.ConversationSandbox
	active              *domain.ConversationSandbox
	err                 error
	createErr           error
	activeOnCreateError *domain.ConversationSandbox
	destroyErr          error
}

func (s *stubConversationSandboxStore) GetLatestConversationSandbox(_ context.Context, conversationID string) (*domain.ConversationSandbox, error) {
	s.conversationID = conversationID
	if s.active != nil {
		return s.active, nil
	}
	if s.destroyed != nil {
		return s.destroyed, nil
	}
	return nil, domain.ErrNotFound
}

func (s *stubConversationSandboxStore) GetActiveConversationSandbox(_ context.Context, conversationID string) (*domain.ConversationSandbox, error) {
	s.conversationID = conversationID
	if s.err != nil {
		return nil, s.err
	}
	if s.active == nil || s.active.Status != domain.SandboxStatusActive {
		return nil, domain.ErrNotFound
	}
	return s.active, nil
}

func (s *stubConversationSandboxStore) GetUsableConversationSandbox(ctx context.Context, conversationID string) (*domain.ConversationSandbox, error) {
	s.conversationID = conversationID
	if s.err != nil {
		return nil, s.err
	}
	if s.active == nil || (s.active.Status != domain.SandboxStatusActive && s.active.Status != domain.SandboxStatusStopped) {
		return nil, domain.ErrNotFound
	}
	return s.active, nil
}

func (s *stubConversationSandboxStore) CreateConversationSandbox(_ context.Context, conversationID string, provider string, runtimeID string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	s.conversationID = conversationID
	if s.createErr != nil {
		if s.activeOnCreateError != nil {
			s.active = s.activeOnCreateError
		}
		return nil, s.createErr
	}
	s.created = &domain.ConversationSandbox{
		ID:              "sandbox-1",
		ConversationID:  conversationID,
		Provider:        provider,
		RuntimeID:       runtimeID,
		Status:          domain.SandboxStatusActive,
		RuntimeMetadata: metadata,
	}
	s.active = s.created
	return s.created, nil
}

func (s *stubConversationSandboxStore) StopConversationSandbox(_ context.Context, sandboxID string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	if s.active == nil {
		return nil, domain.ErrConflict
	}
	now := time.Now()
	s.active.Status = domain.SandboxStatusStopped
	s.active.StoppedAt = &now
	s.active.RuntimeMetadata = metadata
	return s.active, nil
}

func (s *stubConversationSandboxStore) ResumeConversationSandbox(_ context.Context, sandboxID string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	if s.active == nil {
		return nil, domain.ErrConflict
	}
	s.active.Status = domain.SandboxStatusActive
	s.active.StoppedAt = nil
	s.active.RuntimeMetadata = metadata
	s.active.LastActivityAt = time.Now()
	return s.active, nil
}

func (s *stubConversationSandboxStore) TouchConversationSandbox(_ context.Context, sandboxID string) error {
	if s.active == nil {
		return domain.ErrConflict
	}
	s.active.LastActivityAt = time.Now()
	return nil
}

func (s *stubConversationSandboxStore) AcquireConversationSandboxExecution(_ context.Context, sandboxID string, token string, leaseDuration time.Duration) error {
	if s.active == nil || s.active.ID != sandboxID || s.active.Status != domain.SandboxStatusActive {
		return domain.ErrConflict
	}
	leaseUntil := time.Now().Add(leaseDuration)
	s.active.ExecutionToken = token
	s.active.ExecutionLeaseUntil = &leaseUntil
	s.active.LastActivityAt = time.Now()
	return nil
}

func (s *stubConversationSandboxStore) CompleteConversationSandboxExecution(_ context.Context, sandboxID string, token string) error {
	if s.active == nil || s.active.ID != sandboxID || s.active.ExecutionToken != token {
		return domain.ErrConflict
	}
	s.active.ExecutionToken = ""
	s.active.ExecutionLeaseUntil = nil
	s.active.LastActivityAt = time.Now()
	return nil
}

func (s *stubConversationSandboxStore) RenewConversationSandboxExecution(_ context.Context, sandboxID string, token string, leaseDuration time.Duration) error {
	if s.active == nil || s.active.ID != sandboxID || s.active.ExecutionToken != token {
		return domain.ErrConflict
	}
	leaseUntil := time.Now().Add(leaseDuration)
	s.active.ExecutionLeaseUntil = &leaseUntil
	return nil
}

func (s *stubConversationSandboxStore) ListIdleConversationSandboxes(context.Context, time.Time, int) ([]*domain.ConversationSandbox, error) {
	return nil, nil
}

func (s *stubConversationSandboxStore) ListStoppedConversationSandboxes(context.Context, time.Time, int) ([]*domain.ConversationSandbox, error) {
	return nil, nil
}

func (s *stubConversationSandboxStore) ListReleasingConversationSandboxes(context.Context, int) ([]*domain.ConversationSandbox, error) {
	return nil, nil
}

func (s *stubConversationSandboxStore) BeginConversationSandboxRelease(_ context.Context, sandboxID string) (*domain.ConversationSandbox, error) {
	if s.destroyErr != nil {
		return nil, s.destroyErr
	}
	if s.active == nil || s.active.ID != sandboxID {
		return nil, domain.ErrConflict
	}
	s.active.ReleasePreviousStatus = s.active.Status
	s.active.Status = domain.SandboxStatusReleasing
	return s.active, nil
}

func (s *stubConversationSandboxStore) ClaimConversationSandboxRelease(_ context.Context, sandboxID string, token string, leaseDuration time.Duration) (*domain.ConversationSandbox, error) {
	if s.active == nil || s.active.ID != sandboxID || s.active.Status != domain.SandboxStatusReleasing || s.active.ReleaseToken != "" {
		return nil, domain.ErrConflict
	}
	leaseUntil := time.Now().Add(leaseDuration)
	s.active.ReleaseToken = token
	s.active.ReleaseLeaseUntil = &leaseUntil
	return s.active, nil
}

func (s *stubConversationSandboxStore) RenewConversationSandboxReleaseClaim(_ context.Context, sandboxID string, token string, leaseDuration time.Duration) error {
	if s.active == nil || s.active.ID != sandboxID || s.active.ReleaseToken != token {
		return domain.ErrConflict
	}
	leaseUntil := time.Now().Add(leaseDuration)
	s.active.ReleaseLeaseUntil = &leaseUntil
	return nil
}

func (s *stubConversationSandboxStore) CompleteConversationSandboxRelease(_ context.Context, sandboxID string, token string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	if s.active == nil || s.active.ID != sandboxID || s.active.Status != domain.SandboxStatusReleasing || s.active.ReleaseToken != token {
		return nil, domain.ErrConflict
	}
	s.destroyed = &domain.ConversationSandbox{
		ID:              sandboxID,
		ConversationID:  s.active.ConversationID,
		Provider:        s.active.Provider,
		RuntimeID:       s.active.RuntimeID,
		Status:          domain.SandboxStatusDestroyed,
		RuntimeMetadata: metadata,
	}
	s.active = nil
	return s.destroyed, nil
}

type stubSandboxManager struct {
	createdConversationID string
	destroyedHandle       domain.SandboxHandle
	execHandle            domain.SandboxHandle
	execRequest           domain.SandboxCommandRequest
	createResult          *domain.SandboxHandle
	destroyResult         *domain.SandboxHandle
	execResult            *domain.SandboxCommandResult
	err                   error
	createErr             error
	destroyErr            error
	createCalls           int
	destroyCalls          int
	stopCalls             int
	resumeCalls           int
	destroyContextErr     error
	requestKeys           []string
	writtenHandle         domain.SandboxHandle
	writtenPath           string
	writtenData           []byte
}

type stubSandboxAttachmentStore struct {
	conversationID string
	attachment     domain.Attachment
	err            error
}

func (s *stubSandboxAttachmentStore) ListAttachmentsByIDs(_ context.Context, conversationID string, ids []string) ([]domain.Attachment, error) {
	if s.err != nil {
		return nil, s.err
	}
	if conversationID != s.conversationID || len(ids) != 1 || ids[0] != s.attachment.ID {
		return nil, domain.ErrNotFound
	}
	return []domain.Attachment{s.attachment}, nil
}

type stubSandboxAttachmentBlobs struct {
	data []byte
	err  error
}

func (s *stubSandboxAttachmentBlobs) OpenReader(context.Context, string) (io.ReadCloser, error) {
	if s.err != nil {
		return nil, s.err
	}
	return io.NopCloser(bytes.NewReader(s.data)), nil
}

func (s *stubSandboxManager) CreateSandbox(_ context.Context, conversationID string, requestKey string) (*domain.SandboxHandle, error) {
	s.createCalls++
	s.requestKeys = append(s.requestKeys, requestKey)
	s.createdConversationID = conversationID
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.createResult, nil
}

func (s *stubSandboxManager) DestroySandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error) {
	s.destroyCalls++
	s.destroyContextErr = ctx.Err()
	s.requestKeys = append(s.requestKeys, requestKey)
	s.destroyedHandle = handle
	if s.destroyErr != nil {
		return nil, s.destroyErr
	}
	return s.destroyResult, nil
}

func (s *stubSandboxManager) StopSandbox(_ context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error) {
	s.stopCalls++
	s.requestKeys = append(s.requestKeys, requestKey)
	return &handle, nil
}

func (s *stubSandboxManager) ResumeSandbox(_ context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error) {
	s.resumeCalls++
	s.requestKeys = append(s.requestKeys, requestKey)
	return &handle, nil
}

func (s *stubSandboxManager) WriteSandboxFile(_ context.Context, handle domain.SandboxHandle, path string, reader io.Reader, _ int64, requestKey string) error {
	s.requestKeys = append(s.requestKeys, requestKey)
	s.writtenHandle = handle
	s.writtenPath = path
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.writtenData = append([]byte(nil), data...)
	return s.err
}

func TestCreateSandboxResumesStoppedSandbox(t *testing.T) {
	now := time.Now()
	store := &stubConversationSandboxStore{active: &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "local", RuntimeID: "runtime-1",
		Status: domain.SandboxStatusStopped, StoppedAt: &now,
	}}
	runtime := &stubSandboxManager{}

	result, err := (CreateSandbox{Sandboxes: store, Runtime: runtime}).Execute(t.Context(), CreateSandboxInput{ConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	if result.Status != domain.SandboxStatusActive || runtime.resumeCalls != 1 || runtime.createCalls != 0 {
		t.Fatalf("stopped sandbox was not resumed: result=%#v runtime=%#v", result, runtime)
	}
}

func TestExecSandboxCommandResumesStoppedSandboxAndEnforcesTimeout(t *testing.T) {
	now := time.Now()
	store := &stubConversationSandboxStore{active: &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "local", RuntimeID: "runtime-1",
		Status: domain.SandboxStatusStopped, StoppedAt: &now,
	}}
	runtime := &stubSandboxManager{execResult: &domain.SandboxCommandResult{Output: "ok"}}
	useCase := ExecSandboxCommand{
		Sandboxes: store, Runtime: runtime, DefaultTimeout: 20 * time.Second, MaximumTimeout: 40 * time.Second,
	}

	if _, err := useCase.Execute(t.Context(), ExecSandboxCommandInput{ConversationID: "conv-1", Command: "true"}); err != nil {
		t.Fatalf("exec sandbox command: %v", err)
	}
	if runtime.resumeCalls != 1 || runtime.execRequest.TimeoutSeconds != 20 {
		t.Fatalf("unexpected resume/default timeout: runtime=%#v", runtime)
	}
	if _, err := useCase.Execute(t.Context(), ExecSandboxCommandInput{ConversationID: "conv-1", Command: "sleep", TimeoutSeconds: 41}); err == nil {
		t.Fatal("expected command timeout validation error")
	}
}

func TestImportSandboxAttachmentAuthorizesVerifiesAndWritesFile(t *testing.T) {
	const attachmentID = "11111111-1111-4111-8111-111111111111"
	data := []byte("a,b\n1,2\n")
	digest := sha256.Sum256(data)
	attachments := &stubSandboxAttachmentStore{
		conversationID: "conv-1",
		attachment: domain.Attachment{
			ID:             attachmentID,
			ConversationID: "conv-1",
			Filename:       "report.CSV",
			ContentType:    "text/csv",
			SizeBytes:      int64(len(data)),
			SHA256:         hex.EncodeToString(digest[:]),
			ObjectKey:      "attachments/conv-1/attachment/report.CSV",
		},
	}
	store := &stubConversationSandboxStore{active: &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "cubesandbox", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
	}}
	runtime := &stubSandboxManager{}
	result, err := (ImportSandboxAttachment{
		Attachments: attachments,
		Blobs:       &stubSandboxAttachmentBlobs{data: data},
		Sandboxes:   store,
		Runtime:     runtime,
	}).Execute(t.Context(), ImportSandboxAttachmentInput{
		ConversationID: "conv-1",
		AttachmentID:   attachmentID,
		RequestKey:     "run-1:call-1",
	})
	if err != nil {
		t.Fatalf("import attachment: %v", err)
	}
	wantPath := "/workspace/attachment-" + attachmentID + ".csv"
	if result.SandboxPath != wantPath || runtime.writtenPath != wantPath+".partial" || runtime.execRequest.Command != "mv" || string(runtime.writtenData) != string(data) {
		t.Fatalf("unexpected import: result=%#v runtime=%#v", result, runtime)
	}
	if runtime.writtenHandle.RuntimeID != "runtime-1" || store.active.ExecutionToken != "" || store.active.ExecutionLeaseUntil != nil {
		t.Fatalf("sandbox execution lease was not completed: store=%#v runtime=%#v", store.active, runtime)
	}
}

func TestImportSandboxAttachmentRejectsCrossConversationAndChecksumMismatch(t *testing.T) {
	const attachmentID = "11111111-1111-4111-8111-111111111111"
	attachment := domain.Attachment{ID: attachmentID, ConversationID: "conv-1", Filename: "data.bin", SizeBytes: 3, SHA256: strings.Repeat("0", 64), ObjectKey: "object"}
	useCase := ImportSandboxAttachment{
		Attachments: &stubSandboxAttachmentStore{conversationID: "conv-1", attachment: attachment},
		Blobs:       &stubSandboxAttachmentBlobs{data: []byte("abc")},
		Sandboxes: &stubConversationSandboxStore{active: &domain.ConversationSandbox{
			ID: "sandbox-1", ConversationID: "conv-1", Provider: "cubesandbox", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
		}},
		Runtime: &stubSandboxManager{},
	}
	if _, err := useCase.Execute(t.Context(), ImportSandboxAttachmentInput{ConversationID: "conv-2", AttachmentID: attachmentID}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-conversation error = %v, want not found", err)
	}
	if _, err := useCase.Execute(t.Context(), ImportSandboxAttachmentInput{ConversationID: "conv-1", AttachmentID: attachmentID}); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("checksum error = %v", err)
	}
}

func (s *stubSandboxManager) ExecSandboxCommand(_ context.Context, handle domain.SandboxHandle, request domain.SandboxCommandRequest, requestKey string) (*domain.SandboxCommandResult, error) {
	s.requestKeys = append(s.requestKeys, requestKey)
	s.execHandle = handle
	s.execRequest = request
	if s.err != nil {
		return nil, s.err
	}
	if request.Command == "mv" {
		return &domain.SandboxCommandResult{RuntimeID: handle.RuntimeID, ExitCode: 0}, nil
	}
	return s.execResult, nil
}

type stubWebSearcher struct {
	searchRequest  tavily.SearchRequest
	extractRequest tavily.ExtractRequest
	searchResult   *tavily.SearchResponse
	rawResult      json.RawMessage
	err            error
}

func (s *stubWebSearcher) Search(_ context.Context, request tavily.SearchRequest) (*tavily.SearchResponse, error) {
	s.searchRequest = request
	if s.err != nil {
		return nil, s.err
	}
	return s.searchResult, nil
}

func (s *stubWebSearcher) Extract(_ context.Context, request tavily.ExtractRequest) (json.RawMessage, error) {
	s.extractRequest = request
	if s.err != nil {
		return nil, s.err
	}
	return s.rawResult, nil
}

func TestLocalExecutorRenameConversationTitle(t *testing.T) {
	updater := &stubConversationTitleUpdater{
		result: &domain.Conversation{
			ID:    "conv-1",
			Title: "Renamed",
		},
	}
	executor := mustLocalExecutor(t, RenameConversationTitleHandler{
		UseCase: RenameConversationTitle{
			Conversations: updater,
		},
	})

	result, err := executor.Execute(context.Background(), ToolScope{
		ConversationID: "conv-1",
		TurnID:         "turn-1",
	}, ToolCall{
		Name:      ConversationRenameTitle,
		CallID:    "call-1",
		Arguments: json.RawMessage(`{"title":" Renamed "}`),
	})
	if err != nil {
		t.Fatalf("execute rename title tool: %v", err)
	}

	if updater.conversationID != "conv-1" || updater.title != "Renamed" {
		t.Fatalf("unexpected updater input: conversation=%q title=%q", updater.conversationID, updater.title)
	}
	if result == nil || result.OutputItem.Type != llm.ModelItemFunctionCallOutput || result.OutputItem.CallID != "call-1" {
		t.Fatalf("unexpected tool output item: %#v", result)
	}
	if len(result.StreamEvents) != 1 || result.StreamEvents[0].Type != stream.EventConversationUpdated {
		t.Fatalf("unexpected stream events: %#v", result.StreamEvents)
	}
	if result.StreamEvents[0].ToolName != ConversationRenameTitle {
		t.Fatalf("unexpected tool name in stream event: %#v", result.StreamEvents[0])
	}
}

func TestLocalExecutorCreateSandbox(t *testing.T) {
	store := &stubConversationSandboxStore{}
	runtime := &stubSandboxManager{
		createResult: &domain.SandboxHandle{
			Provider:  "local",
			RuntimeID: "runtime-1",
			Metadata:  json.RawMessage(`{"kind":"logical"}`),
		},
	}
	executor := mustLocalExecutor(t, CreateSandboxHandler{
		UseCase: CreateSandbox{
			Sandboxes: store,
			Runtime:   runtime,
		},
	})

	result, err := executor.Execute(context.Background(), ToolScope{
		ConversationID: "conv-1",
		TurnID:         "turn-1",
	}, ToolCall{
		Name:   SandboxCreate,
		CallID: "call-4",
	})
	if err != nil {
		t.Fatalf("execute create sandbox tool: %v", err)
	}

	if runtime.createdConversationID != "conv-1" {
		t.Fatalf("unexpected sandbox create conversation id: %q", runtime.createdConversationID)
	}
	if result == nil || result.OutputItem.CallID != "call-4" {
		t.Fatalf("unexpected sandbox tool output: %#v", result)
	}
	if len(result.StreamEvents) != 1 || result.StreamEvents[0].Type != stream.EventSandboxUpdated || result.StreamEvents[0].ToolName != SandboxCreate {
		t.Fatalf("unexpected sandbox stream events: %#v", result.StreamEvents)
	}
}

func TestLocalExecutorDestroySandbox(t *testing.T) {
	store := &stubConversationSandboxStore{
		active: &domain.ConversationSandbox{
			ID:              "sandbox-1",
			ConversationID:  "conv-1",
			Provider:        "local",
			RuntimeID:       "runtime-1",
			Status:          domain.SandboxStatusActive,
			RuntimeMetadata: json.RawMessage(`{"kind":"logical"}`),
		},
	}
	runtime := &stubSandboxManager{
		destroyResult: &domain.SandboxHandle{
			Provider:  "local",
			RuntimeID: "runtime-1",
			Metadata:  json.RawMessage(`{"kind":"logical","destroyed_at":"now"}`),
		},
	}
	executor := mustLocalExecutor(t, DestroySandboxHandler{
		UseCase: DestroySandbox{
			Sandboxes: store,
			Runtime:   runtime,
		},
	})

	result, err := executor.Execute(context.Background(), ToolScope{
		ConversationID: "conv-1",
		TurnID:         "turn-1",
	}, ToolCall{
		Name:   SandboxDestroy,
		CallID: "call-5",
	})
	if err != nil {
		t.Fatalf("execute destroy sandbox tool: %v", err)
	}

	if runtime.destroyedHandle.RuntimeID != "runtime-1" {
		t.Fatalf("unexpected destroy handle: %#v", runtime.destroyedHandle)
	}
	if result == nil || result.OutputItem.CallID != "call-5" {
		t.Fatalf("unexpected sandbox destroy output: %#v", result)
	}
	if len(result.StreamEvents) != 1 || result.StreamEvents[0].Type != stream.EventSandboxUpdated || result.StreamEvents[0].ToolName != SandboxDestroy {
		t.Fatalf("unexpected sandbox destroy stream events: %#v", result.StreamEvents)
	}
}

func TestCreateSandboxCompensatesRuntimeWhenDatabaseCreateFails(t *testing.T) {
	store := &stubConversationSandboxStore{createErr: errors.New("database unavailable")}
	runtime := &stubSandboxManager{createResult: &domain.SandboxHandle{Provider: "local", RuntimeID: "runtime-1"}}
	useCase := CreateSandbox{Sandboxes: store, Runtime: runtime}

	if _, err := useCase.Execute(t.Context(), CreateSandboxInput{ConversationID: "conv-1", RequestKey: "run-1:call-1"}); err == nil {
		t.Fatal("expected database create failure")
	}
	if runtime.createCalls != 1 || runtime.destroyCalls != 1 {
		t.Fatalf("runtime calls create=%d destroy=%d, want one compensation", runtime.createCalls, runtime.destroyCalls)
	}
}

func TestCreateSandboxDoesNotDestroyReusedRuntimeOnDatabaseConflict(t *testing.T) {
	existing := &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "firecracker", RuntimeID: "vm-1", Status: domain.SandboxStatusActive,
	}
	store := &stubConversationSandboxStore{
		createErr:           domain.ErrConflict,
		activeOnCreateError: existing,
	}
	runtime := &stubSandboxManager{createResult: &domain.SandboxHandle{Provider: "firecracker", RuntimeID: "vm-1", Reused: true}}

	result, err := (CreateSandbox{Sandboxes: store, Runtime: runtime}).Execute(t.Context(), CreateSandboxInput{ConversationID: "conv-1", RequestKey: "run-1:call-1"})
	if err != nil || result.ID != "sandbox-1" {
		t.Fatalf("create reused sandbox result=%#v err=%v", result, err)
	}
	if runtime.destroyCalls != 0 {
		t.Fatalf("reused runtime was destroyed %d times", runtime.destroyCalls)
	}
}

func TestCreateSandboxReturnsCommittedRuntimeAfterAmbiguousDatabaseError(t *testing.T) {
	existing := &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "firecracker", RuntimeID: "vm-1", Status: domain.SandboxStatusActive,
	}
	store := &stubConversationSandboxStore{
		createErr:           errors.New("database connection reset"),
		activeOnCreateError: existing,
	}
	runtime := &stubSandboxManager{createResult: &domain.SandboxHandle{Provider: "firecracker", RuntimeID: "vm-1"}}

	result, err := (CreateSandbox{Sandboxes: store, Runtime: runtime}).Execute(t.Context(), CreateSandboxInput{ConversationID: "conv-1"})
	if err != nil || result.ID != "sandbox-1" {
		t.Fatalf("create sandbox result=%#v err=%v", result, err)
	}
	if runtime.destroyCalls != 0 {
		t.Fatalf("committed runtime was destroyed %d times", runtime.destroyCalls)
	}
}

func TestCreateSandboxDoesNotDestroyReusedRuntimeWhenDatabaseCreateFails(t *testing.T) {
	store := &stubConversationSandboxStore{createErr: errors.New("database unavailable")}
	runtime := &stubSandboxManager{createResult: &domain.SandboxHandle{Provider: "firecracker", RuntimeID: "vm-1", Reused: true}}

	if _, err := (CreateSandbox{Sandboxes: store, Runtime: runtime}).Execute(t.Context(), CreateSandboxInput{ConversationID: "conv-1"}); err == nil {
		t.Fatal("expected database create failure")
	}
	if runtime.destroyCalls != 0 {
		t.Fatalf("reused runtime was destroyed %d times", runtime.destroyCalls)
	}
}

func TestCreateSandboxCompensationIgnoresCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	store := &stubConversationSandboxStore{createErr: errors.New("database unavailable")}
	runtime := &stubSandboxManager{createResult: &domain.SandboxHandle{Provider: "firecracker", RuntimeID: "vm-1"}}

	if _, err := (CreateSandbox{Sandboxes: store, Runtime: runtime}).Execute(ctx, CreateSandboxInput{ConversationID: "conv-1"}); err == nil {
		t.Fatal("expected database create failure")
	}
	if runtime.destroyCalls != 1 || runtime.destroyContextErr != nil {
		t.Fatalf("compensation calls=%d contextErr=%v, want one uncanceled cleanup", runtime.destroyCalls, runtime.destroyContextErr)
	}
}

func TestCreateSandboxReturnsExistingWithoutRuntimeSideEffect(t *testing.T) {
	existing := &domain.ConversationSandbox{ID: "sandbox-1", ConversationID: "conv-1", Status: domain.SandboxStatusActive}
	store := &stubConversationSandboxStore{active: existing}
	runtime := &stubSandboxManager{}

	result, err := (CreateSandbox{Sandboxes: store, Runtime: runtime}).Execute(t.Context(), CreateSandboxInput{ConversationID: "conv-1"})
	if err != nil || result.ID != existing.ID {
		t.Fatalf("idempotent create result=%#v err=%v", result, err)
	}
	if runtime.createCalls != 0 {
		t.Fatalf("idempotent create called runtime %d times", runtime.createCalls)
	}
}

func TestCreateSandboxRuntimeFailureLeavesDatabaseUntouched(t *testing.T) {
	store := &stubConversationSandboxStore{}
	runtime := &stubSandboxManager{createErr: errors.New("runtime unavailable")}

	if _, err := (CreateSandbox{Sandboxes: store, Runtime: runtime}).Execute(t.Context(), CreateSandboxInput{ConversationID: "conv-1"}); err == nil {
		t.Fatal("expected runtime create failure")
	}
	if store.created != nil || runtime.destroyCalls != 0 {
		t.Fatalf("runtime create failure changed database or compensated nonexistent runtime: created=%#v destroys=%d", store.created, runtime.destroyCalls)
	}
}

func TestDestroySandboxKeepsReleasePendingWhenRuntimeDestroyFails(t *testing.T) {
	active := &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "local", RuntimeID: "runtime-1",
		Status: domain.SandboxStatusActive, RuntimeMetadata: json.RawMessage(`{"kind":"logical"}`),
	}
	store := &stubConversationSandboxStore{active: active}
	runtime := &stubSandboxManager{destroyErr: errors.New("runtime unavailable")}

	if _, err := (DestroySandbox{Sandboxes: store, Runtime: runtime}).Execute(t.Context(), DestroySandboxInput{ConversationID: "conv-1"}); err == nil {
		t.Fatal("expected runtime destroy failure")
	}
	if store.active == nil || store.active.Status != domain.SandboxStatusReleasing {
		t.Fatalf("database release was not left pending: %#v", store.active)
	}
}

func TestDestroySandboxDatabaseFailureLeavesRuntimeUntouched(t *testing.T) {
	active := &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "local", RuntimeID: "runtime-1",
		Status: domain.SandboxStatusActive,
	}
	store := &stubConversationSandboxStore{active: active, destroyErr: errors.New("database unavailable")}
	runtime := &stubSandboxManager{}

	if _, err := (DestroySandbox{Sandboxes: store, Runtime: runtime}).Execute(t.Context(), DestroySandboxInput{ConversationID: "conv-1"}); err == nil {
		t.Fatal("expected database destroy failure")
	}
	if runtime.destroyCalls != 0 || store.active == nil {
		t.Fatalf("database destroy failure changed runtime or active record: runtime=%d active=%#v", runtime.destroyCalls, store.active)
	}
}

func TestDestroySandboxReturnsPriorDestroyedRecordWithoutRuntimeSideEffect(t *testing.T) {
	destroyed := &domain.ConversationSandbox{ID: "sandbox-1", ConversationID: "conv-1", Status: domain.SandboxStatusDestroyed}
	store := &stubConversationSandboxStore{destroyed: destroyed}
	runtime := &stubSandboxManager{}

	result, err := (DestroySandbox{Sandboxes: store, Runtime: runtime}).Execute(t.Context(), DestroySandboxInput{ConversationID: "conv-1"})
	if err != nil || result.ID != destroyed.ID {
		t.Fatalf("idempotent destroy result=%#v err=%v", result, err)
	}
	if runtime.destroyCalls != 0 {
		t.Fatalf("idempotent destroy called runtime %d times", runtime.destroyCalls)
	}
}

func TestLocalExecutorExecSandboxCommand(t *testing.T) {
	store := &stubConversationSandboxStore{
		active: &domain.ConversationSandbox{
			ID:              "sandbox-1",
			ConversationID:  "conv-1",
			Provider:        "local",
			RuntimeID:       "runtime-1",
			Status:          domain.SandboxStatusActive,
			RuntimeMetadata: json.RawMessage(`{"workdir":"/tmp/sandbox"}`),
		},
	}
	runtime := &stubSandboxManager{
		execResult: &domain.SandboxCommandResult{
			RuntimeID:        "runtime-1",
			Command:          "pwd",
			WorkingDirectory: "/tmp/sandbox",
			Output:           "/tmp/sandbox\n",
			ExitCode:         0,
		},
	}
	executor := mustLocalExecutor(t, ExecSandboxCommandHandler{
		UseCase: ExecSandboxCommand{
			Sandboxes: store,
			Runtime:   runtime,
		},
	})

	result, err := executor.Execute(context.Background(), ToolScope{
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		HasSandbox:     true,
	}, ToolCall{
		Name:   SandboxExec,
		CallID: "call-6",
		Arguments: json.RawMessage(`{
			"command":"pwd",
			"timeout_seconds":10
		}`),
	})
	if err != nil {
		t.Fatalf("execute sandbox exec tool: %v", err)
	}

	if runtime.execHandle.RuntimeID != "runtime-1" || runtime.execRequest.Command != "pwd" || runtime.execRequest.TimeoutSeconds != 10 {
		t.Fatalf("unexpected sandbox exec input: handle=%#v request=%#v", runtime.execHandle, runtime.execRequest)
	}
	if result == nil || result.OutputItem.CallID != "call-6" || len(result.StreamEvents) != 0 {
		t.Fatalf("unexpected sandbox exec output: %#v", result)
	}
	if !strings.Contains(result.OutputItem.Output, `"output":"/tmp/sandbox\n"`) || strings.Contains(result.OutputItem.Output, `"stdout"`) || strings.Contains(result.OutputItem.Output, `"stderr"`) {
		t.Fatalf("sandbox tool did not return unified command output: %s", result.OutputItem.Output)
	}
}

func TestLocalExecutorSearchWeb(t *testing.T) {
	searcher := &stubWebSearcher{
		searchResult: &tavily.SearchResponse{
			Query:  "latest openai docs",
			Answer: "Use Responses API.",
			Results: []tavily.SearchResult{
				{
					Title:   "OpenAI Docs",
					URL:     "https://developers.openai.com/",
					Content: "Latest platform docs.",
				},
			},
		},
	}
	executor := mustLocalExecutor(t, SearchWebHandler{
		UseCase: TavilyTools{Client: searcher},
	})

	result, err := executor.Execute(context.Background(), ToolScope{
		ConversationID: "conv-1",
		TurnID:         "turn-1",
	}, ToolCall{
		Namespace: internetNamespace,
		Name:      internetSearchName,
		CallID:    "call-7",
		Arguments: json.RawMessage(`{
			"query":" latest openai docs ",
			"search_depth":"advanced",
			"max_results":12,
			"exact_match":true,
			"include_raw_content":true,
			"include_domains":[" developers.openai.com "]
		}`),
	})
	if err != nil {
		t.Fatalf("execute web search tool: %v", err)
	}

	if searcher.searchRequest.Query != "latest openai docs" || searcher.searchRequest.SearchDepth != "advanced" || searcher.searchRequest.MaxResults != 12 {
		t.Fatalf("unexpected search request: %#v", searcher.searchRequest)
	}
	if searcher.searchRequest.ExactMatch {
		t.Fatalf("unquoted query retained exact_match: %#v", searcher.searchRequest)
	}
	if searcher.searchRequest.IncludeAnswer != nil {
		t.Fatalf("unexpected include_answer option: %#v", searcher.searchRequest)
	}
	if rawContent, _ := searcher.searchRequest.IncludeRawContent.(bool); rawContent {
		t.Fatalf("unexpected include options: %#v", searcher.searchRequest)
	}
	if len(searcher.searchRequest.IncludeDomains) != 1 || searcher.searchRequest.IncludeDomains[0] != "developers.openai.com" {
		t.Fatalf("unexpected include domains: %#v", searcher.searchRequest.IncludeDomains)
	}
	if result == nil || result.OutputItem.CallID != "call-7" || len(result.StreamEvents) != 0 {
		t.Fatalf("unexpected web search output: %#v", result)
	}
	if !strings.Contains(result.OutputItem.Output, `"query":"latest openai docs"`) {
		t.Fatalf("unexpected tool output payload: %s", result.OutputItem.Output)
	}
}

func TestLocalExecutorExtractWeb(t *testing.T) {
	searcher := &stubWebSearcher{
		rawResult: json.RawMessage(`{"results":[{"url":"https://developers.openai.com","raw_content":"Docs"}]}`),
	}
	executor := mustLocalExecutor(t, ExtractWebHandler{
		UseCase: TavilyTools{Client: searcher},
	})

	result, err := executor.Execute(context.Background(), ToolScope{
		ConversationID: "conv-1",
		TurnID:         "turn-1",
	}, ToolCall{
		Namespace: internetNamespace,
		Name:      internetExtractName,
		CallID:    "call-8",
		Arguments: json.RawMessage(`{
			"urls":[" https://developers.openai.com "],
			"extract_depth":"advanced",
			"format":"markdown",
			"query":"Responses API"
		}`),
	})
	if err != nil {
		t.Fatalf("execute web extract tool: %v", err)
	}

	if len(searcher.extractRequest.URLs) != 1 || searcher.extractRequest.URLs[0] != "https://developers.openai.com" {
		t.Fatalf("unexpected extract urls: %#v", searcher.extractRequest.URLs)
	}
	if searcher.extractRequest.ExtractDepth != "advanced" || searcher.extractRequest.Format != "markdown" || searcher.extractRequest.Query != "Responses API" {
		t.Fatalf("unexpected extract request: %#v", searcher.extractRequest)
	}
	if result == nil || result.OutputItem.CallID != "call-8" || !strings.Contains(result.OutputItem.Output, `"raw_content":"Docs"`) {
		t.Fatalf("unexpected web extract output: %#v", result)
	}
}

func TestLocalExecutorWebFailuresAreRecoverable(t *testing.T) {
	upstreamErr := errors.New("tavily unavailable")
	tests := []struct {
		name    string
		handler LocalToolHandler
		call    ToolCall
	}{
		{
			name:    "search",
			handler: SearchWebHandler{UseCase: TavilyTools{Client: &stubWebSearcher{err: upstreamErr}}},
			call:    ToolCall{Namespace: internetNamespace, Name: internetSearchName, CallID: "call-search", Arguments: json.RawMessage(`{"query":"docs"}`)},
		},
		{
			name:    "extract",
			handler: ExtractWebHandler{UseCase: TavilyTools{Client: &stubWebSearcher{err: upstreamErr}}},
			call:    ToolCall{Namespace: internetNamespace, Name: internetExtractName, CallID: "call-extract", Arguments: json.RawMessage(`{"urls":["https://example.com"]}`)},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := mustLocalExecutor(t, test.handler)
			_, err := executor.Execute(t.Context(), ToolScope{ConversationID: "conv-1", TurnID: "turn-1"}, test.call)
			if !errors.Is(err, upstreamErr) || !IsRecoverableError(err) {
				t.Fatalf("web tool error = %v, want recoverable upstream error", err)
			}
		})
	}
}

func TestNewLocalExecutorRejectsDuplicateToolHandlers(t *testing.T) {
	_, err := NewLocalExecutor(
		RenameConversationTitleHandler{},
		RenameConversationTitleHandler{},
	)
	if err == nil || !strings.Contains(err.Error(), `duplicate local tool handler "conversation.rename_title"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
