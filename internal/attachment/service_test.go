package attachment

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type stubRepository struct {
	params   CreateAttachmentParams
	existing *domain.Attachment
	err      error
}

func (s *stubRepository) GetAttachmentByIdempotencyKey(context.Context, string, string, string) (*domain.Attachment, error) {
	if s.existing != nil {
		return s.existing, nil
	}
	return nil, domain.ErrNotFound
}

func (s *stubRepository) CreateAttachment(_ context.Context, params CreateAttachmentParams) (*domain.Attachment, error) {
	s.params = params
	if s.err != nil {
		return nil, s.err
	}
	return &domain.Attachment{
		ID:               params.ID,
		ConversationID:   params.ConversationID,
		UploadedByUserID: params.UploadedByUserID,
		Filename:         params.Filename,
		ContentType:      params.ContentType,
		Category:         params.Category,
		SizeBytes:        params.SizeBytes,
		SHA256:           params.SHA256,
		ObjectKey:        params.ObjectKey,
		Metadata:         params.Metadata,
		CreatedAt:        time.Unix(1710000000, 0).UTC(),
		UpdatedAt:        time.Unix(1710000000, 0).UTC(),
	}, nil
}

func (s *stubRepository) ListAttachmentsByIDs(context.Context, string, []string) ([]domain.Attachment, error) {
	return nil, nil
}

type stubBlobStore struct {
	key         string
	contentType string
	size        int64
	data        []byte
	deleteKey   string
	err         error
}

func (s *stubBlobStore) PutReader(_ context.Context, key string, reader io.Reader, size int64, contentType string) error {
	s.key = key
	s.size = size
	s.contentType = contentType
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.data = data
	return s.err
}

func (s *stubBlobStore) DeleteObject(_ context.Context, key string) error {
	s.deleteKey = key
	return nil
}

func TestUploadStoresObjectAndMetadata(t *testing.T) {
	repo := &stubRepository{}
	blobs := &stubBlobStore{}
	service := &Service{Repo: repo, Blobs: blobs}

	attachment, err := service.Upload(context.Background(), UploadInput{
		ConversationID:   "conv-1",
		UploadedByUserID: "user-1",
		Filename:         " report.pdf ",
		ContentType:      "application/octet-stream",
		SizeBytes:        int64(len("hello")),
		File:             bytes.NewBufferString("hello"),
		Metadata:         json.RawMessage(`{"source":"test"}`),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if attachment.Category != domain.AttachmentCategoryDocument {
		t.Fatalf("category = %q, want %q", attachment.Category, domain.AttachmentCategoryDocument)
	}
	if blobs.contentType != "application/pdf" {
		t.Fatalf("contentType = %q, want %q", blobs.contentType, "application/pdf")
	}
	if string(blobs.data) != "hello" {
		t.Fatalf("stored data = %q, want %q", blobs.data, "hello")
	}
	if repo.params.Filename != "report.pdf" {
		t.Fatalf("filename = %q, want %q", repo.params.Filename, "report.pdf")
	}
	if repo.params.SHA256 != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("unexpected sha256: %q", repo.params.SHA256)
	}
	if repo.params.ObjectKey == "" || blobs.key != repo.params.ObjectKey {
		t.Fatalf("expected matching object key, repo=%q blob=%q", repo.params.ObjectKey, blobs.key)
	}
	if string(repo.params.Metadata) != `{"source":"test"}` {
		t.Fatalf("metadata = %s", repo.params.Metadata)
	}
}

func TestUploadDeletesObjectWhenRepositoryFails(t *testing.T) {
	repo := &stubRepository{err: domain.ErrConflict}
	blobs := &stubBlobStore{}
	service := &Service{Repo: repo, Blobs: blobs}

	_, err := service.Upload(context.Background(), UploadInput{
		ConversationID:   "conv-1",
		UploadedByUserID: "user-1",
		Filename:         "archive.zip",
		ContentType:      "application/zip",
		SizeBytes:        int64(len("hello")),
		File:             bytes.NewBufferString("hello"),
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if blobs.deleteKey == "" {
		t.Fatal("expected uploaded object to be deleted on failure")
	}
}

func TestUploadReplaysIdempotentAttachmentWithoutStoringAnotherObject(t *testing.T) {
	existing := &domain.Attachment{ID: "att-existing", ConversationID: "conv-1", UploadedByUserID: "user-1", ObjectKey: "attachments/existing"}
	repo := &stubRepository{existing: existing}
	blobs := &stubBlobStore{}
	service := &Service{Repo: repo, Blobs: blobs}

	attachment, err := service.Upload(context.Background(), UploadInput{
		ConversationID: "conv-1", UploadedByUserID: "user-1", IdempotencyKey: "upload-1",
		Filename: "notes.txt", ContentType: "text/plain", SizeBytes: 5, File: bytes.NewBufferString("hello"),
	})
	if err != nil {
		t.Fatalf("replay upload: %v", err)
	}
	if attachment.ID != existing.ID {
		t.Fatalf("attachment id = %q, want %q", attachment.ID, existing.ID)
	}
	if blobs.key != "" {
		t.Fatalf("unexpected object upload for replay: %q", blobs.key)
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
