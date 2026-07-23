package tool

import (
	"errors"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestEditSandboxFileReplacesUniqueTextAtomically(t *testing.T) {
	store := &stubConversationSandboxStore{active: &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "cubesandbox", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
	}}
	existing := "first line\nold value\nlast line\n"
	runtime := &stubSandboxManager{readData: []byte(existing), readSize: int64(len(existing))}
	result, err := (EditSandboxFile{Sandboxes: store, Runtime: runtime, Files: runtime}).Execute(t.Context(), EditSandboxFileInput{
		ConversationID: "conv-1",
		Path:           "config/settings.txt",
		OldText:        "old value",
		NewText:        "new value",
		RequestKey:     "run-1:call-2",
	})
	if err != nil {
		t.Fatalf("edit sandbox file: %v", err)
	}
	if result.Path != "/workspace/config/settings.txt" || result.Replacements != 1 {
		t.Fatalf("unexpected edit result: %#v", result)
	}
	if runtime.readPath != result.Path || string(runtime.writtenData) != "first line\nnew value\nlast line\n" {
		t.Fatalf("unexpected edit IO: read=%q written=%q", runtime.readPath, runtime.writtenData)
	}
	if runtime.execRequest.Command != "mv" || runtime.execRequest.Args[2] != result.Path {
		t.Fatalf("edit was not committed atomically: %#v", runtime.execRequest)
	}
}

func TestEditSandboxFileRejectsAmbiguousMatch(t *testing.T) {
	store := &stubConversationSandboxStore{active: &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "cubesandbox", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
	}}
	existing := "same\nsame\n"
	runtime := &stubSandboxManager{readData: []byte(existing), readSize: int64(len(existing))}
	_, err := (EditSandboxFile{Sandboxes: store, Runtime: runtime, Files: runtime}).Execute(t.Context(), EditSandboxFileInput{
		ConversationID: "conv-1", Path: "result.txt", OldText: "same", NewText: "changed", RequestKey: "request-1",
	})
	if !errors.Is(err, domain.ErrInvalidInput) || !strings.Contains(err.Error(), "occurs 2 times") {
		t.Fatalf("ambiguous edit error = %v", err)
	}
	if runtime.writtenData != nil {
		t.Fatalf("ambiguous edit wrote data: %q", runtime.writtenData)
	}
}

func TestEditSandboxFileReplaceAll(t *testing.T) {
	store := &stubConversationSandboxStore{active: &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "cubesandbox", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
	}}
	existing := "same\nsame\n"
	runtime := &stubSandboxManager{readData: []byte(existing), readSize: int64(len(existing))}
	result, err := (EditSandboxFile{Sandboxes: store, Runtime: runtime, Files: runtime}).Execute(t.Context(), EditSandboxFileInput{
		ConversationID: "conv-1", Path: "result.txt", OldText: "same", NewText: "changed", ReplaceAll: true, RequestKey: "request-1",
	})
	if err != nil {
		t.Fatalf("replace all: %v", err)
	}
	if result.Replacements != 2 || string(runtime.writtenData) != "changed\nchanged\n" {
		t.Fatalf("unexpected replace-all result: result=%#v data=%q", result, runtime.writtenData)
	}
}
