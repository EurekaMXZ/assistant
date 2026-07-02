package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadPrefersMinIOEnvironmentVariables(t *testing.T) {
	t.Setenv("MINIO_ENDPOINT", "minio-primary:9000")
	t.Setenv("MINIO_REGION", "ap-southeast-1")
	t.Setenv("MINIO_BUCKET", "assistant-primary")
	t.Setenv("MINIO_ACCESS_KEY", "primary-key")
	t.Setenv("MINIO_SECRET_KEY", "primary-secret")
	t.Setenv("MINIO_USE_SSL", "true")

	cfg := Load()

	if cfg.MinIOEndpoint != "minio-primary:9000" {
		t.Fatalf("MinIOEndpoint = %q, want %q", cfg.MinIOEndpoint, "minio-primary:9000")
	}
	if cfg.MinIORegion != "ap-southeast-1" {
		t.Fatalf("MinIORegion = %q, want %q", cfg.MinIORegion, "ap-southeast-1")
	}
	if cfg.MinIOBucket != "assistant-primary" {
		t.Fatalf("MinIOBucket = %q, want %q", cfg.MinIOBucket, "assistant-primary")
	}
	if cfg.MinIOAccessKey != "primary-key" {
		t.Fatalf("MinIOAccessKey = %q, want %q", cfg.MinIOAccessKey, "primary-key")
	}
	if cfg.MinIOSecretKey != "primary-secret" {
		t.Fatalf("MinIOSecretKey = %q, want %q", cfg.MinIOSecretKey, "primary-secret")
	}
	if !cfg.MinIOUseSSL {
		t.Fatal("expected MinIOUseSSL to prefer MINIO_USE_SSL=true")
	}
}

func TestLoadUsesMinIODefaultsWhenEnvironmentIsUnset(t *testing.T) {
	t.Setenv("MINIO_ENDPOINT", "")
	t.Setenv("MINIO_REGION", "")
	t.Setenv("MINIO_BUCKET", "")
	t.Setenv("MINIO_ACCESS_KEY", "")
	t.Setenv("MINIO_SECRET_KEY", "")
	t.Setenv("MINIO_USE_SSL", "")

	cfg := Load()

	if cfg.MinIOEndpoint != "127.0.0.1:9000" {
		t.Fatalf("MinIOEndpoint = %q, want %q", cfg.MinIOEndpoint, "127.0.0.1:9000")
	}
	if cfg.MinIORegion != "us-east-1" {
		t.Fatalf("MinIORegion = %q, want %q", cfg.MinIORegion, "us-east-1")
	}
	if cfg.MinIOBucket != "assistant" {
		t.Fatalf("MinIOBucket = %q, want %q", cfg.MinIOBucket, "assistant")
	}
	if cfg.MinIOAccessKey != "assistantminio" {
		t.Fatalf("MinIOAccessKey = %q, want %q", cfg.MinIOAccessKey, "assistantminio")
	}
	if cfg.MinIOSecretKey != "assistantminio123" {
		t.Fatalf("MinIOSecretKey = %q, want %q", cfg.MinIOSecretKey, "assistantminio123")
	}
	if cfg.MinIOUseSSL {
		t.Fatal("expected MinIOUseSSL to use the default false value")
	}
}

func TestLoadReadsTavilyAPIKey(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "tavily-secret")

	cfg := Load()

	if cfg.TavilyAPIKey != "tavily-secret" {
		t.Fatalf("TavilyAPIKey = %q, want %q", cfg.TavilyAPIKey, "tavily-secret")
	}
}

func TestLoadReadsOpenAIUserAgent(t *testing.T) {
	t.Setenv("OPENAI_USER_AGENT", "assistant-test/1.0")

	cfg := Load()

	if cfg.OpenAIUserAgent != "assistant-test/1.0" {
		t.Fatalf("OpenAIUserAgent = %q, want %q", cfg.OpenAIUserAgent, "assistant-test/1.0")
	}
}

func TestLoadReadsSandboxExecEnabled(t *testing.T) {
	t.Setenv("SANDBOX_EXEC_ENABLED", "true")

	cfg := Load()

	if !cfg.SandboxExecEnabled {
		t.Fatal("expected SandboxExecEnabled to be true")
	}
}

