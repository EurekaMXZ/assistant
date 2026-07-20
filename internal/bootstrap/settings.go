package bootstrap

import (
	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	"github.com/EurekaMXZ/assistant/internal/config"
	assistantkafka "github.com/EurekaMXZ/assistant/internal/kafka"
	"github.com/EurekaMXZ/assistant/internal/objectstore"
	"github.com/EurekaMXZ/assistant/internal/openai"
	streamredis "github.com/EurekaMXZ/assistant/internal/redis"
	assistantsandbox "github.com/EurekaMXZ/assistant/internal/sandbox"
	"github.com/EurekaMXZ/assistant/internal/server"
	"github.com/EurekaMXZ/assistant/internal/tavily"
	"github.com/EurekaMXZ/assistant/internal/worker"
	"github.com/EurekaMXZ/assistant/internal/workflow"
)

type baseSettings struct {
	DatabaseURL                 string
	Address                     string
	EnableAuth                  bool
	BillingCurrency             string
	Sandbox                     assistantsandbox.RuntimeSettings
	SandboxLifecycle            assistantsandbox.LifecycleSettings
	Stream                      streamredis.Settings
	Server                      server.Settings
	Auth                        assistantauth.TokenSettings
	SystemUser                  assistantauth.SystemUserConfig
	ObjectStore                 objectstore.Settings
	ProviderCredentialMasterKey string
}

type workerSettings struct {
	CacheMaxConversations int
	CacheTailCapacity     int
	OpenAI                openai.Settings
	Tavily                tavily.Settings
	SandboxExecEnabled    bool
	Sandbox               assistantsandbox.RuntimeSettings
	SandboxLifecycle      assistantsandbox.LifecycleSettings
	ObjectStore           objectstore.Settings
	AttachmentCleanup     assistantattachment.CleanupSettings
	Kafka                 assistantkafka.Settings
	KafkaReader           assistantkafka.ReaderSettings
	Workflow              workflow.WorkflowSettings
	Process               worker.Settings
}

func newBaseSettings(cfg config.Config, enableAuth bool) baseSettings {
	return baseSettings{
		DatabaseURL:      cfg.DatabaseURL,
		Address:          cfg.Address(),
		EnableAuth:       enableAuth,
		BillingCurrency:  cfg.BillingCurrency,
		Sandbox:          sandboxRuntimeSettings(cfg),
		SandboxLifecycle: sandboxLifecycleSettings(cfg),
		Stream: streamredis.Settings{
			Addr:          cfg.RedisAddr,
			Password:      cfg.RedisPassword,
			DB:            cfg.RedisDB,
			ChannelPrefix: cfg.StreamChannelPrefix,
		},
		Server: server.Settings{
			Address:      cfg.Address(),
			WebOrigin:    cfg.WebOrigin,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
			IdleTimeout:  cfg.IdleTimeout,
		},
		Auth: assistantauth.TokenSettings{
			Secret:         cfg.JWTSecret,
			Issuer:         cfg.JWTIssuer,
			AccessTokenTTL: cfg.AccessTokenTTL,
		},
		SystemUser: assistantauth.SystemUserConfig{
			Email:        cfg.SystemUserEmail,
			Username:     cfg.SystemUserUsername,
			PasswordHash: cfg.SystemUserPasswordHash,
		},
		ObjectStore:                 objectStoreSettings(cfg),
		ProviderCredentialMasterKey: cfg.ProviderCredentialMasterKey,
	}
}

