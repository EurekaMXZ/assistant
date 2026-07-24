package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultHost                      = "0.0.0.0"
	defaultPort                      = 8080
	defaultReadTimeout               = 15 * time.Second
	defaultWriteTimeout              = 15 * time.Second
	defaultIdleTimeout               = 60 * time.Second
	defaultShutdownTimeout           = 10 * time.Second
	defaultDatabaseURL               = "postgres://assistant:assistant@127.0.0.1:5432/assistant?sslmode=disable"
	defaultWorkerPollInterval        = 2 * time.Second
	defaultWorkerLeaseTimeout        = 2 * time.Minute
	defaultWorkerConcurrency         = 4
	defaultKafkaTopic                = "assistant.workflow"
	defaultKafkaStreamTopic          = "assistant.stream"
	defaultKafkaGroup                = "assistant-workers"
	defaultRedisAddr                 = "127.0.0.1:6379"
	defaultRedisDB                   = 0
	defaultStreamChannelPrefix       = "assistant:stream"
	defaultStreamReplayTTL           = time.Hour
	defaultS3Provider                = "minio"
	defaultS3Endpoint                = "127.0.0.1:9000"
	defaultS3Region                  = "us-east-1"
	defaultS3Bucket                  = "assistant"
	defaultS3UseSSL                  = false
	defaultS3AccessKey               = "assistantminio"
	defaultS3SecretKey               = "assistantminio123"
	defaultS3PresignTTL              = 15 * time.Minute
	defaultS3PendingUploadTTL        = 24 * time.Hour
	defaultS3UploadReaperInterval    = 5 * time.Minute
	defaultS3UploadReaperBatchSize   = 100
	defaultOpenAIUserAgent           = "assistant"
	defaultRemoteToolReplayMaxBytes  = 16384
	defaultModelToolOutputMaxTokens  = 10000
	defaultCompactOutputTokens       = 1536
	defaultCompactTriggerTokens      = 0
	defaultCacheMaxConversations     = 1024
	defaultCacheTailCapacity         = 256
	defaultOutboxBatchSize           = 100
	defaultHTTPClientTimeout         = 5 * time.Minute
	defaultJWTIssuer                 = "assistant"
	defaultAccessTokenTTL            = 24 * time.Hour
	defaultSandboxProvider           = "firecracker"
	defaultSandboxBridgeTimeout      = time.Minute
	defaultSandboxIdleStopAfter      = 15 * time.Minute
	defaultSandboxStoppedRetention   = 24 * time.Hour
	defaultSandboxReaperInterval     = time.Minute
	defaultSandboxReaperBatchSize    = 20
	defaultSandboxCommandTimeout     = 30 * time.Second
	defaultSandboxCommandMaxTimeout  = 5 * time.Minute
	defaultSandboxCubeProxyPort      = 80
	defaultSandboxCubeRequestTimeout = 30 * time.Second
	defaultSandboxCubePauseTimeout   = 30 * time.Second
	defaultSandboxCubeMaxOutputBytes = 1 << 20
	defaultAgentSystemPromptFile     = "prompts/system.md"
	defaultAgentCompactPromptFile    = "prompts/compact.md"
	defaultImageGenerationPartials   = 2
	defaultImagePreviewTTL           = 24 * time.Hour
)

