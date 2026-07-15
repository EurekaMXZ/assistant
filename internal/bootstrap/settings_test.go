package bootstrap

import (
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/config"
)

func TestNewBaseSettingsMapsConfig(t *testing.T) {
	cfg := config.Config{
		DatabaseURL:            "postgres://db",
		Host:                   "127.0.0.1",
		Port:                   8088,
		JWTSecret:              "jwt-secret",
		JWTIssuer:              "assistant-test",
		AccessTokenTTL:         6 * time.Hour,
		SystemUserEmail:        "system@example.com",
		SystemUserUsername:     "system",
		SystemUserPasswordHash: "$2a$10$abcdefghijklmnopqrstuvabcdefghijklmnopqrstuvabcd",
		RedisAddr:              "127.0.0.1:6379",
		RedisPassword:          "secret",
		RedisDB:                3,
		StreamChannelPrefix:    "assistant:stream",
		MinIOEndpoint:          "127.0.0.1:9000",
		MinIORegion:            "us-east-1",
		MinIOBucket:            "assistant",
		MinIOAccessKey:         "minio",
		MinIOSecretKey:         "minio123",
		MinIOUseSSL:            true,
		WebOrigin:              "https://example.com",
		ReadTimeout:            2 * time.Second,
		WriteTimeout:           3 * time.Second,
		IdleTimeout:            4 * time.Second,
		SandboxBridgeURL:       "http://127.0.0.1:8787",
		SandboxBridgeToken:     "bridge-token",
		SandboxBridgeTimeout:   9 * time.Second,
		SandboxProvider:        "firecracker",
	}

	settings := newBaseSettings(cfg, true)

	if settings.DatabaseURL != "postgres://db" {
		t.Fatalf("databaseURL = %q, want %q", settings.DatabaseURL, "postgres://db")
	}
	if settings.Address != "127.0.0.1:8088" {
		t.Fatalf("address = %q, want %q", settings.Address, "127.0.0.1:8088")
	}
	if !settings.EnableAuth {
		t.Fatal("expected auth to be enabled in base settings")
	}
	if settings.Stream.Addr != "127.0.0.1:6379" || settings.Stream.Password != "secret" || settings.Stream.DB != 3 || settings.Stream.ChannelPrefix != "assistant:stream" {
		t.Fatalf("unexpected stream settings: %+v", settings.Stream)
	}
	if settings.Server.Address != "127.0.0.1:8088" || settings.Server.WebOrigin != "https://example.com" {
		t.Fatalf("unexpected server settings: %+v", settings.Server)
	}
	if settings.Auth.Secret != "jwt-secret" || settings.Auth.Issuer != "assistant-test" || settings.Auth.AccessTokenTTL != 6*time.Hour {
		t.Fatalf("unexpected auth settings: %+v", settings.Auth)
	}
	if settings.SystemUser.Email != "system@example.com" || settings.SystemUser.Username != "system" {
		t.Fatalf("unexpected system user settings: %+v", settings.SystemUser)
	}
	if settings.MinIO.Bucket != "assistant" || !settings.MinIO.UseSSL {
		t.Fatalf("unexpected minio settings: %+v", settings.MinIO)
	}
	if settings.Sandbox.Provider != "firecracker" || settings.Sandbox.HTTP.BaseURL != "http://127.0.0.1:8787" || settings.Sandbox.HTTP.Token != "bridge-token" || settings.Sandbox.HTTP.HTTPClientTimeout != 9*time.Second {
		t.Fatalf("unexpected sandbox settings: %+v", settings.Sandbox)
	}
}

func TestNewWorkerSettingsMapsConfig(t *testing.T) {
	cfg := config.Config{
		CacheMaxConversations:    11,
		CacheTailCapacity:        22,
		OpenAIUserAgent:          "assistant-test/1.0",
		HTTPClientTimeout:        5 * time.Second,
		TavilyAPIKey:             "tavily-secret",
		SandboxExecEnabled:       true,
		SandboxBridgeURL:         "http://127.0.0.1:8787",
		SandboxBridgeToken:       "bridge-token",
		SandboxBridgeTimeout:     7 * time.Second,
		SandboxProvider:          "firecracker",
		MinIOEndpoint:            "127.0.0.1:9000",
		MinIORegion:              "us-east-1",
		MinIOBucket:              "assistant",
		MinIOAccessKey:           "minio",
		MinIOSecretKey:           "minio123",
		MinIOUseSSL:              true,
		KafkaBrokers:             []string{"127.0.0.1:9092"},
		KafkaWorkflowTopic:       "assistant.workflow",
		KafkaConsumerGroup:       "assistant-workers",
		WorkerConcurrency:        4,
		WorkerPollInterval:       2 * time.Second,
		WorkerLeaseTimeout:       3 * time.Minute,
		AgentSystemPrompt:        "system",
		AgentCompactPrompt:       "compact",
		RemoteToolReplayMaxBytes: 4096,
		CompactMaxOutputTokens:   800,
		CompactTriggerTokens:     12000,
		OutboxBatchSize:          88,
	}

	settings := newWorkerSettings(cfg)

	if settings.CacheMaxConversations != 11 || settings.CacheTailCapacity != 22 {
		t.Fatalf("unexpected cache settings: %+v", settings)
	}
	if settings.OpenAI.UserAgent != "assistant-test/1.0" {
		t.Fatalf("unexpected openai settings: %+v", settings.OpenAI)
	}
	if settings.Tavily.APIKey != "tavily-secret" || settings.Tavily.HTTPClientTimeout != 5*time.Second {
		t.Fatalf("unexpected tavily settings: %+v", settings.Tavily)
	}
	if !settings.SandboxExecEnabled {
		t.Fatalf("expected sandbox exec enabled in worker settings: %+v", settings)
	}
	if settings.Sandbox.Provider != "firecracker" || settings.Sandbox.HTTP.BaseURL != "http://127.0.0.1:8787" || settings.Sandbox.HTTP.Token != "bridge-token" || settings.Sandbox.HTTP.HTTPClientTimeout != 7*time.Second {
		t.Fatalf("unexpected sandbox bridge settings: %+v", settings.Sandbox)
	}
	if settings.MinIO.Bucket != "assistant" || !settings.MinIO.UseSSL {
		t.Fatalf("unexpected minio settings: %+v", settings.MinIO)
	}
	if len(settings.Kafka.Brokers) != 1 || settings.Kafka.WorkflowTopic != "assistant.workflow" {
		t.Fatalf("unexpected kafka settings: %+v", settings.Kafka)
	}
	if settings.KafkaReader.ConsumerGroup != "assistant-workers" {
		t.Fatalf("unexpected kafka reader settings: %+v", settings.KafkaReader)
	}
	if settings.Workflow.OutboxBatchSize != 88 || settings.Workflow.RemoteToolReplayMaxBytes != 4096 {
		t.Fatalf("unexpected workflow settings: %+v", settings.Workflow)
	}
	if settings.Process.WorkerLeaseTimeout != 3*time.Minute || settings.Process.WorkerConcurrency != 4 {
		t.Fatalf("unexpected worker process settings: %+v", settings.Process)
	}
}
