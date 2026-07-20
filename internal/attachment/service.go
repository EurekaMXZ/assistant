package attachment

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
)

type Repository interface {
	CreateAttachment(ctx context.Context, params CreateAttachmentParams) (*domain.Attachment, error)
	GetAttachment(ctx context.Context, conversationID string, uploadedByUserID string, attachmentID string) (*domain.Attachment, error)
	GetAttachmentByIdempotencyKey(ctx context.Context, conversationID string, uploadedByUserID string, idempotencyKey string) (*domain.Attachment, error)
	RefreshPendingAttachment(ctx context.Context, attachmentID string) (*domain.Attachment, error)
	CompleteAttachment(ctx context.Context, conversationID string, uploadedByUserID string, attachmentID string, sha256 string) (*domain.Attachment, error)
	ListAttachmentsByIDs(ctx context.Context, conversationID string, ids []string) ([]domain.Attachment, error)
}

type PresignedURL struct {
	URL       string
	Method    string
	Headers   map[string]string
	ExpiresAt time.Time
}

type ObjectInfo struct {
	SizeBytes   int64
	ContentType string
}

type URLSigner interface {
	PresignUpload(ctx context.Context, key string, contentType string, sizeBytes int64, contentMD5 string) (*PresignedURL, error)
	PresignDownload(ctx context.Context, key string, filename string, attachment bool) (*PresignedURL, error)
	StatObject(ctx context.Context, key string) (*ObjectInfo, error)
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
	ContentMD5       string
	Status           string
	ObjectKey        string
	Metadata         json.RawMessage
}

type CreateUploadInput struct {
	ConversationID   string
	UploadedByUserID string
	IdempotencyKey   string
	Filename         string
	ContentType      string
	SizeBytes        int64
	SHA256           string
	ContentMD5       string
	Metadata         json.RawMessage
}

type CompleteUploadInput struct {
	ConversationID   string
	UploadedByUserID string
	AttachmentID     string
}

type UploadIntent struct {
	Attachment *domain.Attachment
	Upload     *PresignedURL
}

type Service struct {
	Repo   Repository
	Signer URLSigner
}

