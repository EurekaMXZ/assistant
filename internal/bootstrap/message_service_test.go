package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/postgres"
	"github.com/EurekaMXZ/assistant/internal/server"
)

type stubOwnedConversations struct {
	conversation *domain.Conversation
	calls        int
}

func (s *stubOwnedConversations) GetConversationByOwner(_ context.Context, conversationID string, ownerUserID string) (*domain.Conversation, error) {
	s.calls++
	if s.conversation == nil || s.conversation.ID != conversationID || s.conversation.OwnerUserID != ownerUserID {
		return nil, domain.ErrNotFound
	}
	return s.conversation, nil
}

type stubMessageAttachments struct {
	items   []domain.Attachment
	lastIDs []string
	calls   int
}

func (s *stubMessageAttachments) ListAttachmentsByIDs(_ context.Context, conversationID string, ids []string) ([]domain.Attachment, error) {
	s.calls++
	s.lastIDs = append([]string(nil), ids...)
	if len(s.items) > 0 && s.items[0].ConversationID != conversationID {
		return nil, domain.ErrNotFound
	}
	return append([]domain.Attachment(nil), s.items...), nil
}

type stubModelResolver struct {
	snapshot domain.ModelExecutionSnapshot
	calls    int
}

func (s *stubModelResolver) ResolveExecution(context.Context, string, bool) (*domain.ModelExecutionSnapshot, error) {
	s.calls++
	result := s.snapshot
	return &result, nil
}

type stubBillingAdmission struct {
	account domain.BillingAccount
	calls   int
}

func (s *stubBillingAdmission) GetOrCreateAccount(context.Context, string, string) (*domain.BillingAccount, error) {
	s.calls++
	result := s.account
	return &result, nil
}

type stubTurnCreator struct {
	params postgres.CreateUserTurnParams
}

func (s *stubTurnCreator) CreateUserTurn(_ context.Context, params postgres.CreateUserTurnParams) (*domain.EnqueuedTurn, error) {
	s.params = params
	return &domain.EnqueuedTurn{
		ConversationID: params.ConversationID,
		Message:        domain.Message{ID: "message-1", ConversationID: params.ConversationID},
		Turn:           domain.Turn{ID: "turn-1", ConversationID: params.ConversationID},
	}, nil
}

