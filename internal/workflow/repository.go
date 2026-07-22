package workflow

import (
	"context"
	"encoding/json"
	"time"

	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/EurekaMXZ/assistant/internal/domain"

	"github.com/EurekaMXZ/assistant/internal/llm"

	"github.com/EurekaMXZ/assistant/internal/tool"
)

type WorkflowOutboxRepository interface {
	ClaimPendingOutboxEvents(ctx context.Context, leaseTimeout time.Duration, limit int) ([]OutboxEvent, error)
	MarkOutboxPublished(ctx context.Context, eventID string, claimToken string) error
	MarkOutboxPublishError(ctx context.Context, eventID string, claimToken string, message string) error
}

type TurnWorkflowRepository interface {
	GetTurn(ctx context.Context, turnID string) (*domain.Turn, error)
	GetUserMessageByTurn(ctx context.Context, turnID string) (*domain.Message, error)
	MarkTurnContextReady(ctx context.Context, turnID string) (*domain.Turn, error)
	FinalizeTurnSuccess(ctx context.Context, turnID string, assistantMessages []domain.AssistantMessageDraft, summary domain.TurnRunSummary, compactTriggerTokens int) (*domain.Turn, []domain.Message, *domain.ContextHead, bool, error)
	FinalizeTurnFailure(ctx context.Context, turnID string, requestKey string, streamKey string, code string, message string, compactTriggerTokens int) error
}

type WorkflowContextRepository interface {
	GetContextHead(ctx context.Context, conversationID string) (*domain.ContextHead, error)
	HasActiveRetry(ctx context.Context, conversationID string) (bool, error)
	ListRawTailMessages(ctx context.Context, conversationID string, fromSeq int64, toSeq int64) ([]domain.Message, error)
	CompleteCompaction(ctx context.Context, conversationID string, anchor domain.AnchorObject, expectedLastSeq int64, activeContextTokens int) (*domain.ContextHead, error)
}

type CompleteEventStore interface {
	AppendCompleteEvent(ctx context.Context, input domain.ConversationEventInput) (*domain.ConversationEvent, error)
	ListContextEvents(ctx context.Context, conversationID string, fromSeq int64, toSeq int64) ([]domain.ConversationEvent, error)
	ListConversationEvents(ctx context.Context, conversationID string, limit int, beforeSeq int64, afterSeq int64) ([]domain.ConversationEvent, error)
}

type CompleteEventRunStore interface {
	ListConversationEventsByRun(ctx context.Context, runID string) ([]domain.ConversationEvent, error)
}

type AttachmentStore interface {
	ListAttachmentsByIDs(ctx context.Context, conversationID string, ids []string) ([]domain.Attachment, error)
}

type GeneratedAttachmentStore interface {
	UpsertAttachment(ctx context.Context, params assistantattachment.CreateAttachmentParams) (*domain.Attachment, error)
}

type ConversationReader interface {
	GetConversation(ctx context.Context, conversationID string) (*domain.Conversation, error)
}

type PersonalizationReader interface {
	GetUserPreferences(ctx context.Context, userID string) (*domain.UserPreferences, error)
	GetUserLocation(ctx context.Context, userID string) (*domain.UserLocation, error)
}

type ModelCatalogResolver interface {
	ResolveExecution(ctx context.Context, modelID string, compaction bool) (*domain.ModelExecutionSnapshot, error)
	GetTurnExecution(ctx context.Context, turnID string) (*domain.ModelExecutionSnapshot, error)
}

type CompactionUsageRecorder interface {
	RecordCompactionUsage(ctx context.Context, conversationID string, turnID string, requestKey string, execution domain.ModelExecutionSnapshot, result *llm.ModelResult, requestError string) error
}

type AttachmentBlobStore interface {
	GetBytes(ctx context.Context, key string) ([]byte, error)
}

