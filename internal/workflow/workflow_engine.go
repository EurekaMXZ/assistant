package workflow

import (
	"context"
	"errors"
	"log"

	"github.com/EurekaMXZ/assistant/internal/cache"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

type Dependencies struct {
	Logger                *log.Logger
	Settings              WorkflowSettings
	Outbox                WorkflowOutboxRepository
	Turns                 TurnWorkflowRepository
	Contexts              WorkflowContextRepository
	CompleteEvents        CompleteEventStore
	StaleTurns            StaleTurnRepository
	Model                 llm.ModelClient
	ToolCatalog           tool.ToolCatalog
	ToolExecutor          tool.ToolExecutor
	ConversationSandboxes tool.ConversationSandboxReader
	Conversations         ConversationReader
	Profiles              PersonalizationReader
	Models                ModelCatalogResolver
	BillingUsage          CompactionUsageRecorder
	ToolArtifacts         ToolArtifactStore
	TurnRuns              TurnRunWorkflowStore
	ToolCalls             ToolCallStore
	TurnArtifacts         TurnArtifactStore
	ContextAnchors        ContextAnchorStore
	Attachments           AttachmentStore
	GeneratedAttachments  GeneratedAttachmentStore
	AttachmentBlobs       AttachmentBlobStore
	Streams               stream.Publisher
	ContextCache          cache.ContextSnapshotCache
	SharedContextCache    cache.SharedContextSnapshotCache
	ContextTail           cache.ContextTailAppender
	ContextCompaction     cache.ContextCompactionCache
	Locker                ConversationLocker
}

type Engine struct {
	locker        ConversationLocker
	conversations ConversationReader
	turns         *TurnRunner
	compactor     *ContextCompactor
	outbox        *OutboxRelay
	requeue       *StaleTurnRequeuer
}

func New(deps Dependencies) *Engine {
	checkpointStore, _ := deps.ContextAnchors.(ContextCheckpointStore)
	runEvents, _ := deps.CompleteEvents.(CompleteEventRunStore)
	loader := &ContextLoader{
		store:           deps.Contexts,
		completeEvents:  deps.CompleteEvents,
		blobs:           deps.ContextAnchors,
		cache:           deps.ContextCache,
		sharedCache:     deps.SharedContextCache,
		modelContexts:   deps.TurnArtifacts,
		attachments:     deps.Attachments,
		attachmentBlobs: deps.AttachmentBlobs,
	}
	orchestrator := NewToolOrchestrator(deps.Model, deps.ToolCatalog, deps.ToolExecutor, deps.Streams, deps.ToolArtifacts, deps.ToolCalls)
	orchestrator.remoteToolReplayMaxBytes = deps.Settings.RemoteToolReplayMaxBytes
	orchestrator.modelToolOutputMaxTokens = deps.Settings.ModelToolOutputMaxTokens

	return &Engine{
		locker:        deps.Locker,
		conversations: deps.Conversations,
		turns: &TurnRunner{
			logger:               deps.Logger,
			settings:             deps.Settings,
			store:                deps.Turns,
			tools:                orchestrator,
			blobs:                deps.TurnArtifacts,
			streamHub:            deps.Streams,
			cache:                deps.ContextTail,
			loader:               loader,
			conversations:        deps.Conversations,
			profiles:             deps.Profiles,
			generatedAttachments: deps.GeneratedAttachments,
			sandboxes:            deps.ConversationSandboxes,
			runs:                 deps.TurnRuns,
			completeEvents:       runEvents,
			models:               deps.Models,
		},
		compactor: &ContextCompactor{
			settings:      deps.Settings,
			store:         deps.Contexts,
			model:         deps.Model,
			blobs:         deps.ContextAnchors,
			checkpoints:   checkpointStore,
			cache:         deps.ContextCompaction,
			loader:        loader,
			tools:         orchestrator,
			sandboxes:     deps.ConversationSandboxes,
			models:        deps.Models,
			billing:       deps.BillingUsage,
			conversations: deps.Conversations,
		},
		outbox: &OutboxRelay{
			settings: deps.Settings,
			store:    deps.Outbox,
		},
		requeue: &StaleTurnRequeuer{
			settings: deps.Settings,
			store:    deps.StaleTurns,
		},
	}
}

func (e *Engine) HandleWorkflowEvent(ctx context.Context, event WorkflowEvent) error {
	switch event.EventType {
	case EventTurnAccepted:
		if event.ConversationID == "" {
			return nil
		}
		err := e.locker.WithConversationLock(ctx, event.ConversationID, func(ctx context.Context) error {
			if e.compactor != nil {
				if err := e.compactor.HandleRequested(ctx, event); err != nil {
					return err
				}
			}
			return e.turns.HandleAccepted(ctx, event)
		})
		return e.ignoreDeletedConversation(ctx, event.ConversationID, err)
	case EventTurnContextReady:
		if event.ConversationID == "" {
			return nil
		}
		err := e.locker.WithConversationLock(ctx, event.ConversationID, func(ctx context.Context) error {
			return e.turns.HandleContextReady(ctx, event)
		})
		return e.ignoreDeletedConversation(ctx, event.ConversationID, err)
	case EventTurnRunRequested:
		return e.turns.HandleTurnRunRequested(ctx, event)
	case EventTurnCancellationRequested:
		return e.turns.HandleCancellationRequested(ctx, event)
	case EventContextCompactionRequest:
		if event.ConversationID == "" {
			return nil
		}
		err := e.locker.WithConversationLock(ctx, event.ConversationID, func(ctx context.Context) error {
			return e.compactor.HandleRequested(ctx, event)
		})
		return e.ignoreDeletedConversation(ctx, event.ConversationID, err)
	default:
		return nil
	}
}

func (e *Engine) ignoreDeletedConversation(ctx context.Context, conversationID string, eventErr error) error {
	if eventErr == nil || !errors.Is(eventErr, domain.ErrNotFound) || e.conversations == nil {
		return eventErr
	}
	_, lookupErr := e.conversations.GetConversation(ctx, conversationID)
	if errors.Is(lookupErr, domain.ErrNotFound) {
		return nil
	}
	return eventErr
}

func (e *Engine) FlushOutbox(ctx context.Context, publish WorkflowEventPublisher) error {
	return e.outbox.Flush(ctx, publish)
}

func (e *Engine) RequeueStaleTurns(ctx context.Context) (int, error) {
	return e.requeue.Requeue(ctx)
}
