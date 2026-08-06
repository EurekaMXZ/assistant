package tool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestReadSandboxFileReadsTextWithLineMetadata(t *testing.T) {
	content := "first line\nsecond line\nthird line\n"
	store := &stubConversationSandboxStore{active: &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "local", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
	}}
	runtime := &stubSandboxManager{readData: []byte(content), readSize: int64(len(content))}

	result, err := (ReadSandboxFile{
		Attachments: &assistantAttachmentStoreStub{},
		Blobs:       &assistantAttachmentBlobStoreStub{},
		Sandboxes:   store,
		Runtime:     runtime,
		Files:       runtime,
	}).Execute(context.Background(), SandboxFileReadInput{
		ConversationID: "conv-1", TurnID: "turn-1", OwnerUserID: "user-1", CallID: "call-1",
		Path: "reports/result.txt", Offset: 2, Limit: 1, RequestKey: "request-1",
	})
	if err != nil {
		t.Fatalf("read sandbox text: %v", err)
	}
	if result.Path != "/workspace/reports/result.txt" || result.ContentType != "text/plain; charset=utf-8" || result.LineStart != 2 || result.LineEnd != 2 || !result.Truncated {
		t.Fatalf("unexpected text result: %#v", result)
	}
	if !strings.Contains(result.Content, "2: second line") || !strings.Contains(result.Content, "offset=3") {
		t.Fatalf("unexpected text content: %s", result.Content)
	}
	if runtime.readPath != result.Path {
		t.Fatalf("sandbox read path = %q, want %q", runtime.readPath, result.Path)
	}
}

func TestReadSandboxFilePersistsImageAndReturnsReference(t *testing.T) {
	imageData := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	attachments := &assistantAttachmentStoreStub{}
	blobs := &assistantAttachmentBlobStoreStub{}
	store := &stubConversationSandboxStore{active: &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "local", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
	}}
	runtime := &stubSandboxManager{readData: imageData, readSize: int64(len(imageData))}

	result, err := (ReadSandboxFile{
		Attachments: attachments,
		Blobs:       blobs,
		Sandboxes:   store,
		Runtime:     runtime,
		Files:       runtime,
	}).Execute(context.Background(), SandboxFileReadInput{
		ConversationID: "conv-1", TurnID: "turn-1", OwnerUserID: "user-1", CallID: "call-1",
		Path: "images/chart.png", RequestKey: "request-1",
	})
	if err != nil {
		t.Fatalf("read sandbox image: %v", err)
	}
	if result.Image == nil || result.Image.ContentType != "image/png" || result.Image.ObjectKey == "" || result.Image.SizeBytes != int64(len(imageData)) {
		t.Fatalf("unexpected image result: %#v", result)
	}
	if string(blobs.data) != string(imageData) || attachments.params.Metadata == nil {
		t.Fatalf("image was not persisted: data=%v params=%#v", blobs.data, attachments.params)
	}
	if !strings.Contains(string(attachments.params.Metadata), `"source":"sandbox_read"`) {
		t.Fatalf("unexpected image metadata: %s", attachments.params.Metadata)
	}
}

func TestReadSandboxFileRejectsBinaryFile(t *testing.T) {
	store := &stubConversationSandboxStore{active: &domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "conv-1", Provider: "local", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
	}}
	runtime := &stubSandboxManager{readData: []byte{0, 1, 2, 3}, readSize: 4}

	_, err := (ReadSandboxFile{
		Attachments: &assistantAttachmentStoreStub{},
		Blobs:       &assistantAttachmentBlobStoreStub{},
		Sandboxes:   store,
		Runtime:     runtime,
		Files:       runtime,
	}).Execute(context.Background(), SandboxFileReadInput{
		ConversationID: "conv-1", TurnID: "turn-1", OwnerUserID: "user-1", CallID: "call-1",
		Path: "output.bin", RequestKey: "request-1",
	})
	if !errors.Is(err, domain.ErrInvalidInput) || !strings.Contains(err.Error(), "UTF-8 text") {
		t.Fatalf("binary read error = %v", err)
	}
}
