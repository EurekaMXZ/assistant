package objectstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	miniosdk "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Store struct {
	client           *miniosdk.Client
	signer           *miniosdk.Client
	bucket           string
	region           string
	autoCreateBucket bool
	presignTTL       time.Duration
}

var _ workflow.TurnArtifactStore = (*Store)(nil)
var _ workflow.ToolArtifactStore = (*Store)(nil)
var _ workflow.ContextAnchorStore = (*Store)(nil)
var _ workflow.ContextCheckpointStore = (*Store)(nil)
var _ workflow.ImmutableRunArtifactStore = (*Store)(nil)
var _ workflow.RunArtifactObjectStore = (*Store)(nil)

func New(settings Settings) (*Store, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	endpoint, secure, err := normalizeEndpoint(settings.Endpoint, settings.UseSSL)
	if err != nil {
		return nil, err
	}
	lookup := bucketLookup(settings.BucketLookup)
	options := &miniosdk.Options{
		Creds:        credentials.NewStaticV4(settings.AccessKey, settings.SecretKey, settings.SessionToken),
		Secure:       secure,
		Region:       strings.TrimSpace(settings.Region),
		BucketLookup: lookup,
	}
	client, err := miniosdk.New(endpoint, options)
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}

	signer := client
	if strings.TrimSpace(settings.PublicEndpoint) != "" {
		publicEndpoint, publicSecure, normalizeErr := normalizeEndpoint(settings.PublicEndpoint, settings.UseSSL)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		signer, err = miniosdk.New(publicEndpoint, &miniosdk.Options{
			Creds:        credentials.NewStaticV4(settings.AccessKey, settings.SecretKey, settings.SessionToken),
			Secure:       publicSecure,
			Region:       strings.TrimSpace(settings.Region),
			BucketLookup: lookup,
		})
		if err != nil {
			return nil, fmt.Errorf("create public s3 signer: %w", err)
		}
	}
	presignTTL := settings.PresignTTL
	if presignTTL <= 0 {
		presignTTL = 15 * time.Minute
	}

	return &Store{
		client:           client,
		signer:           signer,
		bucket:           settings.Bucket,
		region:           settings.Region,
		autoCreateBucket: settings.AutoCreateBucket,
		presignTTL:       presignTTL,
	}, nil
}

func (s *Store) EnsureBucket(ctx context.Context) error {
	if !s.autoCreateBucket {
		return nil
	}
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check bucket %s: %w", s.bucket, err)
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, s.bucket, miniosdk.MakeBucketOptions{Region: s.region}); err != nil {
			return fmt.Errorf("create bucket %s: %w", s.bucket, err)
		}
	}
	return nil
}

func (s *Store) PresignUpload(ctx context.Context, key string, contentType string, sizeBytes int64, contentMD5 string) (*assistantattachment.PresignedURL, error) {
	signedHeaders := http.Header{
		"Content-Length": []string{strconv.FormatInt(sizeBytes, 10)},
		"Content-MD5":    []string{contentMD5},
	}
	if contentType = strings.TrimSpace(contentType); contentType != "" {
		signedHeaders.Set("Content-Type", contentType)
	}
	presigned, err := s.signer.PresignHeader(ctx, http.MethodPut, s.bucket, key, s.presignTTL, nil, signedHeaders)
	if err != nil {
		return nil, fmt.Errorf("presign put object %s: %w", key, err)
	}
	headers := map[string]string{"Content-MD5": contentMD5}
	if contentType != "" {
		headers["Content-Type"] = contentType
	}
	return &assistantattachment.PresignedURL{
		URL:       presigned.String(),
		Method:    http.MethodPut,
		Headers:   headers,
		ExpiresAt: time.Now().UTC().Add(s.presignTTL),
	}, nil
}

func (s *Store) PresignDownload(ctx context.Context, key string, filename string, attachment bool) (*assistantattachment.PresignedURL, error) {
	disposition := "inline"
	if attachment {
		disposition = "attachment"
	}
	params := url.Values{}
	params.Set("response-content-disposition", mime.FormatMediaType(disposition, map[string]string{"filename": filename}))
	presigned, err := s.signer.PresignedGetObject(ctx, s.bucket, key, s.presignTTL, params)
	if err != nil {
		return nil, fmt.Errorf("presign get object %s: %w", key, err)
	}
	return &assistantattachment.PresignedURL{
		URL:       presigned.String(),
		Method:    "GET",
		ExpiresAt: time.Now().UTC().Add(s.presignTTL),
	}, nil
}

func (s *Store) StatObject(ctx context.Context, key string) (*assistantattachment.ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, miniosdk.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("stat object %s: %w", key, normalizeObjectStoreReadError(err))
	}
	return &assistantattachment.ObjectInfo{SizeBytes: info.Size, ContentType: info.ContentType}, nil
}

func (s *Store) PutJSON(ctx context.Context, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal json for %s: %w", key, err)
	}

	return s.PutBytes(ctx, key, data, "application/json")
}

func (s *Store) PutBytes(ctx context.Context, key string, data []byte, contentType string) error {
	reader := bytes.NewReader(data)
	return s.PutReader(ctx, key, reader, int64(len(data)), contentType)
}

func (s *Store) PutImmutableBytes(ctx context.Context, key string, data []byte, contentType string) error {
	options := miniosdk.PutObjectOptions{ContentType: contentType}
	options.SetMatchETagExcept("*")
	if _, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), options); err != nil {
		response := miniosdk.ToErrorResponse(err)
		if response.Code != "PreconditionFailed" {
			return fmt.Errorf("put immutable object %s: %w", key, err)
		}
		existing, readErr := s.GetBytes(ctx, key)
		if readErr != nil {
			return fmt.Errorf("verify immutable object %s: %w", key, readErr)
		}
		if !bytes.Equal(existing, data) {
			return fmt.Errorf("immutable object %s already exists with different content: %w", key, domain.ErrConflict)
		}
	}
	return nil
}

