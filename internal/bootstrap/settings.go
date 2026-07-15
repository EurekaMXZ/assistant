package bootstrap

import (
	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	"github.com/EurekaMXZ/assistant/internal/config"
	assistantkafka "github.com/EurekaMXZ/assistant/internal/kafka"
	"github.com/EurekaMXZ/assistant/internal/minio"
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
	MinIO                       minio.Settings
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
	MinIO                 minio.Settings
	Kafka                 assistantkafka.Settings
	KafkaReader           assistantkafka.ReaderSettings
	Workflow              workflow.WorkflowSettings
	Process               worker.Settings
}

func newBaseSettings(cfg config.Config, enableAuth bool) baseSettings {
	return baseSettings{
		DatabaseURL:     cfg.DatabaseURL,
		Address:         cfg.Address(),
		EnableAuth:      enableAuth,
		BillingCurrency: cfg.BillingCurrency,
		Sandbox: assistantsandbox.RuntimeSettings{
			Provider: cfg.SandboxProvider,
			HTTP: assistantsandbox.HTTPRuntimeSettings{
				BaseURL:           cfg.SandboxBridgeURL,
				Token:             cfg.SandboxBridgeToken,
				HTTPClientTimeout: cfg.SandboxBridgeTimeout,
				CommandTimeout:    cfg.SandboxCommandMaxTimeout,
			},
		},
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
		MinIO: minio.Settings{
			Endpoint:  cfg.MinIOEndpoint,
			Region:    cfg.MinIORegion,
			Bucket:    cfg.MinIOBucket,
			AccessKey: cfg.MinIOAccessKey,
			SecretKey: cfg.MinIOSecretKey,
			UseSSL:    cfg.MinIOUseSSL,
		},
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
		Sandbox: assistantsandbox.RuntimeSettings{
			Provider: cfg.SandboxProvider,
			HTTP: assistantsandbox.HTTPRuntimeSettings{
				BaseURL:           cfg.SandboxBridgeURL,
				Token:             cfg.SandboxBridgeToken,
				HTTPClientTimeout: cfg.SandboxBridgeTimeout,
				CommandTimeout:    cfg.SandboxCommandMaxTimeout,
			},
		},
		SandboxLifecycle: sandboxLifecycleSettings(cfg),
		MinIO: minio.Settings{
			Endpoint:  cfg.MinIOEndpoint,
			Region:    cfg.MinIORegion,
			Bucket:    cfg.MinIOBucket,
			AccessKey: cfg.MinIOAccessKey,
			SecretKey: cfg.MinIOSecretKey,
			UseSSL:    cfg.MinIOUseSSL,
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