type Config struct {
	Host                        string
	Port                        int
	ReadTimeout                 time.Duration
	WriteTimeout                time.Duration
	IdleTimeout                 time.Duration
	ShutdownTimeout             time.Duration
	WorkerPollInterval          time.Duration
	WorkerLeaseTimeout          time.Duration
	WorkerConcurrency           int
	OutboxBatchSize             int
	WebOrigin                   string
	JWTSecret                   string
	JWTIssuer                   string
	AccessTokenTTL              time.Duration
	SystemUserEmail             string
	SystemUserUsername          string
	SystemUserPasswordHash      string
	DatabaseURL                 string
	MigrationsPath              string
	RedisAddr                   string
	RedisPassword               string
	RedisDB                     int
	StreamChannelPrefix         string
	StreamReplayTTL             time.Duration
	KafkaBrokers                []string
	KafkaWorkflowTopic          string
	KafkaStreamTopic            string
	KafkaConsumerGroup          string
	S3Provider                  string
	S3Endpoint                  string
	S3PublicEndpoint            string
	S3Region                    string
	S3Bucket                    string
	S3AccessKey                 string
	S3SecretKey                 string
	S3SessionToken              string
	S3UseSSL                    bool
	S3BucketLookup              string
	S3AutoCreateBucket          bool
	S3PresignTTL                time.Duration
	S3PendingUploadTTL          time.Duration
	S3UploadReaperInterval      time.Duration
	S3UploadReaperBatchSize     int
	OpenAIUserAgent             string
	ProviderCredentialMasterKey string
	BillingCurrency             string
	TavilyAPIKey                string
	SandboxExecEnabled          bool
	SandboxProvider             string
	SandboxBridgeURL            string
	SandboxBridgeToken          string
	SandboxBridgeTimeout        time.Duration
	SandboxCubeAPIURL           string
	SandboxCubeAPIKey           string
	SandboxCubeTemplateID       string
	SandboxCubeProxyNodeIP      string
	SandboxCubeProxyPortHTTP    int
	SandboxCubeProxyScheme      string
	SandboxCubeDomain           string
	SandboxCubeClusterID        string
	SandboxCubeRequestTimeout   time.Duration
	SandboxCubePauseTimeout     time.Duration
	SandboxCubeMaxOutputBytes   int
	SandboxCubeAllowInternet    bool
	SandboxCubeAllowOut         []string
	SandboxCubeDenyOut          []string
	SandboxIdleStopAfter        time.Duration
	SandboxStoppedRetention     time.Duration
	SandboxReaperInterval       time.Duration
	SandboxReaperBatchSize      int
	SandboxCommandTimeout       time.Duration
	SandboxCommandMaxTimeout    time.Duration
	AgentSystemPromptFile       string
	AgentCompactPromptFile      string
	AgentSystemPrompt           string
	AgentCompactPrompt          string
	RemoteToolReplayMaxBytes    int
	ModelToolOutputMaxTokens    int
	CompactMaxOutputTokens      int
	CompactTriggerTokens        int
	CacheMaxConversations       int
	CacheTailCapacity           int
	HTTPClientTimeout           time.Duration
	ImageGenerationPartials     int
	ImagePreviewTTL             time.Duration
}

