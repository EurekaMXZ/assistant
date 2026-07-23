package tool

import (
	"errors"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestCreateAndConnectSandboxShell(t *testing.T) {
	store := &stubConversationSandboxStore{active: &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "cubesandbox", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
	}}
	runtime := &stubSandboxManager{shellCommandResult: &domain.SandboxShellCommandResult{
		RuntimeID: "runtime-1", SessionID: "session-1", Output: "/workspace/project\n", ExitCode: 0,
	}}
	created, err := (CreateSandboxShell{Sandboxes: store, Runtime: runtime, Shells: runtime}).Execute(t.Context(), CreateSandboxShellInput{
		ConversationID: "conv-1", WorkingDirectory: "project", RequestKey: "run-1:call-1",
	})
	if err != nil {
		t.Fatalf("create shell: %v", err)
	}
	if created.SessionID == "" || runtime.shellCreateRequest.WorkingDirectory != "/workspace/project" {
		t.Fatalf("unexpected shell creation: session=%#v request=%#v", created, runtime.shellCreateRequest)
	}
	runtime.shellCommandResult.SessionID = created.SessionID
	result, err := (ConnectSandboxShell{
		Sandboxes: store, Runtime: runtime, Shells: runtime, DefaultTimeout: 20 * time.Second, MaximumTimeout: time.Minute,
	}).Execute(t.Context(), ConnectSandboxShellInput{
		ConversationID: "conv-1", SessionID: created.SessionID, Command: "pwd", TimeoutSeconds: 15, RequestKey: "run-1:call-2",
	})
	if err != nil {
		t.Fatalf("connect shell: %v", err)
	}
	if result.Output != "/workspace/project\n" || runtime.shellCommandRequest.Command != "pwd" || runtime.shellCommandRequest.TimeoutSeconds != 15 {
		t.Fatalf("unexpected shell command: result=%#v request=%#v", result, runtime.shellCommandRequest)
	}
	closed, err := (DestroySandboxShell{Sandboxes: store, Runtime: runtime, Shells: runtime}).Execute(t.Context(), DestroySandboxShellInput{
		ConversationID: "conv-1", SessionID: created.SessionID, RequestKey: "run-1:call-3",
	})
	if err != nil || closed.Status != domain.SandboxShellStatusClosed || runtime.destroyedShellID != created.SessionID {
		t.Fatalf("destroy shell: result=%#v id=%q err=%v", closed, runtime.destroyedShellID, err)
	}
}

func TestConnectSandboxShellRejectsMultilineScript(t *testing.T) {
	store := &stubConversationSandboxStore{active: &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "cubesandbox", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
	}}
	runtime := &stubSandboxManager{}
	_, err := (ConnectSandboxShell{Sandboxes: store, Runtime: runtime, Shells: runtime}).Execute(t.Context(), ConnectSandboxShellInput{
		ConversationID: "conv-1", SessionID: "shell-1", Command: "echo first\necho second",
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("multiline command error = %v", err)
	}
	if runtime.shellCommandRequest.Command != "" {
		t.Fatalf("multiline script reached runtime: %#v", runtime.shellCommandRequest)
	}
}