func TestMessageServiceOrchestratesValidatedUserTurn(t *testing.T) {
	attachmentID := "4ff17288-4fbe-4b2d-9d1d-aaba6db680dc"
	conversations := &stubOwnedConversations{conversation: &domain.Conversation{ID: "conversation-1", OwnerUserID: "user-1"}}
	attachments := &stubMessageAttachments{items: []domain.Attachment{{
		ID: attachmentID, ConversationID: "conversation-1", Filename: "notes.txt",
		ContentType: "text/plain", Category: domain.AttachmentCategoryText, SizeBytes: 12,
	}}}
	models := &stubModelResolver{snapshot: testModelSnapshot()}
	accounts := &stubBillingAdmission{account: domain.BillingAccount{Status: "active", BalanceNanos: 1_000_000}}
	turns := &stubTurnCreator{}
	service := &MessageService{Conversations: conversations, Attachments: attachments, Models: models, Billing: accounts, Turns: turns}

	result, err := service.SendMessage(t.Context(), "user-1", "conversation-1", server.SendMessageInput{
		Content: "hello", AttachmentIDs: []string{attachmentID}, ModelID: "model-1",
		ReasoningEffort: "HIGH", Metadata: json.RawMessage(`{"client":"web"}`),
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if result.Turn.ID != "turn-1" || turns.params.ModelSnapshot.ReasoningEffort != "high" {
		t.Fatalf("unexpected enqueued turn: result=%#v params=%#v", result, turns.params)
	}
	var metadata map[string]any
	if err := json.Unmarshal(turns.params.Metadata, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata["model_id"] != "model-1" || metadata["reasoning_effort"] != "high" {
		t.Fatalf("model metadata = %#v", metadata)
	}
	ids, ok := metadata["attachment_ids"].([]any)
	if !ok || len(ids) != 1 || ids[0] != attachmentID {
		t.Fatalf("attachment metadata = %#v", metadata)
	}
	if conversations.calls != 1 || attachments.calls != 1 || models.calls != 1 || accounts.calls != 1 {
		t.Fatalf("unexpected dependency calls: conversations=%d attachments=%d models=%d billing=%d", conversations.calls, attachments.calls, models.calls, accounts.calls)
	}
}

type stubInitialTurnStore struct {
	conversation domain.Conversation
	committed    *postgres.CommittedInitialTurn
	commitCalls  int
	failCommit   bool
}

func (s *stubInitialTurnStore) Prepare(_ context.Context, params postgres.PrepareInitialConversationParams) (*postgres.PreparedInitialConversation, error) {
	if s.conversation.ID == "" {
		s.conversation = domain.Conversation{ID: "draft-1", OwnerUserID: params.OwnerUserID, Title: params.Title}
		return &postgres.PreparedInitialConversation{Conversation: s.conversation}, nil
	}
	return &postgres.PreparedInitialConversation{Conversation: s.conversation, Replayed: true}, nil
}

func (s *stubInitialTurnStore) Replay(_ context.Context, _ string, _ string, conversationID string, _ string) (*postgres.CommittedInitialTurn, bool, error) {
	if conversationID != s.conversation.ID {
		return nil, false, domain.ErrConflict
	}
	if s.committed == nil {
		return nil, false, nil
	}
	replayed := *s.committed
	replayed.Replayed = true
	return &replayed, true, nil
}

func (s *stubInitialTurnStore) Commit(_ context.Context, params postgres.CommitInitialTurnParams) (*postgres.CommittedInitialTurn, error) {
	s.commitCalls++
	if s.failCommit {
		s.failCommit = false
		return nil, errors.New("temporary commit failure")
	}
	s.committed = &postgres.CommittedInitialTurn{
		Conversation: s.conversation,
		EnqueuedTurn: domain.EnqueuedTurn{
			ConversationID: s.conversation.ID,
			Message:        domain.Message{ID: "message-1", ConversationID: s.conversation.ID},
			Turn:           domain.Turn{ID: "turn-1", ConversationID: s.conversation.ID},
		},
	}
	if params.Turn.ConversationID != s.conversation.ID {
		return nil, domain.ErrConflict
	}
	return s.committed, nil
}

func TestInitialTurnServiceResumesDraftAfterCommitFailureAndReplaysSuccess(t *testing.T) {
	attachmentID := "4ff17288-4fbe-4b2d-9d1d-aaba6db680dc"
	store := &stubInitialTurnStore{failCommit: true}
	conversations := &stubOwnedConversations{}
	attachments := &stubMessageAttachments{items: []domain.Attachment{{ID: attachmentID, ConversationID: "draft-1"}}}
	models := &stubModelResolver{snapshot: testModelSnapshot()}
	accounts := &stubBillingAdmission{account: domain.BillingAccount{Status: "active", BalanceNanos: 1_000_000}}
	service := &InitialTurnService{
		Store:    store,
		Messages: &MessageService{Conversations: conversations, Attachments: attachments, Models: models, Billing: accounts, Turns: &stubTurnCreator{}},
	}

	prepared, err := service.Execute(t.Context(), "user-1", "request-1", server.InitialTurnInput{Action: server.InitialTurnActionPrepare, Title: "New chat"})
	if err != nil {
		t.Fatalf("prepare error = %v", err)
	}
	conversations.conversation = &prepared.Conversation
	commitInput := server.InitialTurnInput{
		Action: server.InitialTurnActionCommit, ConversationID: prepared.Conversation.ID,
		Content: "hello", AttachmentIDs: []string{attachmentID}, ModelID: "model-1",
	}
	if _, err := service.Execute(t.Context(), "user-1", "request-1", commitInput); err == nil || err.Error() != "temporary commit failure" {
		t.Fatalf("first commit error = %v", err)
	}
	committed, err := service.Execute(t.Context(), "user-1", "request-1", commitInput)
	if err != nil {
		t.Fatalf("retry commit error = %v", err)
	}
	if committed.Conversation.ID != prepared.Conversation.ID || committed.Turn == nil || committed.Turn.ID != "turn-1" {
		t.Fatalf("retry changed draft/result: prepared=%#v committed=%#v", prepared, committed)
	}
	if store.commitCalls != 2 || attachments.calls != 2 {
		t.Fatalf("retry calls: commits=%d attachments=%d", store.commitCalls, attachments.calls)
	}

	replayed, err := service.Execute(t.Context(), "user-1", "request-1", commitInput)
	if err != nil {
		t.Fatalf("replay error = %v", err)
	}
	if !replayed.Replayed || store.commitCalls != 2 || models.calls != 2 || accounts.calls != 2 {
		t.Fatalf("completed request was not replayed without admission: result=%#v commits=%d models=%d billing=%d", replayed, store.commitCalls, models.calls, accounts.calls)
	}
}

func testModelSnapshot() domain.ModelExecutionSnapshot {
	return domain.ModelExecutionSnapshot{
		ModelID: "model-1", ModelRevision: 2, ModelPriceID: "price-1",
		ContextWindowTokens: 100, MaxOutputTokens: 10,
		SupportedReasoningEfforts: []string{"low", "high"},
		PricingSnapshot:           json.RawMessage(`{"currency":"USD","input_per_million_nanos":1,"cache_read_input_per_million_nanos":1,"cache_creation_input_per_million_nanos":1,"output_per_million_nanos":1}`),
	}
}
