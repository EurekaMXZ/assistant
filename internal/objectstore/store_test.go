package objectstore

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	miniosdk "github.com/minio/minio-go/v7"
)

func TestNewUsesSettings(t *testing.T) {
	store, err := New(Settings{
		Provider:     ProviderMinIO,
		Endpoint:     "127.0.0.1:9000",
		Region:       "us-east-1",
		Bucket:       "assistant",
		AccessKey:    "minio",
		SecretKey:    "minio123",
		UseSSL:       false,
		BucketLookup: BucketLookupPath,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	if store.bucket != "assistant" {
		t.Fatalf("bucket = %q, want %q", store.bucket, "assistant")
	}
	if store.region != "us-east-1" {
		t.Fatalf("region = %q, want %q", store.region, "us-east-1")
	}
}

func TestPresignedURLsUseBrowserVisibleEndpoint(t *testing.T) {
	store, err := New(Settings{
		Provider: ProviderR2, Endpoint: "r2.internal", PublicEndpoint: "https://account.r2.cloudflarestorage.com",
		Region: "auto", Bucket: "assistant", AccessKey: "access", SecretKey: "secret", BucketLookup: BucketLookupPath,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	upload, err := store.PresignUpload(t.Context(), "attachments/one.png", "image/png", 42, "XUFAKrxLKna5cZ2REBfFkg==")
	if err != nil {
		t.Fatalf("presign upload: %v", err)
	}
	parsed, err := url.Parse(upload.URL)
	if err != nil {
		t.Fatalf("parse upload url: %v", err)
	}
	if parsed.Host != "account.r2.cloudflarestorage.com" || upload.Method != "PUT" || upload.Headers["Content-Type"] != "image/png" || upload.Headers["Content-MD5"] != "XUFAKrxLKna5cZ2REBfFkg==" {
		t.Fatalf("unexpected upload: %#v", upload)
	}
	if signed := parsed.Query().Get("X-Amz-SignedHeaders"); !strings.Contains(signed, "content-length") || !strings.Contains(signed, "content-md5") {
		t.Fatalf("signed headers = %q", signed)
	}
}

func TestNormalizeEndpointAcceptsS3ProviderURLs(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		secure bool
		host   string
	}{
		{name: "aws", input: "https://s3.us-east-1.amazonaws.com", secure: true, host: "s3.us-east-1.amazonaws.com"},
		{name: "aliyun", input: "https://oss-cn-hangzhou.aliyuncs.com", secure: true, host: "oss-cn-hangzhou.aliyuncs.com"},
		{name: "r2", input: "https://account.r2.cloudflarestorage.com", secure: true, host: "account.r2.cloudflarestorage.com"},
		{name: "minio", input: "127.0.0.1:9000", secure: false, host: "127.0.0.1:9000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host, secure, err := normalizeEndpoint(test.input, false)
			if err != nil {
				t.Fatalf("normalize endpoint: %v", err)
			}
			if host != test.host || secure != test.secure {
				t.Fatalf("endpoint = (%q, %v), want (%q, %v)", host, secure, test.host, test.secure)
			}
		})
	}
}

func TestStoreKeyBuildersUseStableLayout(t *testing.T) {
	store := &Store{}

	if got := store.TurnRequestKey("conv-1", "turn-1"); got != "requests/conv-1/turn-1.json" {
		t.Fatalf("request key = %q, want %q", got, "requests/conv-1/turn-1.json")
	}
	if got := store.TurnResponseKey("conv-1", "turn-1"); got != "responses/conv-1/turn-1.json" {
		t.Fatalf("response key = %q, want %q", got, "responses/conv-1/turn-1.json")
	}
	if got := store.TurnStreamKey("conv-1", "turn-1"); got != "stream-events/conv-1/turn-1.jsonl" {
		t.Fatalf("stream key = %q, want %q", got, "stream-events/conv-1/turn-1.jsonl")
	}
	if got := store.TurnModelContextKey("conv-1", "turn-1"); got != "turn-model-context/conv-1/turn-1.json" {
		t.Fatalf("turn model context key = %q, want %q", got, "turn-model-context/conv-1/turn-1.json")
	}
	if got := store.TurnRunRequestKey("conv-1", "turn-1", 2); got != "run-requests/conv-1/turn-1/step-002.json" {
		t.Fatalf("run request key = %q, want %q", got, "run-requests/conv-1/turn-1/step-002.json")
	}
	if got := store.TurnRunResponseKey("conv-1", "turn-1", 2); got != "run-responses/conv-1/turn-1/step-002.json" {
		t.Fatalf("run response key = %q, want %q", got, "run-responses/conv-1/turn-1/step-002.json")
	}
	if got := store.TurnRunOutputItemsKey("conv-1", "turn-1", 2); got != "run-output-items/conv-1/turn-1/step-002.json" {
		t.Fatalf("run output items key = %q, want %q", got, "run-output-items/conv-1/turn-1/step-002.json")
	}
	if got := store.ToolCallArgumentsKey("conv-1", "turn-1", "call-1"); got != "tool-calls/conv-1/turn-1/call-1-arguments.json" {
		t.Fatalf("tool arguments key = %q, want %q", got, "tool-calls/conv-1/turn-1/call-1-arguments.json")
	}
	if got := store.ToolCallOutputKey("conv-1", "turn-1", "call-1"); got != "tool-calls/conv-1/turn-1/call-1-output.json" {
		t.Fatalf("tool output key = %q, want %q", got, "tool-calls/conv-1/turn-1/call-1-output.json")
	}
	if got := store.ContextAnchorKey("conv-1", 7); got != "context-items/conv-1/gen-000007.json" {
		t.Fatalf("anchor key = %q, want %q", got, "context-items/conv-1/gen-000007.json")
	}
}

func TestNormalizeObjectStoreReadErrorMapsMissingObjectsToDomainNotFound(t *testing.T) {
	err := normalizeObjectStoreReadError(miniosdk.ErrorResponse{Code: miniosdk.NoSuchKey})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected domain.ErrNotFound, got %v", err)
	}

	err = normalizeObjectStoreReadError(miniosdk.ErrorResponse{Code: miniosdk.NoSuchBucket})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected domain.ErrNotFound for missing bucket, got %v", err)
	}
}

func TestNormalizeObjectStoreReadErrorLeavesOtherErrorsUntouched(t *testing.T) {
	expected := errors.New("boom")
	if got := normalizeObjectStoreReadError(expected); got != expected {
		t.Fatalf("expected original error, got %v", got)
	}
}
