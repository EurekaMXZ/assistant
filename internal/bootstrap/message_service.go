package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/EurekaMXZ/assistant/internal/billing"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/postgres"
	"github.com/EurekaMXZ/assistant/internal/server"
)

type ownedConversationStore interface {
	GetConversationByOwner(ctx context.Context, conversationID string, ownerUserID string) (*domain.Conversation, error)
}

type messageAttachmentStore interface {
	ListAttachmentsByIDs(ctx context.Context, conversationID string, ids []string) ([]domain.Attachment, error)
}

type modelExecutionResolver interface {
	ResolveExecution(ctx context.Context, modelID string, compaction bool) (*domain.ModelExecutionSnapshot, error)
}

type billingAdmissionStore interface {
	GetOrCreateAccount(ctx context.Context, userID string, currency string) (*domain.BillingAccount, error)
}

type userTurnCreator interface {
	CreateUserTurn(ctx context.Context, params postgres.CreateUserTurnParams) (*domain.EnqueuedTurn, error)
}

type initialTurnStore interface {
	Prepare(ctx context.Context, params postgres.PrepareInitialConversationParams) (*postgres.PreparedInitialConversation, error)
	Replay(ctx context.Context, ownerUserID string, idempotencyKey string, conversationID string, commitFingerprint string) (*postgres.CommittedInitialTurn, bool, error)
	Commit(ctx context.Context, params postgres.CommitInitialTurnParams) (*postgres.CommittedInitialTurn, error)
}

type MessageService struct {
	Conversations ownedConversationStore
	Attachments   messageAttachmentStore
	Models        modelExecutionResolver
	Billing       billingAdmissionStore
	Turns         userTurnCreator
}

func (s *MessageService) SendMessage(ctx context.Context, ownerUserID string, conversationID string, input server.SendMessageInput) (*domain.EnqueuedTurn, error) {
	params, err := s.prepareTurn(ctx, ownerUserID, conversationID, input)
	if err != nil {
		return nil, err
	}
	return s.Turns.CreateUserTurn(ctx, params)
}

func (s *MessageService) prepareTurn(ctx context.Context, ownerUserID string, conversationID string, input server.SendMessageInput) (postgres.CreateUserTurnParams, error) {
	if strings.TrimSpace(input.Content) == "" && len(input.AttachmentIDs) == 0 {
		return postgres.CreateUserTurnParams{}, domain.NewValidationError("content is required")
	}
	if _, err := s.Conversations.GetConversationByOwner(ctx, conversationID, ownerUserID); err != nil {
		return postgres.CreateUserTurnParams{}, err
	}
	attachmentIDs, err := assistantattachment.NormalizeAttachmentIDs(input.AttachmentIDs)
	if err != nil {
		return postgres.CreateUserTurnParams{}, err
	}
	attachments, err := s.Attachments.ListAttachmentsByIDs(ctx, conversationID, attachmentIDs)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return postgres.CreateUserTurnParams{}, domain.NewValidationError("one or more attachments were not found")
		}
		return postgres.CreateUserTurnParams{}, err
	}
	metadata, err := buildMessageMetadata(input.Metadata, attachments)
	if err != nil {
		return postgres.CreateUserTurnParams{}, err
	}
	modelSnapshot, err := s.Models.ResolveExecution(ctx, input.ModelID, false)
	if err != nil {
		return postgres.CreateUserTurnParams{}, err
	}
	if err := applyRequestedReasoningEffort(modelSnapshot, input.ReasoningEffort); err != nil {
		return postgres.CreateUserTurnParams{}, err
	}
	currency, maximumCharge, err := billing.MaximumSnapshotCharge(modelSnapshot.PricingSnapshot, modelSnapshot.ContextWindowTokens, modelSnapshot.MaxOutputTokens)
	if err != nil {
		return postgres.CreateUserTurnParams{}, err
	}
	account, err := s.Billing.GetOrCreateAccount(ctx, ownerUserID, currency)
	if err != nil {
		return postgres.CreateUserTurnParams{}, err
	}
	if account.Status != "active" {
		return postgres.CreateUserTurnParams{}, domain.ErrForbidden
	}
	if account.BalanceNanos < maximumCharge {
		return postgres.CreateUserTurnParams{}, domain.NewPaymentRequiredError("billing account balance is insufficient")
	}

	metadataFields := decodeMetadata(metadata)
	metadataFields["model_id"] = modelSnapshot.ModelID
	metadataFields["model_revision"] = modelSnapshot.ModelRevision
	metadataFields["model_price_id"] = modelSnapshot.ModelPriceID
	if modelSnapshot.ReasoningEffort != "" {
		metadataFields["reasoning_effort"] = modelSnapshot.ReasoningEffort
	}
	metadata, err = json.Marshal(metadataFields)
	if err != nil {
		return postgres.CreateUserTurnParams{}, fmt.Errorf("marshal turn metadata: %w", err)
	}
	return postgres.CreateUserTurnParams{
		ConversationID: conversationID,
		Content:        input.Content,
		Metadata:       metadata,
		ModelSnapshot:  *modelSnapshot,
	}, nil
}

