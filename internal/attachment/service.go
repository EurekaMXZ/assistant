package attachment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
)

type Repository interface {
	CreateAttachment(ctx context.Context, params CreateAttachmentParams) (*domain.Attachment, error)
	GetAttachmentByIdempotencyKey(ctx context.Context, conversationID string, uploadedByUserID string, idempotencyKey string) (*domain.Attachment, error)
	ListAttachmentsByIDs(ctx context.Context, conversationID string, ids []string) ([]domain.Attachment, error)
}

type BlobStore interface {
	PutReader(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	DeleteObject(ctx context.Context, key string) error
}

type CreateAttachmentParams struct {
	ID               string
	ConversationID   string
	UploadedByUserID string
	IdempotencyKey   string
	Filename         string
	ContentType      string
	Category         string
	SizeBytes        int64
	SHA256           string
	ObjectKey        string
	Metadata         json.RawMessage
}

type UploadInput struct {
	ConversationID   string
	UploadedByUserID string
	IdempotencyKey   string
	Filename         string
	ContentType      string
	SizeBytes        int64
	File             io.Reader
	Metadata         json.RawMessage
}

type Service struct {
	Repo  Repository
	Blobs BlobStore
}

func (s *Service) Upload(ctx context.Context, input UploadInput) (*domain.Attachment, error) {
	if s == nil || s.Repo == nil {
		return nil, fmt.Errorf("attachment repository is required")
	}
	if s.Blobs == nil {
		return nil, fmt.Errorf("attachment blob store is required")
	}
	if strings.TrimSpace(input.ConversationID) == "" {
		return nil, domain.NewValidationError("conversation id is required")
	}
	if strings.TrimSpace(input.UploadedByUserID) == "" {
		return nil, domain.NewValidationError("uploaded by user id is required")
	}
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if len(idempotencyKey) > 128 {
		return nil, domain.NewValidationError("Idempotency-Key must be at most 128 characters")
	}
	if input.File == nil {
		return nil, domain.NewValidationError("file is required")
	}
	if input.SizeBytes <= 0 {
		return nil, domain.NewValidationError("file is empty")
	}
	if idempotencyKey != "" {
		existing, err := s.Repo.GetAttachmentByIdempotencyKey(ctx, input.ConversationID, input.UploadedByUserID, idempotencyKey)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
	}

	filename := sanitizeFilename(input.Filename)
	contentType := normalizeContentType(filename, input.ContentType)
	category := classifyAttachment(contentType, filename)
	attachmentID := uuid.NewString()
	objectKey := attachmentObjectKey(input.ConversationID, attachmentID, filename)

	hasher := sha256.New()
	reader := io.TeeReader(input.File, hasher)
	if err := s.Blobs.PutReader(ctx, objectKey, reader, input.SizeBytes, contentType); err != nil {
		return nil, fmt.Errorf("store attachment object: %w", err)
	}

	attachment, err := s.Repo.CreateAttachment(ctx, CreateAttachmentParams{
		ID:               attachmentID,
		ConversationID:   input.ConversationID,
		UploadedByUserID: input.UploadedByUserID,
		IdempotencyKey:   idempotencyKey,
		Filename:         filename,
		ContentType:      contentType,
		Category:         category,
		SizeBytes:        input.SizeBytes,
		SHA256:           hex.EncodeToString(hasher.Sum(nil)),
		ObjectKey:        objectKey,
		Metadata:         cloneJSON(input.Metadata),
	})
	if err != nil {
		_ = s.Blobs.DeleteObject(ctx, objectKey)
		return nil, err
	}
	if attachment.ObjectKey != objectKey {
		_ = s.Blobs.DeleteObject(ctx, objectKey)
	}

	return attachment, nil
}

func attachmentObjectKey(conversationID string, attachmentID string, filename string) string {
	return fmt.Sprintf("attachments/%s/%s/%s", strings.TrimSpace(conversationID), strings.TrimSpace(attachmentID), sanitizeFilename(filename))
}

func sanitizeFilename(filename string) string {
	trimmed := strings.TrimSpace(filename)
	trimmed = strings.ReplaceAll(trimmed, "\\", "/")
	base := path.Base(trimmed)
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == "/" {
		return "file"
	}
	base = strings.ReplaceAll(base, "/", "_")
	return base
}

func normalizeContentType(filename string, provided string) string {
	contentType := parseMediaType(provided)
	if contentType == "" || contentType == "application/octet-stream" {
		guessed := parseMediaType(mime.TypeByExtension(strings.ToLower(path.Ext(filename))))
		if guessed != "" {
			contentType = guessed
		}
	}
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}

func parseMediaType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}

