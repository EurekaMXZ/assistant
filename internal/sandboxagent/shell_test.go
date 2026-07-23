package sandboxagent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestShellManagerPersistsStateAcrossConnections(t *testing.T) {
	workdir := t.TempDir()
	manager := NewShellManager(Settings{Workdir: workdir, MaxOutputBytes: 1024})
	session, err := manager.Create(t.Context(), domain.SandboxShellCreateRequest{SessionID: "shell-test"})
	if err != nil {
		t.Fatalf("create shell: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = manager.Destroy(cleanupCtx, session.SessionID)
	})

	if _, err := manager.Execute(t.Context(), domain.SandboxShellCommandRequest{
		SessionID: session.SessionID, Command: "export ASSISTANT_VALUE=kept && mkdir project && cd project", TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("set shell state: %v", err)
	}
	result, err := manager.Execute(t.Context(), domain.SandboxShellCommandRequest{
		SessionID: session.SessionID, Command: "printf '%s:%s' \"$ASSISTANT_VALUE\" \"$PWD\"", TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("read shell state: %v", err)
	}
	if result.ExitCode != 0 || result.TimedOut || !strings.Contains(result.Output, "kept:"+workdir+"/project") {
		t.Fatalf("persistent shell result = %#v", result)
	}

	closed, err := manager.Destroy(t.Context(), session.SessionID)
	if err != nil || closed.Status != domain.SandboxShellStatusClosed {
		t.Fatalf("destroy shell: result=%#v err=%v", closed, err)
	}
}
