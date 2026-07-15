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
