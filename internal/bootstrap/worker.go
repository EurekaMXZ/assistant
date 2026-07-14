package bootstrap

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/cache"
	"github.com/EurekaMXZ/assistant/internal/credential"
	assistantkafka "github.com/EurekaMXZ/assistant/internal/kafka"
	"github.com/EurekaMXZ/assistant/internal/minio"
	"github.com/EurekaMXZ/assistant/internal/openai"
	assistantsandbox "github.com/EurekaMXZ/assistant/internal/sandbox"
	"github.com/EurekaMXZ/assistant/internal/stream"
	tavilyclient "github.com/EurekaMXZ/assistant/internal/tavily"
	"github.com/EurekaMXZ/assistant/internal/tool"
	"github.com/EurekaMXZ/assistant/internal/worker"
	"github.com/EurekaMXZ/assistant/internal/workflow"
)

func buildWorker(ctx context.Context, logger *log.Logger, settings workerSettings, workflows workflowAdapters, publisher stream.Publisher) (*worker.Service, error) {
	artifactStore, err := minio.New(settings.MinIO)
	if err != nil {
		return nil, err
	}
	if err := artifactStore.EnsureBucket(ctx); err != nil {
		return nil, err
	}

	if err := assistantkafka.EnsureTopic(ctx, settings.Kafka); err != nil {
		return nil, err
	}

	cacheStore := cache.New(settings.CacheMaxConversations, settings.CacheTailCapacity)
	writer := assistantkafka.NewWorkflowWriter(settings.Kafka)
	newReader := func() worker.WorkflowReader {
		return assistantkafka.NewWorkflowReader(settings.KafkaReader)
	}
	openaiClient := openai.New(settings.OpenAI)
	openaiClient.SetCredentialResolver(credential.NewResolver(workflows.ProviderCredentials, workflows.CredentialCipher))
	streamPublisher := workflow.NewArchivingStreamPublisher(publisher, artifactStore, workflows.StreamEvents)
	sandboxRuntime, err := buildSandboxRuntime(settings.Sandbox)
	if err != nil {
		return nil, err
	}
	tavilyEnabled := strings.TrimSpace(settings.Tavily.APIKey) != ""
	toolHandlers := []tool.LocalToolHandler{
		tool.RenameConversationTitleHandler{
			UseCase: tool.RenameConversationTitle{
				Conversations: workflows.Conversations,
			},
		},
		tool.CreateSandboxHandler{
			UseCase: tool.CreateSandbox{
				Sandboxes: workflows.Sandboxes,
				Runtime:   sandboxRuntime,
				Locker:    workflows.Locker,
			},
		},
		tool.DestroySandboxHandler{
			UseCase: tool.DestroySandbox{
				Sandboxes: workflows.Sandboxes,
				Runtime:   sandboxRuntime,
				Locker:    workflows.Locker,
			},
		},
		tool.ExecSandboxCommandHandler{
			UseCase: tool.ExecSandboxCommand{
				Sandboxes:      workflows.Sandboxes,
				Runtime:        sandboxRuntime,
				Locker:         workflows.Locker,
				DefaultTimeout: settings.SandboxLifecycle.CommandDefault,
				MaximumTimeout: settings.SandboxLifecycle.CommandMaximum,
			},
		},
	}
	if tavilyEnabled {
		tavilyClient := tavilyclient.New(settings.Tavily)
		tavilyTools := tool.TavilyTools{Client: tavilyClient}
		toolHandlers = append(toolHandlers, tool.SearchWebHandler{
			UseCase: tavilyTools,
		}, tool.ExtractWebHandler{
			UseCase: tavilyTools,
		})
	}
	toolExecutor, err := tool.NewLocalExecutor(toolHandlers...)
	if err != nil {
		return nil, err
	}
	toolDefinitions := tool.DefaultTools()
	if tavilyEnabled {
		toolDefinitions = tool.DefaultToolsWithTavily()
	}
	workflowEngine := workflow.New(workflow.Dependencies{
		Logger:      logger,
		Settings:    settings.Workflow,
		Outbox:      workflows.Outbox,
		Turns:       workflows.Turns,
		Contexts:    workflows.Contexts,
		Attachments: workflows.Attachments,
		StaleTurns:  workflows.StaleTurns,
		Model:       openaiClient,
		ToolCatalog: tool.StaticCatalog{
			Tools:             toolDefinitions,
			EnableSandboxExec: settings.SandboxExecEnabled,
		},
		ToolExecutor:          toolExecutor,
		Conversations:         workflows.ConversationReader,
		Models:                workflows.Models,
		BillingUsage:          workflows.BillingUsage,
		ConversationSandboxes: workflows.Sandboxes,
		ToolArtifacts:         artifactStore,
		TurnRuns:              workflows.TurnRuns,
		ToolCalls:             workflows.ToolCalls,
		TurnArtifacts:         artifactStore,
		ContextAnchors:        artifactStore,
		AttachmentBlobs:       artifactStore,
		GeneratedAttachments:  workflows.GeneratedAttachments,
		Streams:               streamPublisher,
		ContextCache:          cacheStore,
		ContextTail:           cacheStore,
		ContextCompaction:     cacheStore,
		Locker:                workflows.Locker,
	})

	reaper := assistantsandbox.NewReaper(settings.SandboxLifecycle, workflows.Sandboxes, sandboxRuntime, workflows.Locker, logger)
	return worker.New(logger, workflowEngine, settings.Process, writer, newReader, reaper), nil
}

func buildSandboxRuntime(settings assistantsandbox.RuntimeSettings) (tool.SandboxManager, error) {
	runtime, err := assistantsandbox.NewRuntime(settings)
	if err != nil {
		return nil, fmt.Errorf("configure sandbox runtime: %w", err)
	}
	return runtime, nil
}