const MaxUploadBytes int64 = 128 << 20

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (s *Service) CreateUpload(ctx context.Context, input CreateUploadInput) (*UploadIntent, error) {
	if s == nil || s.Repo == nil {
		return nil, fmt.Errorf("attachment repository is required")
	}
	if s.Signer == nil {
		return nil, fmt.Errorf("attachment url signer is required")
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
	if input.SizeBytes <= 0 {
		return nil, domain.NewValidationError("file is empty")
	}
	if input.SizeBytes > MaxUploadBytes {
		return nil, domain.NewValidationError(fmt.Sprintf("file exceeds %d bytes", MaxUploadBytes))
	}
	checksum := strings.ToLower(strings.TrimSpace(input.SHA256))
	if !sha256Pattern.MatchString(checksum) {
		return nil, domain.NewValidationError("sha256 must be a 64-character lowercase hex digest")
	}
	contentMD5 := strings.TrimSpace(input.ContentMD5)
	decodedMD5, err := base64.StdEncoding.DecodeString(contentMD5)
	if err != nil || len(decodedMD5) != 16 {
		return nil, domain.NewValidationError("content_md5 must be a base64-encoded 16-byte MD5 digest")
	}
	if idempotencyKey != "" {
		existing, err := s.Repo.GetAttachmentByIdempotencyKey(ctx, input.ConversationID, input.UploadedByUserID, idempotencyKey)
		if err == nil {
			if !matchesUpload(existing, input, checksum, contentMD5) {
				return nil, domain.ErrConflict
			}
			return s.uploadIntent(ctx, existing)
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

	attachment, err := s.Repo.CreateAttachment(ctx, CreateAttachmentParams{
		ID:               attachmentID,
		ConversationID:   input.ConversationID,
		UploadedByUserID: input.UploadedByUserID,
		IdempotencyKey:   idempotencyKey,
		Filename:         filename,
		ContentType:      contentType,
		Category:         category,
		SizeBytes:        input.SizeBytes,
		SHA256:           checksum,
		ContentMD5:       contentMD5,
		Status:           domain.AttachmentStatusPending,
		ObjectKey:        objectKey,
		Metadata:         cloneJSON(input.Metadata),
	})
	if err != nil {
		return nil, err
	}
	if !matchesUpload(attachment, input, checksum, contentMD5) {
		return nil, domain.ErrConflict
	}
	return s.uploadIntent(ctx, attachment)
}

func matchesUpload(attachment *domain.Attachment, input CreateUploadInput, sha256 string, contentMD5 string) bool {
	filename := sanitizeFilename(input.Filename)
	contentType := normalizeContentType(filename, input.ContentType)
	md5Matches := attachment != nil && (attachment.ContentMD5 == contentMD5 ||
		(attachment.Status == domain.AttachmentStatusReady && attachment.ContentMD5 == ""))
	return attachment != nil && attachment.Filename == filename && attachment.ContentType == contentType &&
		attachment.SizeBytes == input.SizeBytes && attachment.SHA256 == sha256 && md5Matches
}

func (s *Service) uploadIntent(ctx context.Context, attachment *domain.Attachment) (*UploadIntent, error) {
	if attachment.Status == domain.AttachmentStatusReady {
		return &UploadIntent{Attachment: attachment}, nil
	}
	if attachment.Status != domain.AttachmentStatusPending {
		return nil, domain.ErrConflict
	}
	refreshed, err := s.Repo.RefreshPendingAttachment(ctx, attachment.ID)
	if err != nil {
		return nil, err
	}
	upload, err := s.Signer.PresignUpload(ctx, refreshed.ObjectKey, refreshed.ContentType, refreshed.SizeBytes, refreshed.ContentMD5)
	if err != nil {
		return nil, fmt.Errorf("presign attachment upload: %w", err)
	}
	return &UploadIntent{Attachment: refreshed, Upload: upload}, nil
}

func (s *Service) CompleteUpload(ctx context.Context, input CompleteUploadInput) (*domain.Attachment, error) {
	if s == nil || s.Repo == nil || s.Signer == nil {
		return nil, fmt.Errorf("attachment service is not configured")
	}
	if _, err := uuid.Parse(strings.TrimSpace(input.AttachmentID)); err != nil {
		return nil, domain.NewValidationError("attachment id must be a UUID")
	}
	attachment, err := s.Repo.GetAttachment(ctx, input.ConversationID, input.UploadedByUserID, input.AttachmentID)
	if err != nil {
		return nil, err
	}
	if attachment.Status == domain.AttachmentStatusReady {
		return attachment, nil
	}
	info, err := s.Signer.StatObject(ctx, attachment.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("stat uploaded attachment: %w", err)
	}
	if info.SizeBytes != attachment.SizeBytes {
		return nil, domain.NewValidationError(fmt.Sprintf("uploaded object size is %d bytes, expected %d", info.SizeBytes, attachment.SizeBytes))
	}
	if actual := parseMediaType(info.ContentType); actual != "" && actual != attachment.ContentType {
		return nil, domain.NewValidationError(fmt.Sprintf("uploaded object content type is %q, expected %q", actual, attachment.ContentType))
	}
	return s.Repo.CompleteAttachment(ctx, input.ConversationID, input.UploadedByUserID, input.AttachmentID, attachment.SHA256)
}

func (s *Service) DownloadURL(ctx context.Context, conversationID string, uploadedByUserID string, attachmentID string, download bool) (*domain.Attachment, *PresignedURL, error) {
	if s == nil || s.Repo == nil || s.Signer == nil {
		return nil, nil, fmt.Errorf("attachment service is not configured")
	}
	if _, err := uuid.Parse(strings.TrimSpace(attachmentID)); err != nil {
		return nil, nil, domain.NewValidationError("attachment id must be a UUID")
	}
	attachment, err := s.Repo.GetAttachment(ctx, conversationID, uploadedByUserID, attachmentID)
	if err != nil {
		return nil, nil, err
	}
	if attachment.Status != domain.AttachmentStatusReady {
		return nil, nil, domain.NewValidationError("attachment upload is not complete")
	}
	presigned, err := s.Signer.PresignDownload(ctx, attachment.ObjectKey, attachment.Filename, download)
	if err != nil {
		return nil, nil, fmt.Errorf("presign attachment download: %w", err)
	}
	return attachment, presigned, nil
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