func Load() Config {
	_ = godotenv.Load()
	s3Provider := getenv("S3_PROVIDER", defaultS3Provider)

	return Config{
		Host:                        getenv("APP_HOST", defaultHost),
		Port:                        getenvInt("PORT", defaultPort),
		ReadTimeout:                 getenvDuration("READ_TIMEOUT", defaultReadTimeout),
		WriteTimeout:                getenvDuration("WRITE_TIMEOUT", defaultWriteTimeout),
		IdleTimeout:                 getenvDuration("IDLE_TIMEOUT", defaultIdleTimeout),
		ShutdownTimeout:             getenvDuration("SHUTDOWN_TIMEOUT", defaultShutdownTimeout),
		WorkerPollInterval:          getenvDuration("WORKER_POLL_INTERVAL", defaultWorkerPollInterval),
		WorkerLeaseTimeout:          getenvDuration("WORKER_LEASE_TIMEOUT", defaultWorkerLeaseTimeout),
		WorkerConcurrency:           getenvInt("WORKER_CONCURRENCY", defaultWorkerConcurrency),
		OutboxBatchSize:             getenvInt("OUTBOX_BATCH_SIZE", defaultOutboxBatchSize),
		WebOrigin:                   strings.TrimSpace(os.Getenv("WEB_ORIGIN")),
		JWTSecret:                   os.Getenv("AUTH_JWT_SECRET"),
		JWTIssuer:                   getenv("AUTH_JWT_ISSUER", defaultJWTIssuer),
		AccessTokenTTL:              getenvDuration("AUTH_ACCESS_TOKEN_TTL", defaultAccessTokenTTL),
		SystemUserEmail:             os.Getenv("SYSTEM_USER_EMAIL"),
		SystemUserUsername:          os.Getenv("SYSTEM_USER_USERNAME"),
		SystemUserPasswordHash:      os.Getenv("SYSTEM_USER_PASSWORD_HASH"),
		DatabaseURL:                 getenv("DATABASE_URL", defaultDatabaseURL),
		MigrationsPath:              getenv("MIGRATIONS_PATH", "file://db/migrations"),
		RedisAddr:                   getenv("REDIS_ADDR", defaultRedisAddr),
		RedisPassword:               os.Getenv("REDIS_PASSWORD"),
		RedisDB:                     getenvInt("REDIS_DB", defaultRedisDB),
		StreamChannelPrefix:         getenv("STREAM_CHANNEL_PREFIX", defaultStreamChannelPrefix),
		StreamReplayTTL:             getenvDuration("STREAM_REPLAY_TTL", defaultStreamReplayTTL),
		KafkaBrokers:                getenvList("KAFKA_BROKERS", []string{"127.0.0.1:9092"}),
		KafkaWorkflowTopic:          getenv("KAFKA_WORKFLOW_TOPIC", defaultKafkaTopic),
		KafkaStreamTopic:            getenv("KAFKA_STREAM_TOPIC", defaultKafkaStreamTopic),
		KafkaConsumerGroup:          getenv("KAFKA_CONSUMER_GROUP", defaultKafkaGroup),
		S3Provider:                  s3Provider,
		S3Endpoint:                  getenv("S3_ENDPOINT", defaultS3Endpoint),
		S3PublicEndpoint:            strings.TrimSpace(os.Getenv("S3_PUBLIC_ENDPOINT")),
		S3Region:                    getenv("S3_REGION", defaultS3Region),
		S3Bucket:                    getenv("S3_BUCKET", defaultS3Bucket),
		S3AccessKey:                 getenv("S3_ACCESS_KEY", defaultS3AccessKey),
		S3SecretKey:                 getenv("S3_SECRET_KEY", defaultS3SecretKey),
		S3SessionToken:              strings.TrimSpace(os.Getenv("S3_SESSION_TOKEN")),
		S3UseSSL:                    getenvBool("S3_USE_SSL", defaultS3UseSSL),
		S3BucketLookup:              getenv("S3_BUCKET_LOOKUP", "auto"),
		S3AutoCreateBucket:          getenvBool("S3_AUTO_CREATE_BUCKET", strings.EqualFold(s3Provider, defaultS3Provider)),
		S3PresignTTL:                getenvDuration("S3_PRESIGN_TTL", defaultS3PresignTTL),
		S3PendingUploadTTL:          getenvDuration("S3_PENDING_UPLOAD_TTL", defaultS3PendingUploadTTL),
		S3UploadReaperInterval:      getenvDuration("S3_UPLOAD_REAPER_INTERVAL", defaultS3UploadReaperInterval),
		S3UploadReaperBatchSize:     getenvInt("S3_UPLOAD_REAPER_BATCH_SIZE", defaultS3UploadReaperBatchSize),
		OpenAIUserAgent:             getenv("OPENAI_USER_AGENT", defaultOpenAIUserAgent),
		ProviderCredentialMasterKey: os.Getenv("PROVIDER_CREDENTIAL_MASTER_KEY"),
		BillingCurrency:             getenv("BILLING_CURRENCY", "USD"),
		TavilyAPIKey:                os.Getenv("TAVILY_API_KEY"),
		SandboxExecEnabled:          getenvBool("SANDBOX_EXEC_ENABLED", false),
		SandboxProvider:             getenv("SANDBOX_PROVIDER", defaultSandboxProvider),
		SandboxBridgeURL:            os.Getenv("SANDBOX_BRIDGE_URL"),
		SandboxBridgeToken:          os.Getenv("SANDBOX_BRIDGE_TOKEN"),
		SandboxBridgeTimeout:        getenvDuration("SANDBOX_BRIDGE_TIMEOUT", defaultSandboxBridgeTimeout),
		SandboxCubeAPIURL:           os.Getenv("SANDBOX_CUBE_API_URL"),
		SandboxCubeAPIKey:           os.Getenv("SANDBOX_CUBE_API_KEY"),
		SandboxCubeTemplateID:       os.Getenv("SANDBOX_CUBE_TEMPLATE_ID"),
		SandboxCubeProxyNodeIP:      os.Getenv("SANDBOX_CUBE_PROXY_NODE_IP"),
		SandboxCubeProxyPortHTTP:    getenvInt("SANDBOX_CUBE_PROXY_PORT_HTTP", defaultSandboxCubeProxyPort),
		SandboxCubeProxyScheme:      getenv("SANDBOX_CUBE_PROXY_SCHEME", "http"),
		SandboxCubeDomain:           getenv("SANDBOX_CUBE_DOMAIN", "cube.app"),
		SandboxCubeClusterID:        getenv("SANDBOX_CUBE_CLUSTER_ID", "default"),
		SandboxCubeRequestTimeout:   getenvDuration("SANDBOX_CUBE_REQUEST_TIMEOUT", defaultSandboxCubeRequestTimeout),
		SandboxCubePauseTimeout:     getenvDuration("SANDBOX_CUBE_PAUSE_TIMEOUT", defaultSandboxCubePauseTimeout),
		SandboxCubeMaxOutputBytes:   getenvInt("SANDBOX_CUBE_MAX_OUTPUT_BYTES", defaultSandboxCubeMaxOutputBytes),
		SandboxCubeAllowInternet:    getenvBool("SANDBOX_CUBE_ALLOW_INTERNET", false),
		SandboxCubeAllowOut:         getenvList("SANDBOX_CUBE_ALLOW_OUT", nil),
		SandboxCubeDenyOut:          getenvList("SANDBOX_CUBE_DENY_OUT", nil),
		SandboxIdleStopAfter:        getenvDuration("SANDBOX_IDLE_STOP_AFTER", defaultSandboxIdleStopAfter),
		SandboxStoppedRetention:     getenvDuration("SANDBOX_STOPPED_RETENTION", defaultSandboxStoppedRetention),
		SandboxReaperInterval:       getenvDuration("SANDBOX_REAPER_INTERVAL", defaultSandboxReaperInterval),
		SandboxReaperBatchSize:      getenvInt("SANDBOX_REAPER_BATCH_SIZE", defaultSandboxReaperBatchSize),
		SandboxCommandTimeout:       getenvDuration("SANDBOX_COMMAND_DEFAULT_TIMEOUT", defaultSandboxCommandTimeout),
		SandboxCommandMaxTimeout:    getenvDuration("SANDBOX_COMMAND_MAX_TIMEOUT", defaultSandboxCommandMaxTimeout),
		AgentSystemPromptFile:       getenv("AGENT_SYSTEM_PROMPT_FILE", defaultAgentSystemPromptFile),
		AgentCompactPromptFile:      getenv("AGENT_COMPACT_PROMPT_FILE", defaultAgentCompactPromptFile),
		RemoteToolReplayMaxBytes:    getenvInt("REMOTE_TOOL_REPLAY_MAX_BYTES", defaultRemoteToolReplayMaxBytes),
		ModelToolOutputMaxTokens:    getenvInt("AGENT_MODEL_TOOL_OUTPUT_MAX_TOKENS", defaultModelToolOutputMaxTokens),
		CompactMaxOutputTokens:      getenvInt("AGENT_COMPACT_MAX_OUTPUT_TOKENS", defaultCompactOutputTokens),
		CompactTriggerTokens:        getenvInt("AGENT_COMPACT_TRIGGER_TOKENS", defaultCompactTriggerTokens),
		CacheMaxConversations:       getenvInt("CACHE_MAX_CONVERSATIONS", defaultCacheMaxConversations),
		CacheTailCapacity:           getenvInt("CACHE_TAIL_CAPACITY", defaultCacheTailCapacity),
		HTTPClientTimeout:           getenvDuration("HTTP_CLIENT_TIMEOUT", defaultHTTPClientTimeout),
		ImageGenerationPartials:     getenvInt("IMAGE_GENERATION_PARTIAL_IMAGES", defaultImageGenerationPartials),
		ImagePreviewTTL:             getenvDuration("IMAGE_GENERATION_PREVIEW_TTL", defaultImagePreviewTTL),
	}
}

