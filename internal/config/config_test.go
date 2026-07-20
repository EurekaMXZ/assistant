package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadReadsS3EnvironmentVariables(t *testing.T) {
	t.Setenv("S3_PROVIDER", "r2")
	t.Setenv("S3_ENDPOINT", "account.r2.cloudflarestorage.com")
	t.Setenv("S3_PUBLIC_ENDPOINT", "https://objects.example.com")
	t.Setenv("S3_REGION", "auto")
	t.Setenv("S3_BUCKET", "assistant-primary")
	t.Setenv("S3_ACCESS_KEY", "primary-key")
	t.Setenv("S3_SECRET_KEY", "primary-secret")
	t.Setenv("S3_USE_SSL", "true")
	t.Setenv("S3_BUCKET_LOOKUP", "path")
	t.Setenv("S3_AUTO_CREATE_BUCKET", "false")
	t.Setenv("S3_PRESIGN_TTL", "20m")

	cfg := Load()

	if cfg.S3Provider != "r2" || cfg.S3Endpoint != "account.r2.cloudflarestorage.com" {
		t.Fatalf("unexpected S3 provider settings: %+v", cfg)
	}
	if cfg.S3PublicEndpoint != "https://objects.example.com" || cfg.S3Region != "auto" {
		t.Fatalf("unexpected S3 endpoint settings: %+v", cfg)
	}
	if cfg.S3Bucket != "assistant-primary" {
		t.Fatalf("S3Bucket = %q, want %q", cfg.S3Bucket, "assistant-primary")
	}
	if cfg.S3AccessKey != "primary-key" || cfg.S3SecretKey != "primary-secret" {
		t.Fatalf("unexpected S3 credentials")
	}
	if !cfg.S3UseSSL || cfg.S3AutoCreateBucket || cfg.S3BucketLookup != "path" || cfg.S3PresignTTL != 20*time.Minute {
		t.Fatalf("unexpected S3 behavior settings: %+v", cfg)
	}
}

