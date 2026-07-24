package bootstrap

import (
	"context"
	"fmt"
	"log"
	"strings"

	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/EurekaMXZ/assistant/internal/cache"
	"github.com/EurekaMXZ/assistant/internal/credential"
	assistantkafka "github.com/EurekaMXZ/assistant/internal/kafka"
	"github.com/EurekaMXZ/assistant/internal/mcpconfig"
	"github.com/EurekaMXZ/assistant/internal/objectstore"
	"github.com/EurekaMXZ/assistant/internal/openai"
	assistantsandbox "github.com/EurekaMXZ/assistant/internal/sandbox"
	"github.com/EurekaMXZ/assistant/internal/stream"
	tavilyclient "github.com/EurekaMXZ/assistant/internal/tavily"
	"github.com/EurekaMXZ/assistant/internal/tool"
	"github.com/EurekaMXZ/assistant/internal/worker"
	"github.com/EurekaMXZ/assistant/internal/workflow"
)

func buildWorker(ctx context.Context, logger *log.Logger, settings workerSettings, workflows workflowAdapters, publisher stream.Publisher) (*worker.Service, error) {
	artifactStore, err := objectstore.New(settings.ObjectStore)
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
	sharedContextCache, _ := publisher.(cache.SharedContextSnapshotCache)
	writer := assistantkafka.NewWorkflowWriter(settings.Kafka)
	newReader := func() worker.WorkflowReader {
		return assistantkafka.NewWorkflowReader(settings.KafkaReader)
	}
	openaiClient := openai.New(settings.OpenAI)
	openaiClient.SetCredentialResolver(credential.NewResolver(workflows.ProviderCredentials, workflows.CredentialCipher))
	kafkaStreamPublisher := assistantkafka.NewStreamPublisher(settings.Kafka, publisher)
	streamRecovery := assistantkafka.NewStreamRecovery(settings.Kafka, settings.KafkaReader.ConsumerGroup, workflows.CompleteEvents)
	streamPublisher := workflow.NewArchivingStreamPublisher(kafkaStreamPublisher, artifactStore, workflows.StreamEvents, workflows.CompleteEvents)
	sandboxRuntime, err := buildSandboxRuntime(settings.Sandbox)
	if err != nil {
		return nil, err
	}
	sandboxFileReader, ok := sandboxRuntime.(tool.SandboxFileReader)
	if !ok {
		return nil, fmt.Errorf("sandbox runtime does not support file reads")
	}
	sandboxShells, ok := sandboxRuntime.(tool.SandboxShellManager)
	if !ok {
		return nil, fmt.Errorf("sandbox runtime does not support shell sessions")
	}
	tavilyEnabled := strings.TrimSpace(settings.Tavily.APIKey) != ""
	toolHandlers := []tool.LocalToolHandler{
		tool.AskUserHandler{},
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
		tool.CreateSandboxShellHandler{
			UseCase: tool.CreateSandboxShell{
				Sandboxes: workflows.Sandboxes,
				Runtime:   sandboxRuntime,
				Shells:    sandboxShells,
				Locker:    workflows.Locker,
			},
		},
		tool.ConnectSandboxShellHandler{
			UseCase: tool.ConnectSandboxShell{
				Sandboxes:      workflows.Sandboxes,
				Runtime:        sandboxRuntime,
				Shells:         sandboxShells,
				Locker:         workflows.Locker,
				DefaultTimeout: settings.SandboxLifecycle.CommandDefault,
				MaximumTimeout: settings.SandboxLifecycle.CommandMaximum,
			},
		},
		tool.DestroySandboxShellHandler{
			UseCase: tool.DestroySandboxShell{
				Sandboxes: workflows.Sandboxes,
				Runtime:   sandboxRuntime,
				Shells:    sandboxShells,
				Locker:    workflows.Locker,
			},
		},
		tool.WriteSandboxFileHandler{
			UseCase: tool.WriteSandboxFile{
				Sandboxes: workflows.Sandboxes,
				Runtime:   sandboxRuntime,
				Locker:    workflows.Locker,
			},
		},
		tool.EditSandboxFileHandler{
			UseCase: tool.EditSandboxFile{
				Sandboxes: workflows.Sandboxes,
				Runtime:   sandboxRuntime,
				Files:     sandboxFileReader,
				Locker:    workflows.Locker,
			},
		},
		tool.ImportSandboxAttachmentHandler{
			UseCase: tool.ImportSandboxAttachment{
				Attachments: workflows.Attachments,
				Blobs:       artifactStore,
				Sandboxes:   workflows.Sandboxes,
				Runtime:     sandboxRuntime,
				Locker:      workflows.Locker,
			},
		},
		tool.SandboxExportFileHandler{
			UseCase: tool.ExportSandboxFile{
				Attachments: workflows.GeneratedAttachments,
				Blobs:       artifactStore,
				Sandboxes:   workflows.Sandboxes,
				Runtime:     sandboxRuntime,
				Files:       sandboxFileReader,
				Locker:      workflows.Locker,
			},
		},
		tool.ConversationExportTextHandler{
			UseCase: tool.ExportTextAttachment{
				Attachments: workflows.GeneratedAttachments,
				Blobs:       artifactStore,
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
	toolDefinitions := tool.DefaultTools(settings.Workflow.ImageGenerationPartials)
	if tavilyEnabled {
		toolDefinitions = tool.DefaultToolsWithTavily(settings.Workflow.ImageGenerationPartials)
	}
	staticCatalog := tool.StaticCatalog{
		Tools:             toolDefinitions,
		EnableSandboxExec: settings.SandboxExecEnabled,
	}
	mcpRuntime := &mcpconfig.CompositeRuntime{
		StaticCatalog: staticCatalog,
		LocalExecutor: toolExecutor,
		Repository:    workflows.MCP,
		Cipher:        workflows.CredentialCipher,
		Client:        &mcpconfig.SDKToolLister{},
	}
	workflowEngine := workflow.New(workflow.Dependencies{
		Logger:                logger,
		Settings:              settings.Workflow,
		Outbox:                workflows.Outbox,
		Turns:                 workflows.Turns,
		Contexts:              workflows.Contexts,
		CompleteEvents:        workflows.CompleteEvents,
		Attachments:           workflows.Attachments,
		StaleTurns:            workflows.StaleTurns,
		Model:                 openaiClient,
		ToolCatalog:           mcpRuntime,
		ToolExecutor:          mcpRuntime,
		Conversations:         workflows.ConversationReader,
		Profiles:              workflows.Profiles,
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
		GeneratedImageAssets:  workflows.GeneratedImageAssets,
		Streams:               streamPublisher,
		ContextCache:          cacheStore,
		SharedContextCache:    sharedContextCache,
		ContextTail:           cacheStore,
		ContextCompaction:     cacheStore,
		Locker:                workflows.Locker,
	})

	reaper := assistantsandbox.NewReaper(settings.SandboxLifecycle, workflows.Sandboxes, sandboxRuntime, workflows.Locker, logger)
	attachmentReaper := assistantattachment.NewReaper(settings.AttachmentCleanup, workflows.AttachmentCleanup, artifactStore, logger)
	runArtifactReferences, ok := workflows.TurnRuns.(workflow.RunArtifactReferenceStore)
	if !ok {
		return nil, fmt.Errorf("turn run repository does not support artifact reference listing")
	}
	runArtifactReaper := workflow.NewRunArtifactReaper(settings.RunArtifactCleanup, runArtifactReferences, artifactStore, logger)
	generatedImageCleanup, ok := workflows.GeneratedImageAssets.(workflow.GeneratedImageAssetCleanupStore)
	if !ok {
		return nil, fmt.Errorf("generated image asset repository does not support preview cleanup")
	}
	generatedImageReaper := workflow.NewGeneratedImageReaper(workflow.GeneratedImageReaperSettings{
		Interval: settings.AttachmentCleanup.Interval, BatchSize: settings.AttachmentCleanup.BatchSize,
	}, generatedImageCleanup, artifactStore, logger)
	return worker.New(logger, workflowEngine, settings.Process, writer, newReader, reaper, attachmentReaper, runArtifactReaper, generatedImageReaper, kafkaStreamPublisher, streamRecovery), nil
}

func buildSandboxRuntime(settings assistantsandbox.RuntimeSettings) (tool.SandboxManager, error) {
	runtime, err := assistantsandbox.NewRuntime(settings)
	if err != nil {
		return nil, fmt.Errorf("configure sandbox runtime: %w", err)
	}
	return runtime, nil
}