func (c Config) LoadPrompts() (Config, error) {
	systemPrompt, err := readPromptFile(c.AgentSystemPromptFile, "system prompt")
	if err != nil {
		return Config{}, err
	}
	compactPrompt, err := readPromptFile(c.AgentCompactPromptFile, "compaction prompt")
	if err != nil {
		return Config{}, err
	}

	c.AgentSystemPrompt = systemPrompt
	c.AgentCompactPrompt = compactPrompt
	return c, nil
}

func readPromptFile(path string, label string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%s file path is empty", label)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s file %q: %w", label, path, err)
	}
	prompt := strings.TrimSpace(string(content))
	if prompt == "" {
		return "", fmt.Errorf("%s file %q is empty", label, path)
	}
	return prompt, nil
}

func (c Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c Config) ValidateAPI() error {
	if err := c.validateSandboxLifecycle(); err != nil {
		return fmt.Errorf("invalid api config: %w", err)
	}
	var missing []string

	required := map[string]string{
		"DATABASE_URL":                   c.DatabaseURL,
		"REDIS_ADDR":                     c.RedisAddr,
		"WEB_ORIGIN":                     c.WebOrigin,
		"AUTH_JWT_SECRET":                c.JWTSecret,
		"SYSTEM_USER_EMAIL":              c.SystemUserEmail,
		"SYSTEM_USER_USERNAME":           c.SystemUserUsername,
		"SYSTEM_USER_PASSWORD_HASH":      c.SystemUserPasswordHash,
		"PROVIDER_CREDENTIAL_MASTER_KEY": c.ProviderCredentialMasterKey,
		"S3_ENDPOINT":                    c.S3Endpoint,
		"S3_BUCKET":                      c.S3Bucket,
		"S3_ACCESS_KEY":                  c.S3AccessKey,
		"S3_SECRET_KEY":                  c.S3SecretKey,
	}
	sandboxRequired, err := c.sandboxProviderRequirements()
	if err != nil {
		return fmt.Errorf("invalid api config: %w", err)
	}
	for key, value := range sandboxRequired {
		required[key] = value
	}

	for key, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required api config: %s", strings.Join(missing, ", "))
	}
	if err := validateWebOrigin(c.WebOrigin); err != nil {
		return fmt.Errorf("invalid api config: %w", err)
	}
	if err := c.validateS3(); err != nil {
		return fmt.Errorf("invalid api config: %w", err)
	}

	return nil
}