func newWorkerSettings(cfg config.Config) workerSettings {
	return workerSettings{
		CacheMaxConversations: cfg.CacheMaxConversations,
		CacheTailCapacity:     cfg.CacheTailCapacity,
		OpenAI: openai.Settings{
			UserAgent:         cfg.OpenAIUserAgent,
			HTTPClientTimeout: cfg.HTTPClientTimeout,
		},
		Tavily: tavily.Settings{
			APIKey:            cfg.TavilyAPIKey,
			HTTPClientTimeout: cfg.HTTPClientTimeout,
		},
		SandboxExecEnabled: cfg.SandboxExecEnabled,
		Sandbox:            sandboxRuntimeSettings(cfg),
		SandboxLifecycle:   sandboxLifecycleSettings(cfg),
		ObjectStore:        objectStoreSettings(cfg),
		AttachmentCleanup: assistantattachment.CleanupSettings{
			PendingTTL: cfg.S3PendingUploadTTL,
			Interval:   cfg.S3UploadReaperInterval,
			BatchSize:  cfg.S3UploadReaperBatchSize,
		},
		Kafka: assistantkafka.Settings{
			Brokers:       cfg.KafkaBrokers,
			WorkflowTopic: cfg.KafkaWorkflowTopic,
		},
		KafkaReader: assistantkafka.ReaderSettings{
			Brokers:       cfg.KafkaBrokers,
			WorkflowTopic: cfg.KafkaWorkflowTopic,
			ConsumerGroup: cfg.KafkaConsumerGroup,
		},
		Workflow: workflow.WorkflowSettings{
			AgentSystemPrompt:        cfg.AgentSystemPrompt,
			AgentCompactPrompt:       cfg.AgentCompactPrompt,
			RemoteToolReplayMaxBytes: cfg.RemoteToolReplayMaxBytes,
			ModelToolOutputMaxTokens: cfg.ModelToolOutputMaxTokens,
			CompactMaxOutputTokens:   cfg.CompactMaxOutputTokens,
			CompactTriggerTokens:     cfg.CompactTriggerTokens,
			WorkerLeaseTimeout:       cfg.WorkerLeaseTimeout,
			OutboxBatchSize:          cfg.OutboxBatchSize,
		},
		Process: worker.Settings{
			WorkerConcurrency:  cfg.WorkerConcurrency,
			WorkerPollInterval: cfg.WorkerPollInterval,
			WorkerLeaseTimeout: cfg.WorkerLeaseTimeout,
		},
	}
}

func objectStoreSettings(cfg config.Config) objectstore.Settings {
	return objectstore.Settings{
		Provider:         cfg.S3Provider,
		Endpoint:         cfg.S3Endpoint,
		PublicEndpoint:   cfg.S3PublicEndpoint,
		Region:           cfg.S3Region,
		Bucket:           cfg.S3Bucket,
		AccessKey:        cfg.S3AccessKey,
		SecretKey:        cfg.S3SecretKey,
		SessionToken:     cfg.S3SessionToken,
		UseSSL:           cfg.S3UseSSL,
		BucketLookup:     cfg.S3BucketLookup,
		AutoCreateBucket: cfg.S3AutoCreateBucket,
		PresignTTL:       cfg.S3PresignTTL,
	}
}

func sandboxRuntimeSettings(cfg config.Config) assistantsandbox.RuntimeSettings {
	return assistantsandbox.RuntimeSettings{
		Provider: cfg.SandboxProvider,
		HTTP: assistantsandbox.HTTPRuntimeSettings{
			BaseURL:           cfg.SandboxBridgeURL,
			Token:             cfg.SandboxBridgeToken,
			HTTPClientTimeout: cfg.SandboxBridgeTimeout,
			CommandTimeout:    cfg.SandboxCommandMaxTimeout,
		},
		Cube: assistantsandbox.CubeRuntimeSettings{
			APIURL:              cfg.SandboxCubeAPIURL,
			APIKey:              cfg.SandboxCubeAPIKey,
			TemplateID:          cfg.SandboxCubeTemplateID,
			ProxyNodeIP:         cfg.SandboxCubeProxyNodeIP,
			ProxyPortHTTP:       cfg.SandboxCubeProxyPortHTTP,
			ProxyScheme:         cfg.SandboxCubeProxyScheme,
			SandboxDomain:       cfg.SandboxCubeDomain,
			ClusterID:           cfg.SandboxCubeClusterID,
			RequestTimeout:      cfg.SandboxCubeRequestTimeout,
			PauseTimeout:        cfg.SandboxCubePauseTimeout,
			MaxOutputBytes:      cfg.SandboxCubeMaxOutputBytes,
			AllowInternetAccess: cfg.SandboxCubeAllowInternet,
			AllowOut:            append([]string(nil), cfg.SandboxCubeAllowOut...),
			DenyOut:             append([]string(nil), cfg.SandboxCubeDenyOut...),
		},
	}
}

func sandboxLifecycleSettings(cfg config.Config) assistantsandbox.LifecycleSettings {
	return assistantsandbox.LifecycleSettings{
		IdleStopAfter:    cfg.SandboxIdleStopAfter,
		StoppedRetention: cfg.SandboxStoppedRetention,
		ReaperInterval:   cfg.SandboxReaperInterval,
		ReaperBatchSize:  cfg.SandboxReaperBatchSize,
		CommandDefault:   cfg.SandboxCommandTimeout,
		CommandMaximum:   cfg.SandboxCommandMaxTimeout,
	}
}
