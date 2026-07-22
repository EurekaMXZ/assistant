package sandboxagent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
		t.Fatalf("ExitCode = %d, want 0; output=%q", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "nested") {
		t.Fatalf("Output = %q, want nested workdir", result.Output)
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
	if !strings.Contains(result.Output, filepath.Join(base, "outside")) {
		t.Fatalf("Output = %q, want outside workdir", result.Output)
	}
}

func TestExecPreservesStdoutAndStderrOrder(t *testing.T) {
	result, err := Exec(context.Background(), Settings{Workdir: t.TempDir(), MaxOutputBytes: 1024}, domain.SandboxCommandRequest{
		Command: "sh",
		Args:    []string{"-c", "printf 'first\\n'; printf 'second\\n' >&2; printf 'third\\n'"},
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.Output != "first\nsecond\nthird\n" {
		t.Fatalf("Output = %q, want interleaved command output", result.Output)
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

func TestWriteFileAtomicallyWritesInsideWorkspace(t *testing.T) {
	workdir := t.TempDir()
	target := filepath.Join(workdir, "input.bin")
	if err := WriteFile(t.Context(), Settings{Workdir: workdir, MaxFileBytes: 1024}, target, bytes.NewReader([]byte{0, 1, 2, 3})); err != nil {
		t.Fatalf("write file: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !bytes.Equal(data, []byte{0, 1, 2, 3}) {
		t.Fatalf("data = %v", data)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("file mode = %v", info.Mode().Perm())
	}
}

func TestWriteFileRejectsTraversalAndOversizedContent(t *testing.T) {
	workdir := t.TempDir()
	err := WriteFile(t.Context(), Settings{Workdir: workdir, MaxFileBytes: 4}, filepath.Join(workdir, "..", "outside"), strings.NewReader("data"))
	if err == nil || !strings.Contains(err.Error(), "inside the sandbox workspace") {
		t.Fatalf("traversal error = %v", err)
	}
	err = WriteFile(t.Context(), Settings{Workdir: workdir, MaxFileBytes: 4}, filepath.Join(workdir, "large"), strings.NewReader("12345"))
	if err == nil || !strings.Contains(err.Error(), "exceeds 4 bytes") {
		t.Fatalf("oversize error = %v", err)
	}
}

func TestOpenFileReadsRegularWorkspaceFileAndRejectsEscapingSymlink(t *testing.T) {
	workdir := t.TempDir()
	target := filepath.Join(workdir, "result.txt")
	if err := os.WriteFile(target, []byte("result-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, size, err := OpenFile(Settings{Workdir: workdir, MaxFileBytes: 1024}, target)
	if err != nil {
		t.Fatalf("open workspace file: %v", err)
	}
	data, readErr := io.ReadAll(file)
	file.Close()
	if readErr != nil || size != 11 || string(data) != "result-data" {
		t.Fatalf("unexpected workspace file: size=%d data=%q err=%v", size, data, readErr)
	}

	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workdir, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenFile(Settings{Workdir: workdir}, link); !errors.Is(err, errInvalidFileRequest) {
		t.Fatalf("escaping symlink error = %v, want invalid file request", err)
	}
}

func TestFileHandlerMapsValidationAndSizeErrors(t *testing.T) {
	workdir := t.TempDir()
	handler := NewHandler(Settings{Workdir: workdir, MaxFileBytes: 4})

	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodPut, "/files?path=/outside", strings.NewReader("data")))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid path status = %d, want %d", invalid.Code, http.StatusBadRequest)
	}

	oversized := httptest.NewRecorder()
	handler.ServeHTTP(oversized, httptest.NewRequest(http.MethodPut, "/files?path="+filepath.Join(workdir, "large"), strings.NewReader("12345")))
	if oversized.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d, want %d", oversized.Code, http.StatusRequestEntityTooLarge)
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
