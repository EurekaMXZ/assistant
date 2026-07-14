package sandbox

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type fakeAgentBayClient struct {
	findLabels      map[string]string
	findImageID     string
	findResult      agentBayFindResult
	createRequest   agentBayCreateRequest
	createResult    agentBayCreateResult
	createCalls     int
	getSessionID    string
	directSessionID string
	session         agentBaySession
}

func (c *fakeAgentBayClient) Find(labels map[string]string, imageID string) (agentBayFindResult, error) {
	c.findLabels = labels
	c.findImageID = imageID
	return c.findResult, nil
}

func (c *fakeAgentBayClient) Create(request agentBayCreateRequest) (agentBayCreateResult, error) {
	c.createCalls++
	c.createRequest = request
	return c.createResult, nil
}

func (c *fakeAgentBayClient) Get(sessionID string) (agentBaySession, error) {
	c.getSessionID = sessionID
	return c.session, nil
}

func (c *fakeAgentBayClient) Session(sessionID string) agentBaySession {
	c.directSessionID = sessionID
	return c.session
}

type fakeAgentBaySession struct {
	id               string
	deleteResult     agentBayDeleteResult
	deleteCalls      int
	command          string
	timeoutMs        int
	workingDirectory string
	commandResult    agentBayCommandResult
}

func (s *fakeAgentBaySession) ID() string {
	return s.id
}

func (s *fakeAgentBaySession) Delete() (agentBayDeleteResult, error) {
	s.deleteCalls++
	return s.deleteResult, nil
}

func (s *fakeAgentBaySession) ExecuteCommand(command string, timeoutMs int, workingDirectory string) (agentBayCommandResult, error) {
	s.command = command
	s.timeoutMs = timeoutMs
	s.workingDirectory = workingDirectory
	return s.commandResult, nil
}

func TestAgentBayRuntimeLifecycle(t *testing.T) {
	createdSession := &fakeAgentBaySession{id: "session-1"}
	activeSession := &fakeAgentBaySession{
		id:           "session-1",
		deleteResult: agentBayDeleteResult{Success: true, RequestID: "delete-request"},
		commandResult: agentBayCommandResult{
			Output:   "hello\n",
			ExitCode: 0,
		},
	}
	client := &fakeAgentBayClient{
		createResult: agentBayCreateResult{Session: createdSession, RequestID: "create-request"},
		session:      activeSession,
	}
	runtime := newAgentBayRuntime(AgentBayRuntimeSettings{
		RegionID: "cn-hangzhou",
		ImageID:  "code_latest",
		PolicyID: "policy-1",
	}, client)
	runtime.now = func() time.Time { return time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC) }

	handle, err := runtime.CreateSandbox(context.Background(), " conv-1 ", "request:key")
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	if handle.Provider != ProviderAgentBay || handle.RuntimeID != "session-1" || handle.Reused {
		t.Fatalf("unexpected handle: %#v", handle)
	}
	if client.createRequest.ImageID != "code_latest" || client.createRequest.PolicyID != "policy-1" {
		t.Fatalf("unexpected create request: %#v", client.createRequest)
	}
	if client.createRequest.Labels[agentBayConversationLabel] != "conv-1" {
		t.Fatalf("unexpected conversation label: %#v", client.createRequest.Labels)
	}
	requestHash := client.createRequest.Labels[agentBayRequestKeyHashLabel]
	if requestHash != hashLabelValue("request:key") || strings.Contains(requestHash, "request:key") {
		t.Fatalf("unexpected request key hash label: %q", requestHash)
	}
	var metadata map[string]any
	if err := json.Unmarshal(handle.Metadata, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata["region_id"] != "cn-hangzhou" || metadata["image_id"] != "code_latest" || metadata["lifecycle"] != "manual_release" {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}

	result, err := runtime.ExecSandboxCommand(context.Background(), *handle, domain.SandboxCommandRequest{
		Command:          "printf",
		Args:             []string{"%s", "hello world", "a'b", "; rm -rf /"},
		WorkingDirectory: " /workspace/project ",
		TimeoutSeconds:   7,
	}, "exec-key")
	if err != nil {
		t.Fatalf("exec sandbox command: %v", err)
	}
	if client.getSessionID != "session-1" {
		t.Fatalf("get session ID = %q, want session-1", client.getSessionID)
	}
	wantCommand := `'printf' '%s' 'hello world' 'a'"'"'b' '; rm -rf /' 2>&1`
	if activeSession.command != wantCommand {
		t.Fatalf("command = %q, want %q", activeSession.command, wantCommand)
	}
	if activeSession.timeoutMs != 7000 || activeSession.workingDirectory != "/workspace/project" {
		t.Fatalf("unexpected command options: timeout=%d cwd=%q", activeSession.timeoutMs, activeSession.workingDirectory)
	}
	if result.RuntimeID != "session-1" || result.Output != "hello\n" || result.Command != "printf" {
		t.Fatalf("unexpected command result: %#v", result)
	}

	destroyed, err := runtime.DestroySandbox(context.Background(), *handle, "destroy-key")
	if err != nil {
		t.Fatalf("destroy sandbox: %v", err)
	}
	if activeSession.deleteCalls != 1 || destroyed.RuntimeID != "session-1" {
		t.Fatalf("unexpected destroy result: calls=%d handle=%#v", activeSession.deleteCalls, destroyed)
	}
	if client.directSessionID != "session-1" {
		t.Fatalf("direct session ID = %q, want session-1", client.directSessionID)
	}
}

