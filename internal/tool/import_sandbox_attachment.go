package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
)

const sandboxAttachmentImportTimeout = 5 * time.Minute

type SandboxAttachmentStore interface {
	ListAttachmentsByIDs(ctx context.Context, conversationID string, ids []string) ([]domain.Attachment, error)
}

type SandboxAttachmentBlobReader interface {
	OpenReader(ctx context.Context, key string) (io.ReadCloser, error)
}

type ImportSandboxAttachmentInput struct {
	ConversationID string
	AttachmentID   string
	RequestKey     string
}

type SandboxAttachmentImportResult struct {
	AttachmentID string `json:"attachment_id"`
	Filename     string `json:"filename"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
	SandboxPath  string `json:"sandbox_path"`
}

type ImportSandboxAttachment struct {
	Attachments SandboxAttachmentStore
	Blobs       SandboxAttachmentBlobReader
	Sandboxes   ConversationSandboxStore
	Runtime     SandboxManager
	Locker      ConversationLocker
}

func (uc ImportSandboxAttachment) Execute(ctx context.Context, input ImportSandboxAttachmentInput) (*SandboxAttachmentImportResult, error) {
	if uc.Attachments == nil {
		return nil, errors.New("import sandbox attachment use case requires attachment store")
	}
	if uc.Blobs == nil {
		return nil, errors.New("import sandbox attachment use case requires attachment blob reader")
	}
	if uc.Sandboxes == nil {
		return nil, errors.New("import sandbox attachment use case requires sandbox store")
	}
	if uc.Runtime == nil {
		return nil, errors.New("import sandbox attachment use case requires sandbox runtime")
	}
	operationCtx, cancel := context.WithTimeout(ctx, sandboxAttachmentImportTimeout)
	defer cancel()
	conversationID := strings.TrimSpace(input.ConversationID)
	if conversationID == "" {
		return nil, domain.NewValidationError("conversation id is required")
	}
	attachmentID := strings.TrimSpace(input.AttachmentID)
	if _, err := uuid.Parse(attachmentID); err != nil {
		return nil, domain.NewValidationError("attachment_id must be a UUID")
	}

	attachments, err := uc.Attachments.ListAttachmentsByIDs(operationCtx, conversationID, []string{attachmentID})
	if err != nil {
		return nil, err
	}
	if len(attachments) != 1 || strings.TrimSpace(attachments[0].ObjectKey) == "" {
		return nil, domain.ErrNotFound
	}
	attachment := attachments[0]
	if attachment.SizeBytes <= 0 || attachment.SizeBytes > domain.SandboxFileMaxBytes {
		return nil, domain.NewValidationError(fmt.Sprintf("attachment must be between 1 and %d bytes", domain.SandboxFileMaxBytes))
	}

	reader, err := uc.Blobs.OpenReader(operationCtx, attachment.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("read attachment content: %w", err)
	}
	defer reader.Close()
	hasher := sha256.New()
	data, err := io.ReadAll(io.TeeReader(io.LimitReader(reader, attachment.SizeBytes+1), hasher))
	if err != nil {
		return nil, fmt.Errorf("read attachment content: %w", err)
	}
	if int64(len(data)) != attachment.SizeBytes {
		return nil, fmt.Errorf("attachment size mismatch: stored=%d expected=%d", len(data), attachment.SizeBytes)
	}
	actualSHA256 := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(strings.TrimSpace(attachment.SHA256), actualSHA256) {
		return nil, errors.New("attachment checksum mismatch")
	}

	sandboxPath := sandboxAttachmentPath(attachment)
	result := &SandboxAttachmentImportResult{
		AttachmentID: attachment.ID,
		Filename:     attachment.Filename,
		ContentType:  attachment.ContentType,
		SizeBytes:    attachment.SizeBytes,
		SHA256:       actualSHA256,
		SandboxPath:  sandboxPath,
	}
	return runConversationSandboxExecution(operationCtx, uc.Sandboxes, uc.Runtime, uc.Locker, conversationID, input.RequestKey, sandboxAttachmentImportTimeout+sandboxExecutionLeaseBuffer, func(writeCtx context.Context, handle domain.SandboxHandle) (*SandboxAttachmentImportResult, error) {
		if err := uc.Runtime.WriteSandboxFile(writeCtx, handle, sandboxPath, data, input.RequestKey); err != nil {
			return nil, err
		}
		return result, nil
	})
}

func sandboxAttachmentPath(attachment domain.Attachment) string {
	extension := strings.ToLower(path.Ext(strings.TrimSpace(attachment.Filename)))
	if len(extension) > 17 || !safeSandboxAttachmentExtension(extension) {
		extension = ""
	}
	return "/workspace/attachment-" + strings.TrimSpace(attachment.ID) + extension
}

func safeSandboxAttachmentExtension(extension string) bool {
	if extension == "" {
		return true
	}
	if extension[0] != '.' || len(extension) == 1 {
		return false
	}
	for _, char := range extension[1:] {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}