func validateWebOrigin(value string) error {
	origin, err := url.Parse(value)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" {
		return errors.New("WEB_ORIGIN must be an absolute HTTP(S) origin")
	}
	if origin.User != nil || (origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.Fragment != "" {
		return errors.New("WEB_ORIGIN must not include credentials, a path, query parameters, or a fragment")
	}
	return nil
}

func (c Config) ValidateWorker() error {
	if err := c.validateSandboxLifecycle(); err != nil {
		return fmt.Errorf("invalid worker config: %w", err)
	}
	var missing []string

	required := map[string]string{
		"DATABASE_URL":                   c.DatabaseURL,
		"REDIS_ADDR":                     c.RedisAddr,
		"KAFKA_BROKERS":                  strings.Join(c.KafkaBrokers, ","),
		"S3_ENDPOINT":                    c.S3Endpoint,
		"S3_BUCKET":                      c.S3Bucket,
		"S3_ACCESS_KEY":                  c.S3AccessKey,
		"S3_SECRET_KEY":                  c.S3SecretKey,
		"PROVIDER_CREDENTIAL_MASTER_KEY": c.ProviderCredentialMasterKey,
		"AGENT_SYSTEM_PROMPT_FILE":       c.AgentSystemPromptFile,
		"AGENT_COMPACT_PROMPT_FILE":      c.AgentCompactPromptFile,
	}
	sandboxRequired, err := c.sandboxProviderRequirements()
	if err != nil {
		return fmt.Errorf("invalid worker config: %w", err)
	}
	for key, value := range sandboxRequired {
		required[key] = value
	}

	for key, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required worker config: %s", strings.Join(missing, ", "))
	}

	if len(c.KafkaBrokers) == 0 {
		return errors.New("missing required worker config: KAFKA_BROKERS")
	}
	if err := c.validateS3(); err != nil {
		return fmt.Errorf("invalid worker config: %w", err)
	}
	if c.ImageGenerationPartials < 0 || c.ImageGenerationPartials > 3 {
		return errors.New("IMAGE_GENERATION_PARTIAL_IMAGES must be between 1 and 3")
	}
	if c.ImagePreviewTTL < 0 || (c.ImagePreviewTTL > 0 && c.ImagePreviewTTL <= c.S3PresignTTL) {
		return errors.New("IMAGE_GENERATION_PREVIEW_TTL must be greater than S3_PRESIGN_TTL")
	}

	return nil
}

func (c Config) validateS3() error {
	switch strings.ToLower(strings.TrimSpace(c.S3Provider)) {
	case "aws", "aliyun", "r2", "minio":
	default:
		return errors.New("S3_PROVIDER must be one of aws, aliyun, r2, or minio")
	}
	switch strings.ToLower(strings.TrimSpace(c.S3BucketLookup)) {
	case "auto", "dns", "path":
	default:
		return errors.New("S3_BUCKET_LOOKUP must be auto, dns, or path")
	}
	if c.S3PresignTTL <= 0 || c.S3PresignTTL > 7*24*time.Hour {
		return errors.New("S3_PRESIGN_TTL must be positive and at most 7 days")
	}
	if c.S3PendingUploadTTL <= c.S3PresignTTL {
		return errors.New("S3_PENDING_UPLOAD_TTL must be greater than S3_PRESIGN_TTL")
	}
	if c.S3UploadReaperInterval <= 0 || c.S3UploadReaperBatchSize <= 0 {
		return errors.New("S3 upload reaper interval and batch size must be positive")
	}
	return nil
}

