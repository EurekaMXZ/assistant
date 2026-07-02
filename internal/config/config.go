package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultHost                     = "0.0.0.0"
	defaultPort                     = 8080
	defaultReadTimeout              = 15 * time.Second
	defaultWriteTimeout             = 15 * time.Second
	defaultIdleTimeout              = 60 * time.Second
	defaultShutdownTimeout          = 10 * time.Second
	defaultDatabaseURL              = "postgres://assistant:assistant@127.0.0.1:5432/assistant?sslmode=disable"
	defaultWorkerPollInterval       = 2 * time.Second
	defaultWorkerLeaseTimeout       = 2 * time.Minute
	defaultWorkerConcurrency        = 4
	defaultKafkaTopic               = "assistant.workflow"
	defaultKafkaGroup               = "assistant-workers"
	defaultRedisAddr                = "127.0.0.1:6379"
	defaultRedisDB                  = 0
	defaultStreamChannelPrefix      = "assistant:stream"
	defaultMinIOEndpoint            = "127.0.0.1:9000"
	defaultMinIORegion              = "us-east-1"
	defaultMinIOBucket              = "assistant"
	defaultMinIOUseSSL              = false
	defaultMinIOAccessKey           = "assistantminio"
	defaultMinIOSecretKey           = "assistantminio123"
	defaultOpenAIUserAgent          = "assistant"
	defaultRemoteToolReplayMaxBytes = 16384
	defaultCompactOutputTokens      = 1536
	defaultCompactTriggerTokens     = 12000
	defaultCacheMaxConversations    = 1024
	defaultCacheTailCapacity        = 256
	defaultOutboxBatchSize          = 100
	defaultHTTPClientTimeout        = 5 * time.Minute
	defaultWebOrigin                = "http://localhost:3000"
	defaultJWTIssuer                = "assistant"
	defaultAccessTokenTTL           = 24 * time.Hour
	defaultSandboxBridgeTimeout     = time.Minute
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
	KafkaBrokers                []string
	KafkaWorkflowTopic          string
	KafkaConsumerGroup          string
	MinIOEndpoint               string
	MinIORegion                 string
	MinIOBucket                 string
	MinIOAccessKey              string
	MinIOSecretKey              string
	MinIOUseSSL                 bool
	OpenAIUserAgent             string
	ProviderCredentialMasterKey string
	BillingCurrency             string
	TavilyAPIKey                string
	SandboxExecEnabled          bool
	SandboxBridgeURL            string
	SandboxBridgeToken          string
	SandboxBridgeTimeout        time.Duration
	AgentSystemPrompt           string
	AgentCompactPrompt          string
	RemoteToolReplayMaxBytes    int
	CompactMaxOutputTokens      int
	CompactTriggerTokens        int
	CacheMaxConversations       int
	CacheTailCapacity           int
	HTTPClientTimeout           time.Duration
}

func Load() Config {
	_ = godotenv.Load()

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
		WebOrigin:                   getenv("WEB_ORIGIN", defaultWebOrigin),
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
		KafkaBrokers:                getenvList("KAFKA_BROKERS", []string{"127.0.0.1:9092"}),
		KafkaWorkflowTopic:          getenv("KAFKA_WORKFLOW_TOPIC", defaultKafkaTopic),
		KafkaConsumerGroup:          getenv("KAFKA_CONSUMER_GROUP", defaultKafkaGroup),
		MinIOEndpoint:               getenv("MINIO_ENDPOINT", defaultMinIOEndpoint),
		MinIORegion:                 getenv("MINIO_REGION", defaultMinIORegion),
		MinIOBucket:                 getenv("MINIO_BUCKET", defaultMinIOBucket),
		MinIOAccessKey:              getenv("MINIO_ACCESS_KEY", defaultMinIOAccessKey),
		MinIOSecretKey:              getenv("MINIO_SECRET_KEY", defaultMinIOSecretKey),
		MinIOUseSSL:                 getenvBool("MINIO_USE_SSL", defaultMinIOUseSSL),
		OpenAIUserAgent:             getenv("OPENAI_USER_AGENT", defaultOpenAIUserAgent),
		ProviderCredentialMasterKey: os.Getenv("PROVIDER_CREDENTIAL_MASTER_KEY"),
		BillingCurrency:             getenv("BILLING_CURRENCY", "USD"),
		TavilyAPIKey:                os.Getenv("TAVILY_API_KEY"),
		SandboxExecEnabled:          getenvBool("SANDBOX_EXEC_ENABLED", false),
		SandboxBridgeURL:            os.Getenv("SANDBOX_BRIDGE_URL"),
		SandboxBridgeToken:          os.Getenv("SANDBOX_BRIDGE_TOKEN"),
		SandboxBridgeTimeout:        getenvDuration("SANDBOX_BRIDGE_TIMEOUT", defaultSandboxBridgeTimeout),
		AgentSystemPrompt:           os.Getenv("AGENT_SYSTEM_PROMPT"),
		AgentCompactPrompt:          os.Getenv("AGENT_COMPACT_PROMPT"),
		RemoteToolReplayMaxBytes:    getenvInt("REMOTE_TOOL_REPLAY_MAX_BYTES", defaultRemoteToolReplayMaxBytes),
		CompactMaxOutputTokens:      getenvInt("AGENT_COMPACT_MAX_OUTPUT_TOKENS", defaultCompactOutputTokens),
		CompactTriggerTokens:        getenvInt("AGENT_COMPACT_TRIGGER_TOKENS", defaultCompactTriggerTokens),
		CacheMaxConversations:       getenvInt("CACHE_MAX_CONVERSATIONS", defaultCacheMaxConversations),
		CacheTailCapacity:           getenvInt("CACHE_TAIL_CAPACITY", defaultCacheTailCapacity),
		HTTPClientTimeout:           getenvDuration("HTTP_CLIENT_TIMEOUT", defaultHTTPClientTimeout),
	}
}

func (c Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c Config) ValidateAPI() error {
	var missing []string

	required := map[string]string{
		"DATABASE_URL":                   c.DatabaseURL,
		"REDIS_ADDR":                     c.RedisAddr,
		"SANDBOX_BRIDGE_URL":             c.SandboxBridgeURL,
		"AUTH_JWT_SECRET":                c.JWTSecret,
		"SYSTEM_USER_EMAIL":              c.SystemUserEmail,
		"SYSTEM_USER_USERNAME":           c.SystemUserUsername,
		"SYSTEM_USER_PASSWORD_HASH":      c.SystemUserPasswordHash,
		"PROVIDER_CREDENTIAL_MASTER_KEY": c.ProviderCredentialMasterKey,
	}

	for key, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required api config: %s", strings.Join(missing, ", "))
	}

	return nil
}

func (c Config) ValidateWorker() error {
	var missing []string

	required := map[string]string{
		"DATABASE_URL":                   c.DatabaseURL,
		"REDIS_ADDR":                     c.RedisAddr,
		"SANDBOX_BRIDGE_URL":             c.SandboxBridgeURL,
		"KAFKA_BROKERS":                  strings.Join(c.KafkaBrokers, ","),
		"MINIO_ACCESS_KEY":               c.MinIOAccessKey,
		"MINIO_SECRET_KEY":               c.MinIOSecretKey,
		"PROVIDER_CREDENTIAL_MASTER_KEY": c.ProviderCredentialMasterKey,
		"AGENT_SYSTEM_PROMPT":            c.AgentSystemPrompt,
		"AGENT_COMPACT_PROMPT":           c.AgentCompactPrompt,
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