func TestLoadUsesLocalS3DefaultsWhenEnvironmentIsUnset(t *testing.T) {
	for _, key := range []string{"S3_PROVIDER", "S3_ENDPOINT", "S3_PUBLIC_ENDPOINT", "S3_REGION", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_USE_SSL", "S3_BUCKET_LOOKUP", "S3_AUTO_CREATE_BUCKET", "S3_PRESIGN_TTL"} {
		t.Setenv(key, "")
	}

	cfg := Load()

	if cfg.S3Provider != "minio" || cfg.S3Endpoint != "127.0.0.1:9000" {
		t.Fatalf("unexpected S3 defaults: %+v", cfg)
	}
	if cfg.S3Region != "us-east-1" {
		t.Fatalf("S3Region = %q, want %q", cfg.S3Region, "us-east-1")
	}
	if cfg.S3Bucket != "assistant" {
		t.Fatalf("S3Bucket = %q, want %q", cfg.S3Bucket, "assistant")
	}
	if cfg.S3AccessKey != "assistantminio" || cfg.S3SecretKey != "assistantminio123" {
		t.Fatalf("unexpected S3 credential defaults")
	}
	if cfg.S3UseSSL || !cfg.S3AutoCreateBucket {
		t.Fatalf("unexpected local S3 defaults: %+v", cfg)
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

func TestLoadDoesNotDefaultWebOrigin(t *testing.T) {
	t.Setenv("WEB_ORIGIN", "")

	if got := Load().WebOrigin; got != "" {
		t.Fatalf("WebOrigin = %q, want empty value", got)
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

func TestLoadReadsCubeSandboxSettings(t *testing.T) {
	t.Setenv("SANDBOX_PROVIDER", "cubesandbox")
	t.Setenv("SANDBOX_CUBE_API_URL", "https://cube-api.internal")
	t.Setenv("SANDBOX_CUBE_API_KEY", "cube-key")
	t.Setenv("SANDBOX_CUBE_TEMPLATE_ID", "tpl-1")
	t.Setenv("SANDBOX_CUBE_PROXY_NODE_IP", "10.0.0.12")
	t.Setenv("SANDBOX_CUBE_PROXY_PORT_HTTP", "443")
	t.Setenv("SANDBOX_CUBE_PROXY_SCHEME", "https")
	t.Setenv("SANDBOX_CUBE_DOMAIN", "cube.internal")
	t.Setenv("SANDBOX_CUBE_CLUSTER_ID", "cluster-1")
	t.Setenv("SANDBOX_CUBE_REQUEST_TIMEOUT", "45s")
	t.Setenv("SANDBOX_CUBE_PAUSE_TIMEOUT", "50s")
	t.Setenv("SANDBOX_CUBE_MAX_OUTPUT_BYTES", "2048")
	t.Setenv("SANDBOX_CUBE_ALLOW_INTERNET", "true")
	t.Setenv("SANDBOX_CUBE_ALLOW_OUT", "api.example.com, 10.0.0.0/8")
	t.Setenv("SANDBOX_CUBE_DENY_OUT", "0.0.0.0/0")

	cfg := Load()
	if cfg.SandboxProvider != "cubesandbox" || cfg.SandboxCubeAPIURL != "https://cube-api.internal" || cfg.SandboxCubeAPIKey != "cube-key" || cfg.SandboxCubeTemplateID != "tpl-1" {
		t.Fatalf("unexpected cube sandbox control settings: %+v", cfg)
	}
	if cfg.SandboxCubeProxyNodeIP != "10.0.0.12" || cfg.SandboxCubeProxyPortHTTP != 443 || cfg.SandboxCubeProxyScheme != "https" || cfg.SandboxCubeDomain != "cube.internal" {
		t.Fatalf("unexpected cube sandbox proxy settings: %+v", cfg)
	}
	if cfg.SandboxCubeRequestTimeout != 45*time.Second || cfg.SandboxCubePauseTimeout != 50*time.Second || cfg.SandboxCubeMaxOutputBytes != 2048 || !cfg.SandboxCubeAllowInternet {
		t.Fatalf("unexpected cube sandbox runtime settings: %+v", cfg)
	}
	if len(cfg.SandboxCubeAllowOut) != 2 || len(cfg.SandboxCubeDenyOut) != 1 {
		t.Fatalf("unexpected cube sandbox network settings: %+v", cfg)
	}
}

func TestValidateAPIRequiresBridgeURL(t *testing.T) {
	cfg := validAPIConfig()
	cfg.SandboxBridgeURL = ""

	err := cfg.ValidateAPI()
	if err == nil || !strings.Contains(err.Error(), "SANDBOX_BRIDGE_URL") {
		t.Fatalf("ValidateAPI error = %v, want missing SANDBOX_BRIDGE_URL", err)
	}
}

func TestValidateAPIRequiresWebOrigin(t *testing.T) {
	cfg := validAPIConfig()
	cfg.WebOrigin = ""

	err := cfg.ValidateAPI()
	if err == nil || !strings.Contains(err.Error(), "WEB_ORIGIN") {
		t.Fatalf("ValidateAPI error = %v, want missing WEB_ORIGIN", err)
	}
}

func TestValidateAPIRejectsInvalidWebOrigin(t *testing.T) {
	for _, value := range []string{"localhost:3000", "ftp://example.com", "https://example.com/app", "https://example.com?source=mail"} {
		t.Run(value, func(t *testing.T) {
			cfg := validAPIConfig()
			cfg.WebOrigin = value
			if err := cfg.ValidateAPI(); err == nil || !strings.Contains(err.Error(), "WEB_ORIGIN") {
				t.Fatalf("ValidateAPI error = %v, want invalid WEB_ORIGIN", err)
			}
		})
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

func TestValidateWorkerRejectsRemovedAgentBayProvider(t *testing.T) {
	cfg := validWorkerConfig()
	cfg.SandboxProvider = "agentbay"

	err := cfg.ValidateWorker()
	if err == nil || !strings.Contains(err.Error(), "SANDBOX_PROVIDER must be firecracker or cubesandbox") {
		t.Fatalf("ValidateWorker error = %v, want removed AgentBay provider rejection", err)
	}
}

func TestValidateAPIRequiresCubeSandboxSettings(t *testing.T) {
	for name, clear := range map[string]func(*Config){
		"api url":     func(cfg *Config) { cfg.SandboxCubeAPIURL = "" },
		"api key":     func(cfg *Config) { cfg.SandboxCubeAPIKey = "" },
		"template id": func(cfg *Config) { cfg.SandboxCubeTemplateID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := validCubeAPIConfig()
			clear(&cfg)
			if err := cfg.ValidateAPI(); err == nil || !strings.Contains(err.Error(), "SANDBOX_CUBE_") {
				t.Fatalf("ValidateAPI error = %v, want missing CubeSandbox setting", err)
			}
		})
	}
}

func TestValidateWorkerAcceptsCubeSandboxWithoutFirecrackerBridge(t *testing.T) {
	cfg := validCubeWorkerConfig()
	cfg.SandboxBridgeURL = ""
	if err := cfg.ValidateWorker(); err != nil {
		t.Fatalf("ValidateWorker: %v", err)
	}
}

func TestValidateWorkerRejectsUnsafeCubeSandboxDomainAllowList(t *testing.T) {
	cfg := validCubeWorkerConfig()
	cfg.SandboxCubeAllowInternet = true
	cfg.SandboxCubeAllowOut = []string{"api.example.com"}
	cfg.SandboxCubeDenyOut = nil
	if err := cfg.ValidateWorker(); err == nil || !strings.Contains(err.Error(), "SANDBOX_CUBE_ALLOW_OUT domains") {
		t.Fatalf("ValidateWorker error = %v, want unsafe domain allow-list rejection", err)
	}
	cfg.SandboxCubeDenyOut = []string{"0.0.0.0/0"}
	if err := cfg.ValidateWorker(); err != nil {
		t.Fatalf("ValidateWorker with deny-all: %v", err)
	}
}

func TestLoadReadsRemoteToolReplayMaxBytes(t *testing.T) {
	t.Setenv("REMOTE_TOOL_REPLAY_MAX_BYTES", "4096")
	t.Setenv("AGENT_MODEL_TOOL_OUTPUT_MAX_TOKENS", "9000")

	cfg := Load()

	if cfg.RemoteToolReplayMaxBytes != 4096 {
		t.Fatalf("RemoteToolReplayMaxBytes = %d, want %d", cfg.RemoteToolReplayMaxBytes, 4096)
	}
	if cfg.ModelToolOutputMaxTokens != 9000 {
		t.Fatalf("ModelToolOutputMaxTokens = %d, want %d", cfg.ModelToolOutputMaxTokens, 9000)
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
		S3Provider:                  "minio",
		S3Endpoint:                  "127.0.0.1:9000",
		S3Bucket:                    "assistant",
		S3AccessKey:                 "minio",
		S3SecretKey:                 "minio123",
		S3BucketLookup:              "auto",
		S3PresignTTL:                15 * time.Minute,
		S3PendingUploadTTL:          24 * time.Hour,
		S3UploadReaperInterval:      5 * time.Minute,
		S3UploadReaperBatchSize:     100,
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

func validAPIConfig() Config {
	cfg := validWorkerConfig()
	cfg.WebOrigin = "https://assistant.example.com"
	cfg.JWTSecret = "jwt-secret"
	cfg.SystemUserEmail = "system@example.com"
	cfg.SystemUserUsername = "system"
	cfg.SystemUserPasswordHash = "hash"
	return cfg
}

func validCubeWorkerConfig() Config {
	cfg := validWorkerConfig()
	cfg.SandboxProvider = "cubesandbox"
	cfg.SandboxCubeAPIURL = "https://cube-api.internal"
	cfg.SandboxCubeAPIKey = "cube-key"
	cfg.SandboxCubeTemplateID = "tpl-1"
	cfg.SandboxCubeProxyPortHTTP = 80
	cfg.SandboxCubeRequestTimeout = 30 * time.Second
	cfg.SandboxCubePauseTimeout = 30 * time.Second
	cfg.SandboxCubeMaxOutputBytes = 1 << 20
	return cfg
}

func validCubeAPIConfig() Config {
	cfg := validCubeWorkerConfig()
	cfg.WebOrigin = "https://assistant.example.com"
	cfg.JWTSecret = "jwt-secret"
	cfg.SystemUserEmail = "system@example.com"
	cfg.SystemUserUsername = "system"
	cfg.SystemUserPasswordHash = "hash"
	return cfg
}
