package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/EurekaMXZ/assistant/internal/domain"
)

type assistantAttachmentStoreStub struct {
	params assistantattachment.CreateAttachmentParams
}

func (s *assistantAttachmentStoreStub) UpsertAttachment(_ context.Context, params assistantattachment.CreateAttachmentParams) (*domain.Attachment, error) {
	s.params = params
	return &domain.Attachment{
		ID: params.ID, ConversationID: params.ConversationID, UploadedByUserID: params.UploadedByUserID,
		Filename: params.Filename, ContentType: params.ContentType, Category: params.Category,
		SizeBytes: params.SizeBytes, SHA256: params.SHA256, Status: params.Status,
	}, nil
}

type assistantAttachmentBlobStoreStub struct {
	key         string
	contentType string
	data        []byte
	deletedKey  string
}

type exportSandboxStoreStub struct {
	ConversationSandboxStore
	sandbox   domain.ConversationSandbox
	acquired  bool
	completed bool
}

func (s *exportSandboxStoreStub) GetUsableConversationSandbox(_ context.Context, _ string) (*domain.ConversationSandbox, error) {
	copy := s.sandbox
	return &copy, nil
}

func (s *exportSandboxStoreStub) AcquireConversationSandboxExecution(_ context.Context, _ string, _ string, _ time.Duration) error {
	s.acquired = true
	return nil
}

func (s *exportSandboxStoreStub) CompleteConversationSandboxExecution(_ context.Context, _ string, _ string) error {
	s.completed = true
	return nil
}

type exportSandboxRuntimeStub struct {
	SandboxManager
	request domain.SandboxCommandRequest
}

func (s *exportSandboxRuntimeStub) ExecSandboxCommand(_ context.Context, _ domain.SandboxHandle, request domain.SandboxCommandRequest, _ string) (*domain.SandboxCommandResult, error) {
	s.request = request
	return &domain.SandboxCommandResult{Output: "/workspace/results/report.csv\n", ExitCode: 0}, nil
}

type exportSandboxFileReaderStub struct {
	path string
}

func (s *exportSandboxFileReaderStub) ReadSandboxFile(_ context.Context, _ domain.SandboxHandle, path string) (io.ReadCloser, int64, error) {
	s.path = path
	return io.NopCloser(strings.NewReader("a,b\n1,2\n")), 8, nil
}

func (s *assistantAttachmentBlobStoreStub) PutReader(_ context.Context, key string, reader io.Reader, _ int64, contentType string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.key = key
	s.contentType = contentType
	s.data = data
	return nil
}

func (s *assistantAttachmentBlobStoreStub) DeleteObject(_ context.Context, key string) error {
	s.deletedKey = key
	return nil
}

func TestExportTextAttachmentPersistsReadyAttachment(t *testing.T) {
	attachments := &assistantAttachmentStoreStub{}
	blobs := &assistantAttachmentBlobStoreStub{}
	content := "# Result\n\nverified\n"
	result, err := (ExportTextAttachment{Attachments: attachments, Blobs: blobs}).Execute(context.Background(), ExportTextAttachmentInput{
		ConversationID: "11111111-1111-1111-1111-111111111111",
		TurnID:         "22222222-2222-2222-2222-222222222222",
		OwnerUserID:    "33333333-3333-3333-3333-333333333333",
		CallID:         "call/export:1",
		Filename:       "../result.md",
		Content:        content,
	})
	if err != nil {
		t.Fatalf("export text attachment: %v", err)
	}
	if result.Filename != "result.md" || result.ContentType != "text/plain" || result.Category != domain.AttachmentCategoryText {
		t.Fatalf("unexpected attachment result: %#v", result)
	}
	if string(blobs.data) != content || blobs.contentType != "text/plain" {
		t.Fatalf("unexpected stored object: content=%q type=%q", blobs.data, blobs.contentType)
	}
	if !strings.Contains(blobs.key, "/call_export_1/result.md") {
		t.Fatalf("object key was not sanitized: %q", blobs.key)
	}
	wantChecksum := sha256.Sum256([]byte(content))
	if attachments.params.SHA256 != hex.EncodeToString(wantChecksum[:]) || attachments.params.Status != domain.AttachmentStatusReady {
		t.Fatalf("unexpected attachment params: %#v", attachments.params)
	}
}

func TestExportSandboxFileResolvesAndPersistsWorkspaceFile(t *testing.T) {
	attachments := &assistantAttachmentStoreStub{}
	blobs := &assistantAttachmentBlobStoreStub{}
	store := &exportSandboxStoreStub{sandbox: domain.ConversationSandbox{
		ID: "sandbox-1", ConversationID: "11111111-1111-1111-1111-111111111111",
		Provider: "firecracker", RuntimeID: "runtime-1", Status: domain.SandboxStatusActive,
	}}
	runtime := &exportSandboxRuntimeStub{}
	files := &exportSandboxFileReaderStub{}
	result, err := (ExportSandboxFile{
		Attachments: attachments, Blobs: blobs, Sandboxes: store, Runtime: runtime, Files: files,
	}).Execute(context.Background(), ExportSandboxFileInput{
		ConversationID: "11111111-1111-1111-1111-111111111111",
		TurnID:         "22222222-2222-2222-2222-222222222222",
		OwnerUserID:    "33333333-3333-3333-3333-333333333333",
		CallID:         "call-1", SandboxPath: "results/report.csv", RequestKey: "request-1",
	})
	if err != nil {
		t.Fatalf("export sandbox file: %v", err)
	}
	if !store.acquired || !store.completed {
		t.Fatalf("sandbox execution lease was not completed: acquired=%t completed=%t", store.acquired, store.completed)
	}
	if runtime.request.Command != "readlink" || files.path != "/workspace/results/report.csv" {
		t.Fatalf("unexpected sandbox resolution: request=%#v path=%q", runtime.request, files.path)
	}
	if result.Filename != "report.csv" || string(blobs.data) != "a,b\n1,2\n" || attachments.params.Metadata == nil {
		t.Fatalf("unexpected exported attachment: result=%#v data=%q params=%#v", result, blobs.data, attachments.params)
	}
}

func TestExportTextAttachmentRejectsOversizedContent(t *testing.T) {
	_, err := (ExportTextAttachment{
		Attachments: &assistantAttachmentStoreStub{},
		Blobs:       &assistantAttachmentBlobStoreStub{},
	}).Execute(context.Background(), ExportTextAttachmentInput{
		ConversationID: "conversation", TurnID: "turn", OwnerUserID: "owner", CallID: "call",
		Filename: "large.txt", Content: strings.Repeat("x", maxAssistantTextAttachmentBytes+1),
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestNormalizeSandboxWorkspacePath(t *testing.T) {
	for input, want := range map[string]string{
		"results/output.csv": "/workspace/results/output.csv",
		"/workspace/a.txt":   "/workspace/a.txt",
		"./report.md":        "/workspace/report.md",
	} {
		got, err := normalizeSandboxWorkspacePath(input)
		if err != nil || got != want {
			t.Fatalf("normalize %q = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"", "/workspace", "../secret", "/etc/passwd", "/workspace/../../etc/passwd"} {
		if _, err := normalizeSandboxWorkspacePath(input); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("normalize %q should fail validation, got %v", input, err)
		}
	}
}
