package sandboxagent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestExecRunsCommandInsideWorkdir(t *testing.T) {
	workdir := t.TempDir()
	result, err := Exec(context.Background(), Settings{Workdir: workdir, MaxOutputBytes: 1024}, domain.SandboxCommandRequest{
		Command:          "pwd",
		WorkingDirectory: "nested",
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0; stderr=%q", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "nested") {
		t.Fatalf("Stdout = %q, want nested workdir", result.Stdout)
	}
}

func TestExecAllowsWorkdirOutsideRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	result, err := Exec(context.Background(), Settings{Workdir: root, MaxOutputBytes: 1024}, domain.SandboxCommandRequest{
		Command:          "pwd",
		WorkingDirectory: "../outside",
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(result.Stdout, filepath.Join(base, "outside")) {
		t.Fatalf("Stdout = %q, want outside workdir", result.Stdout)
	}
}

func TestExecMarksTimeout(t *testing.T) {
	result, err := Exec(context.Background(), Settings{Workdir: t.TempDir(), MaxOutputBytes: 1024}, domain.SandboxCommandRequest{
		Command:        "sleep",
		Args:           []string{"2"},
		TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !result.TimedOut || result.ExitCode != -1 {
		t.Fatalf("unexpected timeout result: %#v", result)
	}
}

func TestConfigureNetworkValidatesInput(t *testing.T) {
	err := ConfigureNetwork(context.Background(), NetworkConfigRequest{
		Interface: "eth0",
		Address:   "not-cidr",
		Gateway:   "172.16.0.1",
	})
	if err == nil || !strings.Contains(err.Error(), "address must be CIDR") {
		t.Fatalf("error = %v, want CIDR validation", err)
	}

	err = ConfigureNetwork(context.Background(), NetworkConfigRequest{
		Interface: "eth0",
		Address:   "172.16.0.100/24",
		Gateway:   "not-ip",
	})
	if err == nil || !strings.Contains(err.Error(), "gateway") {
		t.Fatalf("error = %v, want gateway validation", err)
	}
}