func (s *Store) PutReader(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, reader, size, miniosdk.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}

	return nil
}

func (s *Store) DeleteObject(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, miniosdk.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete object %s: %w", key, err)
	}
	return nil
}

func (s *Store) ListRunArtifactObjects(ctx context.Context, prefix string) ([]workflow.RunArtifactObject, error) {
	objects := make([]workflow.RunArtifactObject, 0)
	for object := range s.client.ListObjects(ctx, s.bucket, miniosdk.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if object.Err != nil {
			return nil, fmt.Errorf("list objects with prefix %s: %w", prefix, object.Err)
		}
		objects = append(objects, workflow.RunArtifactObject{Key: object.Key, LastModified: object.LastModified})
	}
	return objects, nil
}

func (s *Store) GetBytes(ctx context.Context, key string) ([]byte, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, miniosdk.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object %s: %w", key, normalizeObjectStoreReadError(err))
	}
	defer object.Close()

	data, err := io.ReadAll(object)
	if err != nil {
		return nil, fmt.Errorf("read object %s: %w", key, normalizeObjectStoreReadError(err))
	}

	return data, nil
}

func (s *Store) OpenReader(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, miniosdk.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object %s: %w", key, normalizeObjectStoreReadError(err))
	}
	return object, nil
}

func (s *Store) GetJSON(ctx context.Context, key string, target any) error {
	data, err := s.GetBytes(ctx, key)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("unmarshal object %s: %w", key, err)
	}

	return nil
}

func turnModelContextKey(conversationID, turnID string) string {
	return fmt.Sprintf("conversations/%s/turns/%s/model-context.json.zst", conversationID, turnID)
}

func (s *Store) TurnModelContextKey(conversationID, turnID string) string {
	return turnModelContextKey(conversationID, turnID)
}

func turnRunRequestKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("conversations/%s/turns/%s/staging/step-%03d/request.json.zst", conversationID, turnID, stepIndex)
}

func (s *Store) TurnRunRequestKey(conversationID, turnID string, stepIndex int) string {
	return turnRunRequestKey(conversationID, turnID, stepIndex)
}

func (s *Store) TurnRunStateKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("conversations/%s/turns/%s/staging/step-%03d/state.json.zst", conversationID, turnID, stepIndex)
}

func (s *Store) TurnRunResultKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("conversations/%s/turns/%s/staging/step-%03d/outcome.json.zst", conversationID, turnID, stepIndex)
}

func immutableRunArtifactKey(conversationID, turnID string, stepIndex int, runID string, artifact string) string {
	return fmt.Sprintf("conversations/%s/turns/%s/runs/%06d-%s/%s", conversationID, turnID, stepIndex, runID, artifact)
}

func (s *Store) ImmutableRunArtifactKey(conversationID, turnID string, stepIndex int, runID string, artifact string) string {
	return immutableRunArtifactKey(conversationID, turnID, stepIndex, runID, artifact)
}

func toolCallArgumentsKey(conversationID, turnID, callID string) string {
	return fmt.Sprintf("conversations/%s/turns/%s/tool-calls/%s/arguments.json", conversationID, turnID, callID)
}

func (s *Store) ToolCallArgumentsKey(conversationID, turnID, callID string) string {
	return toolCallArgumentsKey(conversationID, turnID, callID)
}

func toolCallOutputKey(conversationID, turnID, callID string) string {
	return fmt.Sprintf("conversations/%s/turns/%s/tool-calls/%s/output.json", conversationID, turnID, callID)
}

func (s *Store) ToolCallOutputKey(conversationID, turnID, callID string) string {
	return toolCallOutputKey(conversationID, turnID, callID)
}

func contextAnchorKey(conversationID string, generation int64) string {
	return fmt.Sprintf("conversations/%s/context-anchors/gen-%06d.json", conversationID, generation)
}

func (s *Store) ContextAnchorKey(conversationID string, generation int64) string {
	return contextAnchorKey(conversationID, generation)
}

func (s *Store) ContextCheckpointKey(conversationID string, version int64) string {
	return fmt.Sprintf("conversations/%s/context-checkpoints/%06d.json.zst", conversationID, version)
}

func normalizeEndpoint(endpoint string, defaultSecure bool) (string, bool, error) {
	value := strings.TrimSpace(endpoint)
	if !strings.Contains(value, "://") {
		return value, defaultSecure, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return "", false, fmt.Errorf("invalid s3 endpoint %q", endpoint)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", false, fmt.Errorf("s3 endpoint must not include a path")
	}
	switch parsed.Scheme {
	case "http":
		return parsed.Host, false, nil
	case "https":
		return parsed.Host, true, nil
	default:
		return "", false, fmt.Errorf("s3 endpoint scheme must be http or https")
	}
}

func bucketLookup(value string) miniosdk.BucketLookupType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case BucketLookupDNS:
		return miniosdk.BucketLookupDNS
	case BucketLookupPath:
		return miniosdk.BucketLookupPath
	default:
		return miniosdk.BucketLookupAuto
	}
}

func normalizeObjectStoreReadError(err error) error {
	if err == nil {
		return nil
	}

	response := miniosdk.ToErrorResponse(err)
	switch response.Code {
	case miniosdk.NoSuchKey, miniosdk.NoSuchBucket:
		return domain.ErrNotFound
	default:
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrNotFound
		}
		return err
	}
}
