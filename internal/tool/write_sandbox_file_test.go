package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestWriteSandboxFileWritesGeneratedContentAtomically(t *testing.T) {
	store := &stubConversationSandboxStore{active: &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "cubesandbox", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
	}}
	runtime := &stubSandboxManager{}
	content := "# Report\n\nGenerated directly.\n"
	result, err := (WriteSandboxFile{Sandboxes: store, Runtime: runtime}).Execute(t.Context(), WriteSandboxFileInput{
		ConversationID: "conv-1",
		Path:           "reports/result.md",
		Content:        content,
		RequestKey:     "run-1:call-1",
	})
	if err != nil {
		t.Fatalf("write sandbox file: %v", err)
	}
	digest := sha256.Sum256([]byte(content))
	wantPath := "/workspace/reports/result.md"
	wantTemporaryPath := wantPath + ".assistant-write-run-1_call-1"
	if result.Path != wantPath || result.SizeBytes != int64(len(content)) || result.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("unexpected write result: %#v", result)
	}
	if runtime.writtenPath != wantTemporaryPath || string(runtime.writtenData) != content {
		t.Fatalf("unexpected write stream: path=%q data=%q", runtime.writtenPath, runtime.writtenData)
	}
	if len(runtime.execRequests) != 3 || runtime.execRequests[1].Command != "mkdir" || runtime.execRequests[1].Args[2] != "/workspace/reports" {
		t.Fatalf("unexpected parent directory creation: %#v", runtime.execRequests)
	}
	if runtime.execRequest.Command != "mv" || len(runtime.execRequest.Args) != 3 || runtime.execRequest.Args[1] != wantTemporaryPath || runtime.execRequest.Args[2] != wantPath {
		t.Fatalf("unexpected atomic commit: %#v", runtime.execRequest)
	}
	if store.active.ExecutionToken != "" || store.active.ExecutionLeaseUntil != nil {
		t.Fatalf("sandbox execution lease was not completed: %#v", store.active)
	}
}

func TestWriteSandboxFileRejectsUnsafePathAndOversizedContent(t *testing.T) {
	useCase := WriteSandboxFile{
		Sandboxes: &stubConversationSandboxStore{active: &domain.ConversationSandbox{
			ID: "sandbox-1", ConversationID: "conv-1", Provider: "cubesandbox", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
		}},
		Runtime: &stubSandboxManager{},
	}
	if _, err := useCase.Execute(t.Context(), WriteSandboxFileInput{ConversationID: "conv-1", Path: "../outside.txt", Content: "data"}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("unsafe path error = %v", err)
	}
	if _, err := useCase.Execute(t.Context(), WriteSandboxFileInput{
		ConversationID: "conv-1", Path: "large.txt", Content: strings.Repeat("x", maxSandboxFileWriteContentBytes+1),
	}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("oversized content error = %v", err)
	}
}

func TestResolveSandboxWritePathRejectsSymlinkEscape(t *testing.T) {
	runtime := &stubSandboxManager{readlinkResult: &domain.SandboxCommandResult{Output: "/etc/escaped.txt\n", ExitCode: 0}}
	_, err := resolveSandboxWritePath(t.Context(), runtime, domain.SandboxHandle{RuntimeID: "runtime-1"}, "/workspace/link/escaped.txt", "request-1")
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestWriteSandboxFileCleansTemporaryPathWhenCommitFails(t *testing.T) {
	store := &stubConversationSandboxStore{active: &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "cubesandbox", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
	}}
	runtime := &stubSandboxManager{moveResult: &domain.SandboxCommandResult{Output: "rename failed", ExitCode: 1}}
	_, err := (WriteSandboxFile{Sandboxes: store, Runtime: runtime}).Execute(t.Context(), WriteSandboxFileInput{
		ConversationID: "conv-1", Path: "result.txt", Content: "data", RequestKey: "request-1",
	})
	if err == nil || !strings.Contains(err.Error(), "rename failed") {
		t.Fatalf("commit error = %v", err)
	}
	if len(runtime.execRequests) != 4 || runtime.execRequests[0].Command != "readlink" || runtime.execRequests[1].Command != "mkdir" || runtime.execRequests[2].Command != "mv" || runtime.execRequests[3].Command != "rm" {
		t.Fatalf("unexpected command sequence: %#v", runtime.execRequests)
	}
}