func TestLoadReadsSandboxBridgeSettings(t *testing.T) {
	t.Setenv("SANDBOX_BRIDGE_URL", "http://127.0.0.1:8787")
	t.Setenv("SANDBOX_BRIDGE_TOKEN", "bridge-token")
	t.Setenv("SANDBOX_BRIDGE_TIMEOUT", "7s")

	cfg := Load()

	if cfg.SandboxBridgeURL != "http://127.0.0.1:8787" || cfg.SandboxBridgeToken != "bridge-token" {
		t.Fatalf("unexpected sandbox bridge settings: %+v", cfg)
	}
	if cfg.SandboxBridgeTimeout != 7*time.Second {
		t.Fatalf("SandboxBridgeTimeout = %v, want 7s", cfg.SandboxBridgeTimeout)
	}
}

func TestValidateAPIRequiresBridgeURL(t *testing.T) {
	cfg := Config{
		DatabaseURL:            "postgres://db",
		RedisAddr:              "127.0.0.1:6379",
		JWTSecret:              "jwt-secret",
		SystemUserEmail:        "system@example.com",
		SystemUserUsername:     "system",
		SystemUserPasswordHash: "hash",
		SandboxBridgeURL:       "",
	}

	err := cfg.ValidateAPI()
	if err == nil || !strings.Contains(err.Error(), "SANDBOX_BRIDGE_URL") {
		t.Fatalf("ValidateAPI error = %v, want missing SANDBOX_BRIDGE_URL", err)
	}
}

func TestValidateWorkerRequiresBridgeURL(t *testing.T) {
	cfg := validWorkerConfig()
	cfg.SandboxBridgeURL = ""

	err := cfg.ValidateWorker()
	if err == nil || !strings.Contains(err.Error(), "SANDBOX_BRIDGE_URL") {
		t.Fatalf("ValidateWorker error = %v, want missing SANDBOX_BRIDGE_URL", err)
	}
}

func TestLoadReadsRemoteToolReplayMaxBytes(t *testing.T) {
	t.Setenv("REMOTE_TOOL_REPLAY_MAX_BYTES", "4096")

	cfg := Load()

	if cfg.RemoteToolReplayMaxBytes != 4096 {
		t.Fatalf("RemoteToolReplayMaxBytes = %d, want %d", cfg.RemoteToolReplayMaxBytes, 4096)
	}
}

func TestLoadReadsAuthSettings(t *testing.T) {
	t.Setenv("AUTH_JWT_SECRET", "jwt-secret")
	t.Setenv("AUTH_JWT_ISSUER", "assistant-test")
	t.Setenv("AUTH_ACCESS_TOKEN_TTL", "12h")
	t.Setenv("SYSTEM_USER_EMAIL", "system@example.com")
	t.Setenv("SYSTEM_USER_USERNAME", "system")
	t.Setenv("SYSTEM_USER_PASSWORD_HASH", "hash-value")

	cfg := Load()

	if cfg.JWTSecret != "jwt-secret" {
		t.Fatalf("JWTSecret = %q, want %q", cfg.JWTSecret, "jwt-secret")
	}
	if cfg.JWTIssuer != "assistant-test" {
		t.Fatalf("JWTIssuer = %q, want %q", cfg.JWTIssuer, "assistant-test")
	}
	if cfg.AccessTokenTTL.String() != "12h0m0s" {
		t.Fatalf("AccessTokenTTL = %v, want 12h", cfg.AccessTokenTTL)
	}
	if cfg.SystemUserEmail != "system@example.com" || cfg.SystemUserUsername != "system" || cfg.SystemUserPasswordHash != "hash-value" {
		t.Fatalf("unexpected system user config: %+v", cfg)
	}
}

func TestLoadReadsBillingSettings(t *testing.T) {
	t.Setenv("BILLING_CURRENCY", "CNY")

	cfg := Load()

	if cfg.BillingCurrency != "CNY" {
		t.Fatalf("BillingCurrency = %q, want %q", cfg.BillingCurrency, "CNY")
	}
}

func validWorkerConfig() Config {
	return Config{
		DatabaseURL:                 "postgres://db",
		RedisAddr:                   "127.0.0.1:6379",
		KafkaBrokers:                []string{"127.0.0.1:9092"},
		MinIOAccessKey:              "minio",
		MinIOSecretKey:              "minio123",
		ProviderCredentialMasterKey: "credential-master-key",
		AgentSystemPrompt:           "system",
		AgentCompactPrompt:          "compact",
		SandboxBridgeURL:            "http://127.0.0.1:8787",
	}
}