func (c Config) validateSandboxLifecycle() error {
	for key, value := range map[string]time.Duration{
		"SANDBOX_IDLE_STOP_AFTER":         c.SandboxIdleStopAfter,
		"SANDBOX_STOPPED_RETENTION":       c.SandboxStoppedRetention,
		"SANDBOX_REAPER_INTERVAL":         c.SandboxReaperInterval,
		"SANDBOX_COMMAND_DEFAULT_TIMEOUT": c.SandboxCommandTimeout,
		"SANDBOX_COMMAND_MAX_TIMEOUT":     c.SandboxCommandMaxTimeout,
	} {
		if value <= 0 {
			return fmt.Errorf("%s must be positive", key)
		}
	}
	if c.SandboxReaperBatchSize <= 0 {
		return errors.New("SANDBOX_REAPER_BATCH_SIZE must be positive")
	}
	if c.SandboxCommandMaxTimeout < c.SandboxCommandTimeout {
		return errors.New("SANDBOX_COMMAND_MAX_TIMEOUT must be greater than or equal to SANDBOX_COMMAND_DEFAULT_TIMEOUT")
	}
	if c.SandboxCommandTimeout < time.Second || c.SandboxCommandTimeout%time.Second != 0 || c.SandboxCommandMaxTimeout%time.Second != 0 {
		return errors.New("sandbox command timeouts must use whole seconds and be at least one second")
	}
	return nil
}

func (c Config) ValidateBackend() error {
	if err := c.ValidateAPI(); err != nil {
		return err
	}

	if err := c.ValidateWorker(); err != nil {
		return err
	}

	return nil
}

func (c Config) ValidateMigration() error {
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return errors.New("missing required config: DATABASE_URL")
	}

	return nil
}

func (c Config) sandboxProviderRequirements() (map[string]string, error) {
	provider := strings.ToLower(strings.TrimSpace(c.SandboxProvider))
	if provider == "" {
		provider = defaultSandboxProvider
	}
	switch provider {
	case "firecracker":
		return map[string]string{"SANDBOX_BRIDGE_URL": c.SandboxBridgeURL}, nil
	case "cubesandbox":
		if c.SandboxCubeProxyPortHTTP <= 0 {
			return nil, errors.New("SANDBOX_CUBE_PROXY_PORT_HTTP must be positive")
		}
		if c.SandboxCubeRequestTimeout <= 0 {
			return nil, errors.New("SANDBOX_CUBE_REQUEST_TIMEOUT must be positive")
		}
		if c.SandboxCubePauseTimeout <= 0 {
			return nil, errors.New("SANDBOX_CUBE_PAUSE_TIMEOUT must be positive")
		}
		if c.SandboxCubeMaxOutputBytes <= 0 {
			return nil, errors.New("SANDBOX_CUBE_MAX_OUTPUT_BYTES must be positive")
		}
		if c.SandboxCubeAllowInternet && cubeAllowOutHasDomain(c.SandboxCubeAllowOut) && !stringListContains(c.SandboxCubeDenyOut, "0.0.0.0/0") {
			return nil, errors.New("SANDBOX_CUBE_ALLOW_OUT domains require SANDBOX_CUBE_ALLOW_INTERNET=false or SANDBOX_CUBE_DENY_OUT containing 0.0.0.0/0")
		}
		return map[string]string{
			"SANDBOX_CUBE_API_URL":     c.SandboxCubeAPIURL,
			"SANDBOX_CUBE_API_KEY":     c.SandboxCubeAPIKey,
			"SANDBOX_CUBE_TEMPLATE_ID": c.SandboxCubeTemplateID,
		}, nil
	default:
		return nil, fmt.Errorf("SANDBOX_PROVIDER must be firecracker or cubesandbox, got %q", c.SandboxProvider)
	}
}

func cubeAllowOutHasDomain(values []string) bool {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || net.ParseIP(value) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(value); err == nil {
			continue
		}
		return true
	}
	return false
}

func stringListContains(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func getenvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func getenvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}

	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func getenvList(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return append([]string(nil), fallback...)
	}

	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			values = append(values, item)
		}
	}

	if len(values) == 0 {
		return append([]string(nil), fallback...)
	}

	return values
}
