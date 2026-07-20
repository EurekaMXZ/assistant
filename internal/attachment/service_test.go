package attachment

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

const (
	testSHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	testMD5    = "XUFAKrxLKna5cZ2REBfFkg=="
)

type stubRepository struct {
	params   CreateAttachmentParams
	existing *domain.Attachment
	deleted  string
}

func (s *stubRepository) CreateAttachment(_ context.Context, params CreateAttachmentParams) (*domain.Attachment, error) {
	s.params = params
	s.existing = &domain.Attachment{
		ID: params.ID, ConversationID: params.ConversationID, UploadedByUserID: params.UploadedByUserID,
		Filename: params.Filename, ContentType: params.ContentType, Category: params.Category,
		SizeBytes: params.SizeBytes, SHA256: params.SHA256, ContentMD5: params.ContentMD5, Status: params.Status,
		ObjectKey: params.ObjectKey, Metadata: params.Metadata,
		CreatedAt: time.Unix(1710000000, 0).UTC(), UpdatedAt: time.Unix(1710000000, 0).UTC(),
	}
	return s.existing, nil
}

func (s *stubRepository) RefreshPendingAttachment(context.Context, string) (*domain.Attachment, error) {
	return s.existing, nil
}

func (s *stubRepository) GetAttachment(context.Context, string, string, string) (*domain.Attachment, error) {
	if s.existing == nil {
		return nil, domain.ErrNotFound
	}
	return s.existing, nil
}

func (s *stubRepository) GetAttachmentByIdempotencyKey(context.Context, string, string, string) (*domain.Attachment, error) {
	if s.existing == nil {
		return nil, domain.ErrNotFound
	}
	return s.existing, nil
}

func (s *stubRepository) CompleteAttachment(_ context.Context, _, _, _, checksum string) (*domain.Attachment, error) {
	s.existing.Status = domain.AttachmentStatusReady
	s.existing.SHA256 = checksum
	now := time.Now().UTC()
	s.existing.UploadCompletedAt = &now
	return s.existing, nil
}

func (s *stubRepository) ListAttachmentsByIDs(context.Context, string, []string) ([]domain.Attachment, error) {
	return nil, nil
}

func (s *stubRepository) GetStorageUsage(context.Context, string) (*domain.StorageUsage, error) {
	return &domain.StorageUsage{QuotaBytes: domain.DefaultStorageQuotaBytes}, nil
}

func (s *stubRepository) ListStorageAttachments(context.Context, string, int, string) ([]domain.StorageAttachment, string, error) {
	return nil, "", nil
}

func (s *stubRepository) ClaimAttachmentDeletion(context.Context, string, string) (*domain.Attachment, error) {
	return s.existing, nil
}

func (s *stubRepository) DeleteClaimedAttachmentUpload(_ context.Context, attachmentID string) error {
	s.deleted = attachmentID
	return nil
}

type stubObjectDeleter struct {
	key string
}

func (s *stubObjectDeleter) DeleteObject(_ context.Context, key string) error {
	s.key = key
	return nil
}

type stubURLSigner struct {
	key         string
	contentType string
	info        ObjectInfo
}

