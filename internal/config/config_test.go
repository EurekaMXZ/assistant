package config

import (
	"os"
	"path/filepath"
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

func TestLoadReadsSandboxLifecycleSettings(t *testing.T) {
	t.Setenv("SANDBOX_IDLE_STOP_AFTER", "20m")
	t.Setenv("SANDBOX_STOPPED_RETENTION", "48h")
	t.Setenv("SANDBOX_REAPER_INTERVAL", "2m")
	t.Setenv("SANDBOX_REAPER_BATCH_SIZE", "12")

	cfg := Load()
	if cfg.SandboxIdleStopAfter != 20*time.Minute || cfg.SandboxStoppedRetention != 48*time.Hour || cfg.SandboxReaperInterval != 2*time.Minute || cfg.SandboxReaperBatchSize != 12 {
		t.Fatalf("unexpected sandbox lifecycle settings: %+v", cfg)
	}
}

func TestLoadReadsSandboxBridgeSettings(t *testing.T) {
	t.Setenv("SANDBOX_PROVIDER", "firecracker")
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

func TestLoadReadsAgentBaySettings(t *testing.T) {
	t.Setenv("SANDBOX_PROVIDER", "agentbay")
	t.Setenv("AGENTBAY_API_KEY", "agentbay-key")
	t.Setenv("AGENTBAY_REGION_ID", "ap-southeast-1")
	t.Setenv("AGENTBAY_IMAGE_ID", "linux_latest")
	t.Setenv("AGENTBAY_POLICY_ID", "policy-1")
	t.Setenv("AGENTBAY_API_TIMEOUT", "45s")

	cfg := Load()

	if cfg.SandboxProvider != "agentbay" || cfg.AgentBayAPIKey != "agentbay-key" {
		t.Fatalf("unexpected AgentBay provider settings: %+v", cfg)
	}
	if cfg.AgentBayRegionID != "ap-southeast-1" || cfg.AgentBayImageID != "linux_latest" || cfg.AgentBayPolicyID != "policy-1" {
		t.Fatalf("unexpected AgentBay session settings: %+v", cfg)
	}
	if cfg.AgentBayAPITimeout != 45*time.Second {
		t.Fatalf("AgentBayAPITimeout = %v, want 45s", cfg.AgentBayAPITimeout)
	}
}

func TestValidateAPIRequiresBridgeURL(t *testing.T) {
	cfg := Config{
		DatabaseURL:              "postgres://db",
		RedisAddr:                "127.0.0.1:6379",
		JWTSecret:                "jwt-secret",
		SystemUserEmail:          "system@example.com",
		SystemUserUsername:       "system",
		SystemUserPasswordHash:   "hash",
		SandboxBridgeURL:         "",
		SandboxIdleStopAfter:     15 * time.Minute,
		SandboxStoppedRetention:  24 * time.Hour,
		SandboxReaperInterval:    time.Minute,
		SandboxReaperBatchSize:   20,
		SandboxCommandTimeout:    30 * time.Second,
		SandboxCommandMaxTimeout: 5 * time.Minute,
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

func TestValidateWorkerAcceptsAgentBayWithoutBridge(t *testing.T) {
	cfg := validWorkerConfig()
	cfg.SandboxProvider = "agentbay"
	cfg.SandboxBridgeURL = ""
	cfg.AgentBayAPIKey = "agentbay-key"

	if err := cfg.ValidateWorker(); err != nil {
		t.Fatalf("ValidateWorker: %v", err)
	}
}

func TestValidateWorkerRequiresAgentBayAPIKey(t *testing.T) {
	cfg := validWorkerConfig()
	cfg.SandboxProvider = "agentbay"
	cfg.AgentBayAPIKey = ""

	err := cfg.ValidateWorker()
	if err == nil || !strings.Contains(err.Error(), "AGENTBAY_API_KEY") {
		t.Fatalf("ValidateWorker error = %v, want missing AGENTBAY_API_KEY", err)
	}
}

func TestValidateWorkerRejectsUnknownSandboxProvider(t *testing.T) {
	cfg := validWorkerConfig()
	cfg.SandboxProvider = "unknown"

	err := cfg.ValidateWorker()
	if err == nil || !strings.Contains(err.Error(), "SANDBOX_PROVIDER must be firecracker or agentbay") {
		t.Fatalf("ValidateWorker error = %v, want unknown sandbox provider", err)
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

func TestLoadReadsPromptFilePaths(t *testing.T) {
	t.Setenv("AGENT_SYSTEM_PROMPT_FILE", "/etc/assistant/system.md")
	t.Setenv("AGENT_COMPACT_PROMPT_FILE", "/etc/assistant/compact.md")

	cfg := Load()

	if cfg.AgentSystemPromptFile != "/etc/assistant/system.md" {
		t.Fatalf("AgentSystemPromptFile = %q", cfg.AgentSystemPromptFile)
	}
	if cfg.AgentCompactPromptFile != "/etc/assistant/compact.md" {
		t.Fatalf("AgentCompactPromptFile = %q", cfg.AgentCompactPromptFile)
	}
}

func TestLoadPromptsReadsAndTrimsMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.md")
	compactPath := filepath.Join(dir, "compact.md")
	if err := os.WriteFile(systemPath, []byte("\n# System\n\nDo the work.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(compactPath, []byte("\n# Compact\n\nSummarize.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := (Config{
		AgentSystemPromptFile:  systemPath,
		AgentCompactPromptFile: compactPath,
	}).LoadPrompts()
	if err != nil {
		t.Fatalf("LoadPrompts: %v", err)
	}
	if cfg.AgentSystemPrompt != "# System\n\nDo the work." {
		t.Fatalf("AgentSystemPrompt = %q", cfg.AgentSystemPrompt)
	}
	if cfg.AgentCompactPrompt != "# Compact\n\nSummarize." {
		t.Fatalf("AgentCompactPrompt = %q", cfg.AgentCompactPrompt)
	}
}

func TestLoadPromptsRejectsEmptyMarkdownFile(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.md")
	compactPath := filepath.Join(dir, "compact.md")
	if err := os.WriteFile(systemPath, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(compactPath, []byte("compact"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := (Config{
		AgentSystemPromptFile:  systemPath,
		AgentCompactPromptFile: compactPath,
	}).LoadPrompts()
	if err == nil || !strings.Contains(err.Error(), "system prompt") {
		t.Fatalf("LoadPrompts error = %v", err)
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
		AgentSystemPromptFile:       "prompts/system.md",
		AgentCompactPromptFile:      "prompts/compact.md",
		SandboxBridgeURL:            "http://127.0.0.1:8787",
		SandboxIdleStopAfter:        15 * time.Minute,
		SandboxStoppedRetention:     24 * time.Hour,
		SandboxReaperInterval:       time.Minute,
		SandboxReaperBatchSize:      20,
		SandboxCommandTimeout:       30 * time.Second,
		SandboxCommandMaxTimeout:    5 * time.Minute,
	}
}