type InitialTurnService struct {
	Messages *MessageService
	Store    initialTurnStore
}

func (s *InitialTurnService) Execute(ctx context.Context, ownerUserID string, idempotencyKey string, input server.InitialTurnInput) (*server.InitialTurnResult, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if strings.TrimSpace(ownerUserID) == "" || idempotencyKey == "" || len(idempotencyKey) > 128 {
		return nil, domain.NewValidationError("owner user id and a valid idempotency key are required")
	}
	switch input.Action {
	case server.InitialTurnActionPrepare:
		fingerprint, err := initialTurnFingerprint(struct {
			Title    string          `json:"title"`
			Metadata json.RawMessage `json:"metadata"`
		}{Title: strings.TrimSpace(input.Title), Metadata: canonicalMetadata(input.Metadata)})
		if err != nil {
			return nil, err
		}
		prepared, err := s.Store.Prepare(ctx, postgres.PrepareInitialConversationParams{
			OwnerUserID: ownerUserID, IdempotencyKey: idempotencyKey, Title: input.Title,
			Metadata: input.Metadata, PrepareFingerprint: fingerprint,
		})
		if err != nil {
			return nil, err
		}
		return &server.InitialTurnResult{State: "draft", Replayed: prepared.Replayed, Conversation: prepared.Conversation}, nil

	case server.InitialTurnActionCommit:
		fingerprint, err := initialTurnFingerprint(struct {
			ConversationID  string          `json:"conversation_id"`
			Content         string          `json:"content"`
			AttachmentIDs   []string        `json:"attachment_ids"`
			ModelID         string          `json:"model_id"`
			ReasoningEffort string          `json:"reasoning_effort"`
			Metadata        json.RawMessage `json:"metadata"`
		}{
			ConversationID: strings.TrimSpace(input.ConversationID), Content: input.Content,
			AttachmentIDs: input.AttachmentIDs, ModelID: strings.TrimSpace(input.ModelID),
			ReasoningEffort: strings.ToLower(strings.TrimSpace(input.ReasoningEffort)), Metadata: canonicalMetadata(input.Metadata),
		})
		if err != nil {
			return nil, err
		}
		if replayed, ok, err := s.Store.Replay(ctx, ownerUserID, idempotencyKey, input.ConversationID, fingerprint); err != nil {
			return nil, err
		} else if ok {
			return initialTurnResult(replayed), nil
		}
		turn, err := s.Messages.prepareTurn(ctx, ownerUserID, input.ConversationID, server.SendMessageInput{
			Content: input.Content, AttachmentIDs: input.AttachmentIDs, ModelID: input.ModelID,
			ReasoningEffort: input.ReasoningEffort, Metadata: input.Metadata,
		})
		if err != nil {
			return nil, err
		}
		committed, err := s.Store.Commit(ctx, postgres.CommitInitialTurnParams{
			OwnerUserID: ownerUserID, IdempotencyKey: idempotencyKey, CommitFingerprint: fingerprint, Turn: turn,
		})
		if err != nil {
			return nil, err
		}
		return initialTurnResult(committed), nil
	default:
		return nil, domain.NewValidationError("action must be prepare or commit")
	}
}

func initialTurnResult(committed *postgres.CommittedInitialTurn) *server.InitialTurnResult {
	message := committed.EnqueuedTurn.Message
	turn := committed.EnqueuedTurn.Turn
	return &server.InitialTurnResult{
		State: "committed", Replayed: committed.Replayed, Conversation: committed.Conversation,
		Message: &message, Turn: &turn,
	}
}

func buildMessageMetadata(metadata json.RawMessage, attachments []domain.Attachment) (json.RawMessage, error) {
	payload := decodeMetadata(metadata)
	if len(attachments) > 0 {
		ids := make([]string, 0, len(attachments))
		summaries := make([]map[string]any, 0, len(attachments))
		for _, item := range attachments {
			ids = append(ids, item.ID)
			summaries = append(summaries, map[string]any{
				"id": item.ID, "filename": item.Filename, "content_type": item.ContentType,
				"category": item.Category, "size_bytes": item.SizeBytes,
			})
		}
		payload["attachment_ids"] = ids
		payload["attachments"] = summaries
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal message metadata: %w", err)
	}
	return raw, nil
}

func decodeMetadata(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
		return map[string]any{}
	}
	return decoded
}

func canonicalMetadata(raw json.RawMessage) json.RawMessage {
	encoded, _ := json.Marshal(decodeMetadata(raw))
	return encoded
}

func initialTurnFingerprint(input any) (string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal initial turn fingerprint: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func applyRequestedReasoningEffort(snapshot *domain.ModelExecutionSnapshot, requested string) error {
	effort := strings.ToLower(strings.TrimSpace(requested))
	if effort == "" {
		return nil
	}
	if !slices.Contains(snapshot.SupportedReasoningEfforts, effort) {
		return domain.NewValidationError("selected model does not support the requested reasoning_effort")
	}
	snapshot.ReasoningEffort = effort
	return nil
}