func (s *stubURLSigner) PresignUpload(_ context.Context, key string, contentType string, _ int64, _ string) (*PresignedURL, error) {
	s.key = key
	s.contentType = contentType
	return &PresignedURL{URL: "https://objects.example/upload", Method: "PUT", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (s *stubURLSigner) PresignDownload(context.Context, string, string, bool) (*PresignedURL, error) {
	return &PresignedURL{URL: "https://objects.example/download", Method: "GET", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (s *stubURLSigner) StatObject(context.Context, string) (*ObjectInfo, error) {
	return &s.info, nil
}

func TestCreateUploadStoresPendingMetadataAndReturnsPresignedURL(t *testing.T) {
	repo := &stubRepository{}
	signer := &stubURLSigner{}
	service := &Service{Repo: repo, Signer: signer}

	intent, err := service.CreateUpload(t.Context(), CreateUploadInput{
		ConversationID: "conv-1", UploadedByUserID: "user-1", Filename: " report.pdf ",
		ContentType: "application/octet-stream", SizeBytes: 5, SHA256: testSHA256, ContentMD5: testMD5,
		Metadata: json.RawMessage(`{"source":"test"}`),
	})
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}
	if intent.Attachment.Status != domain.AttachmentStatusPending || intent.Upload == nil {
		t.Fatalf("unexpected intent: %#v", intent)
	}
	if repo.params.Filename != "report.pdf" || repo.params.ContentType != "application/pdf" {
		t.Fatalf("unexpected normalized metadata: %#v", repo.params)
	}
	if repo.params.SHA256 != testSHA256 || repo.params.ContentMD5 != testMD5 || repo.params.ObjectKey == "" || signer.key != repo.params.ObjectKey {
		t.Fatalf("unexpected object metadata: %#v", repo.params)
	}
}

func TestDeleteStorageAttachmentRemovesObjectAndRow(t *testing.T) {
	repo := &stubRepository{existing: &domain.Attachment{
		ID: "attachment-1", ObjectKey: "attachments/conv-1/attachment-1/file.txt",
	}}
	objects := &stubObjectDeleter{}

	if err := (&Service{Repo: repo, Objects: objects}).DeleteStorageAttachment(t.Context(), "user-1", "attachment-1"); err != nil {
		t.Fatalf("delete storage attachment: %v", err)
	}
	if objects.key != repo.existing.ObjectKey {
		t.Fatalf("deleted object key = %q, want %q", objects.key, repo.existing.ObjectKey)
	}
	if repo.deleted != repo.existing.ID {
		t.Fatalf("deleted row = %q, want %q", repo.deleted, repo.existing.ID)
	}
}

func TestCompleteUploadStatsObjectBeforeMarkingReady(t *testing.T) {
	const attachmentID = "11111111-1111-4111-8111-111111111111"
	repo := &stubRepository{existing: &domain.Attachment{
		ID: attachmentID, ConversationID: "conv-1", UploadedByUserID: "user-1", Status: domain.AttachmentStatusPending,
		ContentType: "text/plain", SizeBytes: 5, SHA256: testSHA256, ContentMD5: testMD5,
		ObjectKey: "attachments/conv-1/" + attachmentID + "/file.txt",
	}}
	signer := &stubURLSigner{info: ObjectInfo{SizeBytes: 5, ContentType: "text/plain"}}
	attachment, err := (&Service{Repo: repo, Signer: signer}).CompleteUpload(t.Context(), CompleteUploadInput{
		ConversationID: "conv-1", UploadedByUserID: "user-1", AttachmentID: attachmentID,
	})
	if err != nil {
		t.Fatalf("complete upload: %v", err)
	}
	if attachment.Status != domain.AttachmentStatusReady || attachment.SHA256 != testSHA256 {
		t.Fatalf("unexpected completed attachment: %#v", attachment)
	}
}

func TestCreateUploadReplaysReadyAttachmentWithoutAnotherUploadURL(t *testing.T) {
	repo := &stubRepository{existing: &domain.Attachment{
		ID: "att-existing", ConversationID: "conv-1", UploadedByUserID: "user-1", Status: domain.AttachmentStatusReady,
		Filename: "notes.txt", ContentType: "text/plain", SizeBytes: 5, SHA256: testSHA256, ContentMD5: testMD5, ObjectKey: "attachments/existing",
	}}
	intent, err := (&Service{Repo: repo, Signer: &stubURLSigner{}}).CreateUpload(t.Context(), CreateUploadInput{
		ConversationID: "conv-1", UploadedByUserID: "user-1", IdempotencyKey: "upload-1",
		Filename: "notes.txt", ContentType: "text/plain", SizeBytes: 5, SHA256: testSHA256, ContentMD5: testMD5,
	})
	if err != nil {
		t.Fatalf("replay upload: %v", err)
	}
	if intent.Attachment.ID != "att-existing" || intent.Upload != nil {
		t.Fatalf("unexpected replay intent: %#v", intent)
	}
}

func TestCreateUploadRejectsIdempotencyReplayWithDifferentMetadata(t *testing.T) {
	repo := &stubRepository{existing: &domain.Attachment{
		ID: "att-existing", ConversationID: "conv-1", UploadedByUserID: "user-1", Status: domain.AttachmentStatusPending,
		Filename: "notes.txt", ContentType: "text/plain", SizeBytes: 5, SHA256: testSHA256, ContentMD5: testMD5, ObjectKey: "attachments/existing",
	}}
	_, err := (&Service{Repo: repo, Signer: &stubURLSigner{}}).CreateUpload(t.Context(), CreateUploadInput{
		ConversationID: "conv-1", UploadedByUserID: "user-1", IdempotencyKey: "upload-1",
		Filename: "different.txt", ContentType: "text/plain", SizeBytes: 7, SHA256: testSHA256, ContentMD5: testMD5,
	})
	if err != domain.ErrConflict {
		t.Fatalf("error = %v, want conflict", err)
	}
}

func TestClassifyAttachmentRecognizesTextAndImage(t *testing.T) {
	if category := classifyAttachment("image/png", "diagram.png"); category != domain.AttachmentCategoryImage {
		t.Fatalf("image category = %q", category)
	}
	if category := classifyAttachment("application/json", "config.json"); category != domain.AttachmentCategoryText {
		t.Fatalf("text category = %q", category)
	}
	if category := classifyAttachment("application/octet-stream", "tool.exe"); category != domain.AttachmentCategoryBinary {
		t.Fatalf("binary category = %q", category)
	}
}