func classifyAttachment(contentType string, filename string) string {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	ext := strings.ToLower(path.Ext(filename))

	if strings.HasPrefix(contentType, "image/") {
		return domain.AttachmentCategoryImage
	}
	if isDocumentType(contentType, ext) {
		return domain.AttachmentCategoryDocument
	}
	if isTextType(contentType, ext) {
		return domain.AttachmentCategoryText
	}
	return domain.AttachmentCategoryBinary
}

func isDocumentType(contentType string, ext string) bool {
	if _, ok := documentExtensions[ext]; ok {
		return true
	}
	_, ok := documentContentTypes[contentType]
	return ok
}

func isTextType(contentType string, ext string) bool {
	if strings.HasPrefix(contentType, "text/") {
		return true
	}
	if _, ok := textExtensions[ext]; ok {
		return true
	}
	_, ok := textContentTypes[contentType]
	return ok
}

var documentExtensions = map[string]struct{}{
	".csv":  {},
	".doc":  {},
	".docx": {},
	".ods":  {},
	".odt":  {},
	".pdf":  {},
	".ppt":  {},
	".pptx": {},
	".rtf":  {},
	".tsv":  {},
	".xls":  {},
	".xlsx": {},
}

var documentContentTypes = map[string]struct{}{
	"application/msword":                             {},
	"application/pdf":                                {},
	"application/rtf":                                {},
	"application/vnd.ms-excel":                       {},
	"application/vnd.ms-powerpoint":                  {},
	"application/vnd.oasis.opendocument.spreadsheet": {},
	"application/vnd.oasis.opendocument.text":        {},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   {},
	"text/csv":                  {},
	"text/tab-separated-values": {},
}

var textExtensions = map[string]struct{}{
	".bat":      {},
	".bash":     {},
	".c":        {},
	".cc":       {},
	".conf":     {},
	".cpp":      {},
	".css":      {},
	".diff":     {},
	".env":      {},
	".go":       {},
	".h":        {},
	".html":     {},
	".ini":      {},
	".java":     {},
	".js":       {},
	".json":     {},
	".jsx":      {},
	".log":      {},
	".lua":      {},
	".markdown": {},
	".md":       {},
	".mjs":      {},
	".patch":    {},
	".php":      {},
	".py":       {},
	".rb":       {},
	".rs":       {},
	".sh":       {},
	".sql":      {},
	".text":     {},
	".toml":     {},
	".ts":       {},
	".tsx":      {},
	".txt":      {},
	".xml":      {},
	".yaml":     {},
	".yml":      {},
	".zsh":      {},
}

var textContentTypes = map[string]struct{}{
	"application/graphql":    {},
	"application/javascript": {},
	"application/json":       {},
	"application/ld+json":    {},
	"application/sql":        {},
	"application/toml":       {},
	"application/x-bash":     {},
	"application/x-python":   {},
	"application/xml":        {},
	"application/yaml":       {},
	"text/javascript":        {},
}

func cloneJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func NormalizeAttachmentIDs(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(ids))
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, err := uuid.Parse(trimmed); err != nil {
			return nil, domain.NewValidationError(fmt.Sprintf("attachment id %q is invalid", trimmed))
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	return normalized, nil
}
