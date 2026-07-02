package minio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	miniosdk "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Store struct {
	client *miniosdk.Client
	bucket string
	region string
}

var _ workflow.TurnArtifactStore = (*Store)(nil)
var _ workflow.ToolArtifactStore = (*Store)(nil)
var _ workflow.ContextAnchorStore = (*Store)(nil)

func New(settings Settings) (*Store, error) {
	client, err := miniosdk.New(normalizeEndpoint(settings.Endpoint), &miniosdk.Options{
		Creds:  credentials.NewStaticV4(settings.AccessKey, settings.SecretKey, ""),
		Secure: settings.UseSSL,
		Region: settings.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	return &Store{
		client: client,
		bucket: settings.Bucket,
		region: settings.Region,
	}, nil
}

func (s *Store) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check bucket %s: %w", s.bucket, err)
	}
	if exists {
		return nil
	}

	if err := s.client.MakeBucket(ctx, s.bucket, miniosdk.MakeBucketOptions{Region: s.region}); err != nil {
		return fmt.Errorf("create bucket %s: %w", s.bucket, err)
	}

	return nil
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

func turnRequestKey(conversationID, turnID string) string {
	return fmt.Sprintf("requests/%s/%s.json", conversationID, turnID)
}

func (s *Store) TurnRequestKey(conversationID, turnID string) string {
	return turnRequestKey(conversationID, turnID)
}

func turnResponseKey(conversationID, turnID string) string {
	return fmt.Sprintf("responses/%s/%s.json", conversationID, turnID)
}

func (s *Store) TurnResponseKey(conversationID, turnID string) string {
	return turnResponseKey(conversationID, turnID)
}

func turnStreamKey(conversationID, turnID string) string {
	return fmt.Sprintf("stream-events/%s/%s.jsonl", conversationID, turnID)
}

func (s *Store) TurnStreamKey(conversationID, turnID string) string {
	return turnStreamKey(conversationID, turnID)
}

func turnModelContextKey(conversationID, turnID string) string {
	return fmt.Sprintf("turn-model-context/%s/%s.json", conversationID, turnID)
}

func (s *Store) TurnModelContextKey(conversationID, turnID string) string {
	return turnModelContextKey(conversationID, turnID)
}

func turnRunRequestKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("run-requests/%s/%s/step-%03d.json", conversationID, turnID, stepIndex)
}

func (s *Store) TurnRunRequestKey(conversationID, turnID string, stepIndex int) string {
	return turnRunRequestKey(conversationID, turnID, stepIndex)
}

func (s *Store) TurnRunStateKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("run-states/%s/%s/step-%03d.json", conversationID, turnID, stepIndex)
}

func (s *Store) TurnRunResultKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("run-results/%s/%s/step-%03d.json", conversationID, turnID, stepIndex)
}

func turnRunResponseKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("run-responses/%s/%s/step-%03d.json", conversationID, turnID, stepIndex)
}

func (s *Store) TurnRunResponseKey(conversationID, turnID string, stepIndex int) string {
	return turnRunResponseKey(conversationID, turnID, stepIndex)
}

func turnRunOutputItemsKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("run-output-items/%s/%s/step-%03d.json", conversationID, turnID, stepIndex)
}

func (s *Store) TurnRunOutputItemsKey(conversationID, turnID string, stepIndex int) string {
	return turnRunOutputItemsKey(conversationID, turnID, stepIndex)
}

func toolCallArgumentsKey(conversationID, turnID, callID string) string {
	return fmt.Sprintf("tool-calls/%s/%s/%s-arguments.json", conversationID, turnID, callID)
}

func (s *Store) ToolCallArgumentsKey(conversationID, turnID, callID string) string {
	return toolCallArgumentsKey(conversationID, turnID, callID)
}

func toolCallOutputKey(conversationID, turnID, callID string) string {
	return fmt.Sprintf("tool-calls/%s/%s/%s-output.json", conversationID, turnID, callID)
}

func (s *Store) ToolCallOutputKey(conversationID, turnID, callID string) string {
	return toolCallOutputKey(conversationID, turnID, callID)
}

func contextAnchorKey(conversationID string, generation int64) string {
	return fmt.Sprintf("context-items/%s/gen-%06d.json", conversationID, generation)
}

func (s *Store) ContextAnchorKey(conversationID string, generation int64) string {
	return contextAnchorKey(conversationID, generation)
}

func normalizeEndpoint(endpoint string) string {
	return strings.TrimSpace(endpoint)
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