type StaleTurnRepository interface {
	RequeueStaleTurns(ctx context.Context, leaseTimeout time.Duration) (int, error)
	RequeueStaleTurnRuns(ctx context.Context, leaseTimeout time.Duration) (int, error)
}

type TurnRunLease struct {
	TurnID string
	RunID  string
	Token  string
}

type AwaitScheduledTurnRunInput struct {
	Lease                TurnRunLease
	ToolCallID           string
	Interaction          json.RawMessage
	Usage                llm.ModelUsage
	ImageGenerationCount int
	CompactTriggerTokens int
}

type AskUserAnswerClaim struct {
	ToolCall       *domain.ToolCallRecord
	ConversationID string
	Finalized      bool
}

type TurnRunWorkflowStore interface {
	StartTurnRun(ctx context.Context, turnID string, provider string, model string, requestBlobKey string, stateBlobKey string) (string, error)
	ScheduleNextTurnRun(ctx context.Context, turnID string, previousRunID string, stepIndex int, provider string, model string, requestBlobKey string, stateBlobKey string) (string, error)
	GetTurnRun(ctx context.Context, runID string) (*domain.TurnRun, error)
	ClaimTurnRun(ctx context.Context, runID string) (*domain.TurnRun, TurnRunLease, error)
	RenewTurnRunLease(ctx context.Context, lease TurnRunLease) error
	CheckpointScheduledTurnRun(ctx context.Context, lease TurnRunLease, responseID string, responseBlobKey string, resultBlobKey string) error
	AwaitScheduledTurnRunInput(ctx context.Context, input AwaitScheduledTurnRunInput) (*domain.TurnRun, error)
	CompleteScheduledTurnRun(ctx context.Context, lease TurnRunLease, responseID string, responseBlobKey string, resultBlobKey string, usage llm.ModelUsage, imageGenerationCount int, compactTriggerTokens int) (*domain.TurnRun, error)
	FailScheduledTurnRun(ctx context.Context, lease TurnRunLease, responseID string, responseBlobKey string, resultBlobKey string, runMessage string, requestBlobKey string, streamBlobKey string, turnCode string, turnMessage string, compactTriggerTokens int) (*domain.TurnRun, error)
}

type TurnRunArtifactIndexer interface {
	SetTurnRunArtifactMetadata(ctx context.Context, runID string, artifacts []RunArtifactMetadata) error
}

type RunArtifactReferenceStore interface {
	ListReferencedRunArtifactKeys(ctx context.Context) ([]string, error)
}

type TurnCancellationStore interface {
	FinalizeTurnCancellation(ctx context.Context, conversationID string, turnID string) error
}

type ToolCallStore interface {
	AcquireToolCall(ctx context.Context, turnID string, turnRunID string, executionAttempt int, call tool.ToolCall, argumentsBlobKey string) (*domain.ToolCallRecord, bool, error)
	CompleteToolCall(ctx context.Context, recordID string, outputBlobKey string) (*domain.ToolCallRecord, error)
	FailToolCall(ctx context.Context, recordID string, outputBlobKey string, message string) (*domain.ToolCallRecord, error)
	GetToolCallForAnswer(ctx context.Context, ownerUserID string, turnID string, toolCallID string) (*domain.ToolCallRecord, error)
	ClaimAwaitingInputAnswer(ctx context.Context, ownerUserID string, turnID string, toolCallID string, answerKey string, answerFingerprint string, answerOptionID string, outputBlobKey string) (*AskUserAnswerClaim, error)
	FinalizeAwaitingInputAnswer(ctx context.Context, ownerUserID string, turnID string, toolCallID string, answerKey string, answerFingerprint string, answerOptionID string, outputBlobKey string, interaction json.RawMessage) (*domain.ToolCallRecord, bool, error)
}

type TurnStreamEventStore interface {
	AppendTurnStreamEvent(ctx context.Context, conversationID string, turnID string, eventType string, payload json.RawMessage) (*domain.TurnStreamEvent, error)
}
