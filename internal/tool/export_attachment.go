package tool

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
)

const (
	assistantAttachmentExportTimeout = 5 * time.Minute
	maxAssistantTextAttachmentBytes  = 1 << 20
)

type AssistantAttachmentStore interface {
	UpsertAttachment(ctx context.Context, params assistantattachment.CreateAttachmentParams) (*domain.Attachment, error)
}

type AssistantAttachmentBlobStore interface {
	PutReader(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	DeleteObject(ctx context.Context, key string) error
}

type AssistantAttachmentResult struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Category    string `json:"category"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
}

type AssistantAttachmentReference struct {
	ConversationID string                    `json:"conversation_id"`
	TurnID         string                    `json:"turn_id"`
	Source         string                    `json:"source"`
	Attachment     AssistantAttachmentResult `json:"attachment"`
}

type AssistantAttachmentToolOutput struct {
	AssistantAttachment *AssistantAttachmentReference `json:"assistant_attachment,omitempty"`
}

type ExportSandboxFileInput struct {
	ConversationID string
	TurnID         string
	OwnerUserID    string
	CallID         string
	SandboxPath    string
	Filename       string
	RequestKey     string
}

type ExportSandboxFile struct {
	Attachments AssistantAttachmentStore
	Blobs       AssistantAttachmentBlobStore
	Sandboxes   ConversationSandboxStore
	Runtime     SandboxManager
	Files       SandboxFileReader
	Locker      ConversationLocker
}

func (uc ExportSandboxFile) Execute(ctx context.Context, input ExportSandboxFileInput) (*AssistantAttachmentResult, error) {
	if uc.Attachments == nil || uc.Blobs == nil || uc.Sandboxes == nil || uc.Runtime == nil || uc.Files == nil {
		return nil, errors.New("sandbox file export is not configured")
	}
	sandboxPath, err := normalizeSandboxExportPath(input.SandboxPath)
	if err != nil {
		return nil, err
	}
	operationCtx, cancel := context.WithTimeout(ctx, assistantAttachmentExportTimeout)
	defer cancel()
	return runConversationSandboxExecution(operationCtx, uc.Sandboxes, uc.Runtime, uc.Locker, strings.TrimSpace(input.ConversationID), input.RequestKey, assistantAttachmentExportTimeout+sandboxExecutionLeaseBuffer, func(exportCtx context.Context, handle domain.SandboxHandle) (*AssistantAttachmentResult, error) {
		resolved, err := resolveSandboxExportPath(exportCtx, uc.Runtime, handle, sandboxPath, input.RequestKey)
		if err != nil {
			return nil, err
		}
		reader, size, err := uc.Files.ReadSandboxFile(exportCtx, handle, resolved)
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		filename := strings.TrimSpace(input.Filename)
		if filename == "" {
			filename = path.Base(resolved)
		}
		return persistAssistantAttachment(exportCtx, uc.Attachments, uc.Blobs, assistantAttachmentPersistInput{
			ConversationID: input.ConversationID,
			TurnID:         input.TurnID,
			OwnerUserID:    input.OwnerUserID,
			CallID:         input.CallID,
			Filename:       filename,
			Source:         "sandbox_export",
			SourcePath:     resolved,
			Reader:         reader,
			SizeBytes:      size,
		})
	})
}

type ExportTextAttachmentInput struct {
	ConversationID string
	TurnID         string
	OwnerUserID    string
	CallID         string
	Filename       string
	Content        string
}

type ExportTextAttachment struct {
	Attachments AssistantAttachmentStore
	Blobs       AssistantAttachmentBlobStore
}

func (uc ExportTextAttachment) Execute(ctx context.Context, input ExportTextAttachmentInput) (*AssistantAttachmentResult, error) {
	if uc.Attachments == nil || uc.Blobs == nil {
		return nil, errors.New("text attachment export is not configured")
	}
	if strings.TrimSpace(input.Filename) == "" {
		return nil, domain.NewValidationError("filename is required")
	}
	if !utf8.ValidString(input.Content) {
		return nil, domain.NewValidationError("content must be valid UTF-8")
	}
	size := int64(len(input.Content))
	if size <= 0 {
		return nil, domain.NewValidationError("content is empty")
	}
	if size > maxAssistantTextAttachmentBytes {
		return nil, domain.NewValidationError(fmt.Sprintf("text attachment exceeds %d bytes", maxAssistantTextAttachmentBytes))
	}
	return persistAssistantAttachment(ctx, uc.Attachments, uc.Blobs, assistantAttachmentPersistInput{
		ConversationID: input.ConversationID,
		TurnID:         input.TurnID,
		OwnerUserID:    input.OwnerUserID,
		CallID:         input.CallID,
		Filename:       input.Filename,
		ContentType:    "text/plain; charset=utf-8",
		Source:         "text_export",
		Reader:         strings.NewReader(input.Content),
		SizeBytes:      size,
	})
}

type assistantAttachmentPersistInput struct {
	ConversationID string
	TurnID         string
	OwnerUserID    string
	CallID         string
	Filename       string
	ContentType    string
	Source         string
	SourcePath     string
	Reader         io.Reader
	SizeBytes      int64
}

func persistAssistantAttachment(ctx context.Context, attachments AssistantAttachmentStore, blobs AssistantAttachmentBlobStore, input assistantAttachmentPersistInput) (*AssistantAttachmentResult, error) {
	if strings.TrimSpace(input.ConversationID) == "" || strings.TrimSpace(input.TurnID) == "" || strings.TrimSpace(input.OwnerUserID) == "" || strings.TrimSpace(input.CallID) == "" {
		return nil, errors.New("assistant attachment scope is incomplete")
	}
	if input.Reader == nil || input.SizeBytes <= 0 || input.SizeBytes > domain.SandboxFileMaxBytes {
		return nil, domain.NewValidationError(fmt.Sprintf("attachment must be between 1 and %d bytes", domain.SandboxFileMaxBytes))
	}
	filename := assistantattachment.SanitizeFilename(input.Filename)
	buffered := bufio.NewReader(io.LimitReader(input.Reader, input.SizeBytes+1))
	peekSize := min(int64(512), input.SizeBytes)
	preview, _ := buffered.Peek(int(peekSize))
	providedType := strings.TrimSpace(input.ContentType)
	if providedType == "" && len(preview) > 0 {
		providedType = http.DetectContentType(preview)
	}
	contentType := assistantattachment.NormalizeContentType(filename, providedType)
	category := assistantattachment.ClassifyAttachment(contentType, filename)
	objectKey := assistantAttachmentObjectKey(input.ConversationID, input.TurnID, input.CallID, filename)
	attachmentID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(objectKey)).String()
	hasher := sha256.New()
	stream := &countingReader{reader: io.TeeReader(io.LimitReader(buffered, input.SizeBytes), hasher)}
	if err := blobs.PutReader(ctx, objectKey, stream, input.SizeBytes, contentType); err != nil {
		return nil, fmt.Errorf("store assistant attachment: %w", err)
	}
	removeObject := true
	defer func() {
		if removeObject {
			_ = blobs.DeleteObject(context.WithoutCancel(ctx), objectKey)
		}
	}()
	if stream.read != input.SizeBytes {
		return nil, fmt.Errorf("assistant attachment size mismatch: streamed=%d expected=%d", stream.read, input.SizeBytes)
	}
	var extra [1]byte
	if count, readErr := buffered.Read(extra[:]); count != 0 || (readErr != nil && !errors.Is(readErr, io.EOF)) {
		return nil, errors.New("assistant attachment contains more bytes than expected")
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))
	metadata, err := json.Marshal(map[string]any{
		"source":       input.Source,
		"turn_id":      input.TurnID,
		"tool_call_id": input.CallID,
		"sandbox_path": strings.TrimSpace(input.SourcePath),
	})
	if err != nil {
		return nil, err
	}
	attachment, err := attachments.UpsertAttachment(ctx, assistantattachment.CreateAttachmentParams{
		ID: attachmentID, ConversationID: input.ConversationID, UploadedByUserID: input.OwnerUserID,
		Filename: filename, ContentType: contentType, Category: category, SizeBytes: input.SizeBytes,
		SHA256: checksum, Status: domain.AttachmentStatusReady, ObjectKey: objectKey, Metadata: metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("record assistant attachment: %w", err)
	}
	removeObject = false
	return &AssistantAttachmentResult{
		ID: attachment.ID, Filename: attachment.Filename, ContentType: attachment.ContentType,
		Category: attachment.Category, SizeBytes: attachment.SizeBytes, SHA256: attachment.SHA256,
	}, nil
}

func normalizeSandboxExportPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", domain.NewValidationError("path is required")
	}
	if !path.IsAbs(value) {
		value = path.Join("/workspace", value)
	}
	value = path.Clean(value)
	if value == "/workspace" || !strings.HasPrefix(value, "/workspace/") {
		return "", domain.NewValidationError("path must be inside /workspace")
	}
	return value, nil
}

func resolveSandboxExportPath(ctx context.Context, runtime SandboxManager, handle domain.SandboxHandle, value string, requestKey string) (string, error) {
	result, err := runtime.ExecSandboxCommand(ctx, handle, domain.SandboxCommandRequest{
		Command: "readlink", Args: []string{"-f", "--", value}, WorkingDirectory: "/workspace", TimeoutSeconds: 30,
	}, requestKey+":resolve")
	if err != nil {
		return "", fmt.Errorf("resolve sandbox export path: %w", err)
	}
	if result == nil || result.ExitCode != 0 {
		return "", domain.NewValidationError("sandbox export path does not exist")
	}
	resolved := strings.TrimSpace(result.Output)
	if resolved == "/workspace" || !strings.HasPrefix(resolved, "/workspace/") || strings.Contains(resolved, "\n") {
		return "", domain.NewValidationError("resolved sandbox export path must be inside /workspace")
	}
	return resolved, nil
}

func assistantAttachmentObjectKey(conversationID string, turnID string, callID string, filename string) string {
	return fmt.Sprintf("assistant-attachments/%s/%s/%s/%s", strings.TrimSpace(conversationID), strings.TrimSpace(turnID), safeAssistantAttachmentKeyPart(callID), assistantattachment.SanitizeFilename(filename))
}

func safeAssistantAttachmentKeyPart(value string) string {
	var result strings.Builder
	for _, char := range strings.TrimSpace(value) {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			result.WriteRune(char)
		} else {
			result.WriteByte('_')
		}
	}
	if result.Len() == 0 {
		return "attachment"
	}
	return result.String()
}

func assistantAttachmentReference(scope ToolScope, source string, attachment *AssistantAttachmentResult) AssistantAttachmentToolOutput {
	return AssistantAttachmentToolOutput{AssistantAttachment: &AssistantAttachmentReference{
		ConversationID: scope.ConversationID, TurnID: scope.TurnID, Source: source, Attachment: *attachment,
	}}
}
