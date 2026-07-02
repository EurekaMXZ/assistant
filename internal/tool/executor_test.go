package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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
	conversationID string
	created        *domain.ConversationSandbox
	destroyed      *domain.ConversationSandbox
	active         *domain.ConversationSandbox
	err            error
	createErr      error
	destroyErr     error
	restoreErr     error
	restored       *domain.ConversationSandbox
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
	if s.active == nil {
		return nil, domain.ErrNotFound
	}
	return s.active, nil
}

func (s *stubConversationSandboxStore) CreateConversationSandbox(_ context.Context, conversationID string, provider string, runtimeID string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	s.conversationID = conversationID
	if s.createErr != nil {
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

func (s *stubConversationSandboxStore) DestroyConversationSandbox(_ context.Context, sandboxID string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	if s.destroyErr != nil {
		return nil, s.destroyErr
	}
	s.destroyed = &domain.ConversationSandbox{
		ID:              sandboxID,
		ConversationID:  s.conversationID,
		Provider:        "local",
		RuntimeID:       "runtime-1",
		Status:          domain.SandboxStatusDestroyed,
		RuntimeMetadata: metadata,
	}
	s.active = nil
	return s.destroyed, nil
}

func (s *stubConversationSandboxStore) RestoreConversationSandbox(_ context.Context, sandboxID string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	if s.restoreErr != nil {
		return nil, s.restoreErr
	}
	s.restored = &domain.ConversationSandbox{
		ID: sandboxID, ConversationID: s.conversationID, Provider: "local", RuntimeID: "runtime-1",
		Status: domain.SandboxStatusActive, RuntimeMetadata: metadata,
	}
	s.active = s.restored
	return s.restored, nil
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
	requestKeys           []string
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

func (s *stubSandboxManager) DestroySandbox(_ context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error) {
	s.destroyCalls++
	s.requestKeys = append(s.requestKeys, requestKey)
	s.destroyedHandle = handle
	if s.destroyErr != nil {
		return nil, s.destroyErr
	}
	return s.destroyResult, nil
}

func (s *stubSandboxManager) ExecSandboxCommand(_ context.Context, handle domain.SandboxHandle, request domain.SandboxCommandRequest, requestKey string) (*domain.SandboxCommandResult, error) {
	s.requestKeys = append(s.requestKeys, requestKey)
	s.execHandle = handle
	s.execRequest = request
	if s.err != nil {
		return nil, s.err
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

func TestDestroySandboxRestoresDatabaseWhenRuntimeDestroyFails(t *testing.T) {
	active := &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "local", RuntimeID: "runtime-1",
		Status: domain.SandboxStatusActive, RuntimeMetadata: json.RawMessage(`{"kind":"logical"}`),
	}
	store := &stubConversationSandboxStore{active: active}
	runtime := &stubSandboxManager{destroyErr: errors.New("runtime unavailable")}

	if _, err := (DestroySandbox{Sandboxes: store, Runtime: runtime}).Execute(t.Context(), DestroySandboxInput{ConversationID: "conv-1"}); err == nil {
		t.Fatal("expected runtime destroy failure")
	}
	if store.restored == nil || store.active == nil || store.active.Status != domain.SandboxStatusActive {
		t.Fatalf("database state was not restored: %#v", store.active)
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
			Stdout:           "/tmp/sandbox\n",
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
			"include_answer":"advanced",
			"include_raw_content":"markdown",
			"include_domains":[" developers.openai.com "]
		}`),
	})
	if err != nil {
		t.Fatalf("execute web search tool: %v", err)
	}

	if searcher.searchRequest.Query != "latest openai docs" || searcher.searchRequest.SearchDepth != "advanced" || searcher.searchRequest.MaxResults != 12 {
		t.Fatalf("unexpected search request: %#v", searcher.searchRequest)
	}
	if searcher.searchRequest.IncludeAnswer != "advanced" || searcher.searchRequest.IncludeRawContent != "markdown" {
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

func TestNewLocalExecutorRejectsDuplicateToolHandlers(t *testing.T) {
	_, err := NewLocalExecutor(
		RenameConversationTitleHandler{},
		RenameConversationTitleHandler{},
	)
	if err == nil || !strings.Contains(err.Error(), `duplicate local tool handler "conversation.rename_title"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