func TestAgentBayRuntimeReusesSessionForRequestKey(t *testing.T) {
	existing := &fakeAgentBaySession{id: "session-existing"}
	client := &fakeAgentBayClient{
		findResult: agentBayFindResult{Session: existing, RequestID: "lookup-request"},
	}
	runtime := newAgentBayRuntime(AgentBayRuntimeSettings{ImageID: "code_latest"}, client)

	handle, err := runtime.CreateSandbox(context.Background(), "conv-1", "create-key")
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	if handle.RuntimeID != "session-existing" || !handle.Reused || client.createCalls != 0 {
		t.Fatalf("unexpected reuse result: handle=%#v createCalls=%d", handle, client.createCalls)
	}
	if client.findImageID != "code_latest" || client.findLabels[agentBayRequestKeyHashLabel] != hashLabelValue("create-key") {
		t.Fatalf("unexpected lookup: image=%q labels=%#v", client.findImageID, client.findLabels)
	}
	if !strings.Contains(string(handle.Metadata), `"reused":true`) || !strings.Contains(string(handle.Metadata), "lookup-request") {
		t.Fatalf("reuse metadata = %s", handle.Metadata)
	}
}

func TestAgentBayRuntimeRejectsCanceledRequestBeforeRemoteCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	session := &fakeAgentBaySession{id: "session-1", deleteResult: agentBayDeleteResult{Success: true}}
	client := &fakeAgentBayClient{createResult: agentBayCreateResult{Session: session}}
	runtime := newAgentBayRuntime(AgentBayRuntimeSettings{}, client)

	_, err := runtime.CreateSandbox(ctx, "conv-1", "request-key")
	if err == nil || err != context.Canceled {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if client.createRequest.ImageID != "" {
		t.Fatalf("unexpected remote create request after cancellation: %#v", client.createRequest)
	}
}

func TestAgentBayRuntimeUsesDefaultCommandTimeoutAndPreservesFailure(t *testing.T) {
	session := &fakeAgentBaySession{
		id: "session-1",
		commandResult: agentBayCommandResult{
			ExitCode:     2,
			ErrorMessage: "command failed",
			TimedOut:     true,
		},
	}
	runtime := newAgentBayRuntime(AgentBayRuntimeSettings{}, &fakeAgentBayClient{session: session})

	result, err := runtime.ExecSandboxCommand(context.Background(), domain.SandboxHandle{
		Provider: ProviderAgentBay, RuntimeID: "session-1",
	}, domain.SandboxCommandRequest{Command: "false"}, "")
	if err != nil {
		t.Fatalf("exec sandbox command: %v", err)
	}
	if session.timeoutMs != defaultCommandTimeoutSeconds*1000 {
		t.Fatalf("timeout = %d, want %d", session.timeoutMs, defaultCommandTimeoutSeconds*1000)
	}
	if result.ExitCode != 2 || result.Output != "command failed" || !result.TimedOut {
		t.Fatalf("unexpected failed command result: %#v", result)
	}
}

func TestAgentBayRuntimeTreatsAcceptedDeletionAsDestroyed(t *testing.T) {
	session := &fakeAgentBaySession{
		id:           "session-1",
		deleteResult: agentBayDeleteResult{Accepted: true, ErrorMessage: "Timeout waiting for session deletion after 5m"},
	}
	runtime := newAgentBayRuntime(AgentBayRuntimeSettings{}, &fakeAgentBayClient{session: session})

	_, err := runtime.DestroySandbox(context.Background(), domain.SandboxHandle{
		Provider: ProviderAgentBay, RuntimeID: "session-1",
	}, "")
	if err != nil {
		t.Fatalf("destroy accepted AgentBay session: %v", err)
	}
}

func TestNewAgentBayRuntimeRequiresAPIKey(t *testing.T) {
	_, err := NewAgentBayRuntime(AgentBayRuntimeSettings{})
	if err == nil || err.Error() != "AgentBay API key is required" {
		t.Fatalf("error = %v, want missing API key", err)
	}
}

func TestAgentBayMessageClassification(t *testing.T) {
	if !isAgentBayDeleteAccepted("Timeout waiting for session deletion after 5m") {
		t.Fatal("expected accepted asynchronous deletion timeout")
	}
	if !isAgentBayNotFound("InvalidMcpSession.NotFound") {
		t.Fatal("expected AgentBay not-found classification")
	}
	if !isAgentBayTimeout("command timed out after 30000 ms") {
		t.Fatal("expected command timeout classification")
	}
	if isAgentBayTimeout("invalid timeout option") {
		t.Fatal("configuration error must not be classified as execution timeout")
	}
}
