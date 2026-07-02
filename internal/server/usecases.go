package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"time"

	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	"github.com/EurekaMXZ/assistant/internal/domain"
	assistantmail "github.com/EurekaMXZ/assistant/internal/mail"
)

type UpdateConversationInput struct {
	ConversationID string
	Title          *string
	Archived       *bool
}

type ExecConversationSandboxInput struct {
	ConversationID   string
	Command          string
	Args             []string
	WorkingDirectory string
	TimeoutSeconds   int
}

type UploadConversationAttachmentInput struct {
	IdempotencyKey string
	Filename       string
	ContentType    string
	SizeBytes      int64
	File           io.Reader
}

type ConversationAttachmentContent struct {
	Attachment domain.Attachment
	Data       []byte
}

type SendMessageInput struct {
	Content         string
	AttachmentIDs   []string
	ModelID         string
	ReasoningEffort string
	Metadata        json.RawMessage
}

const (
	InitialTurnActionPrepare = "prepare"
	InitialTurnActionCommit  = "commit"
)

type InitialTurnInput struct {
	Action          string
	ConversationID  string
	Title           string
	Content         string
	AttachmentIDs   []string
	ModelID         string
	ReasoningEffort string
	Metadata        json.RawMessage
}

type InitialTurnResult struct {
	State        string              `json:"state"`
	Replayed     bool                `json:"replayed"`
	Conversation domain.Conversation `json:"conversation"`
	Message      *domain.Message     `json:"message,omitempty"`
	Turn         *domain.Turn        `json:"turn,omitempty"`
}

type PageResult[T any] struct {
	Items      []T
	NextCursor string
}

type CreateProviderCredentialInput struct {
	Provider string
	Name     string
	BaseURL  string
	APIKey   string
}

type UpdateProviderCredentialInput struct {
	ID      string
	Name    *string
	BaseURL *string
	Status  *string
}

type CreateModelInput struct {
	Provider                  string
	CredentialID              string
	Slug                      string
	UpstreamModel             string
	DisplayName               string
	Description               string
	InputModalities           []string
	OutputModalities          []string
	SupportsTools             bool
	SupportsParallelTools     bool
	SupportedReasoningEfforts []string
	ContextWindowTokens       int
	MaxOutputTokens           int
	DefaultParameters         json.RawMessage
}

type UpdateModelInput struct {
	ID                        string
	CredentialID              *string
	DisplayName               *string
	Description               *string
	InputModalities           []string
	OutputModalities          []string
	SupportsTools             *bool
	SupportsParallelTools     *bool
	SupportedReasoningEfforts []string
	ContextWindowTokens       *int
	MaxOutputTokens           *int
	DefaultParameters         json.RawMessage
	Status                    *string
}

type CreateModelPriceInput struct {
	ModelID                           string `json:"-"`
	Currency                          string `json:"currency"`
	InputPerMillionNanos              int64  `json:"input_per_million_nanos"`
	CacheReadInputPerMillionNanos     int64  `json:"cache_read_input_per_million_nanos"`
	CacheCreationInputPerMillionNanos int64  `json:"cache_creation_input_per_million_nanos"`
	OutputPerMillionNanos             int64  `json:"output_per_million_nanos"`
	ImageInputPerMillionNanos         *int64 `json:"image_input_per_million_nanos"`
	ImageOutputPerImageNanos          *int64 `json:"image_output_per_image_nanos"`
}

type UpdateModelSettingsInput struct {
	DefaultChatModelID *string
	CompactionModelID  *string
}

type ManualBillingInput struct {
	UserID         string
	Amount         string
	Currency       string
	Reason         string
	Reference      string
	IdempotencyKey string
	RequestID      string
}

type UpdateBillingAccountInput struct {
	UserID string
	Status *string
}

type BillingListInput struct {
	UserID string
	Kind   string
	Status string
	Limit  int
	Cursor string
}

type AuditListInput struct {
	ViewerUserID  string
	ViewerRole    string
	ActorUserID   string
	SubjectUserID string
	Action        string
	ResourceType  string
	Outcome       string
	Limit         int
	Cursor        string
}

type RecordAuditInput struct {
	ActorUserID      string
	ActorRole        string
	SubjectUserID    string
	Action           string
	ResourceType     string
	ResourceID       string
	Outcome          string
	RequestID        string
	ClientIP         string
	UserAgent        string
	Reason           string
	VisibleToSubject bool
	RequiredRole     string
	Metadata         json.RawMessage
}

type AuthUseCases struct {
	AuthenticateAccessToken func(ctx context.Context, accessToken string) (*domain.User, error)
	Register                func(ctx context.Context, input assistantauth.RegisterInput) (*assistantauth.RegistrationResult, error)
	Login                   func(ctx context.Context, input assistantauth.LoginInput) (*assistantauth.Session, error)
	VerifyEmail             func(ctx context.Context, input assistantauth.VerifyEmailInput) (*domain.User, error)
	ResendVerification      func(ctx context.Context, input assistantauth.ResendVerificationInput) error
	ForgotPassword          func(ctx context.Context, input assistantauth.ForgotPasswordInput) error
	ResetPassword           func(ctx context.Context, input assistantauth.ResetPasswordInput) (*domain.User, error)
	ChangeOwnPassword       func(ctx context.Context, input assistantauth.ChangePasswordInput) (*domain.User, error)
}

type UserUseCases struct {
	ListManagedUsers     func(ctx context.Context, actor *domain.User, limit int) ([]domain.User, error)
	GetManagedUser       func(ctx context.Context, actor *domain.User, userID string) (*domain.User, error)
	CreateManagedUser    func(ctx context.Context, actor *domain.User, input assistantauth.CreateManagedUserInput) (*domain.User, error)
	UpdateManagedUser    func(ctx context.Context, actor *domain.User, input assistantauth.UpdateManagedUserInput) (*domain.User, error)
	ResetManagedPassword func(ctx context.Context, actor *domain.User, input assistantauth.ResetManagedPasswordInput) (*domain.User, error)
}

type ConversationUseCases struct {
	CreateConversation func(ctx context.Context, ownerUserID string, title string, metadata json.RawMessage) (*domain.Conversation, error)
	InitialTurn        func(ctx context.Context, ownerUserID string, idempotencyKey string, input InitialTurnInput) (*InitialTurnResult, error)
	ListConversations  func(ctx context.Context, ownerUserID string, limit int) ([]domain.Conversation, error)
	GetConversation    func(ctx context.Context, ownerUserID string, conversationID string) (*domain.Conversation, error)
	UpdateConversation func(ctx context.Context, ownerUserID string, input UpdateConversationInput) (*domain.Conversation, error)
	SendMessage        func(ctx context.Context, ownerUserID string, conversationID string, input SendMessageInput) (*domain.EnqueuedTurn, error)
	ListMessages       func(ctx context.Context, ownerUserID string, conversationID string, limit int) ([]domain.Message, error)
}

type AttachmentUseCases struct {
	UploadConversationAttachment func(ctx context.Context, ownerUserID string, conversationID string, input UploadConversationAttachmentInput) (*domain.Attachment, error)
	GetConversationAttachment    func(ctx context.Context, ownerUserID string, conversationID string, attachmentID string) (*ConversationAttachmentContent, error)
}

type SandboxUseCases struct {
	GetConversationSandbox     func(ctx context.Context, ownerUserID string, conversationID string) (*domain.ConversationSandbox, error)
	CreateConversationSandbox  func(ctx context.Context, ownerUserID string, conversationID string) (*domain.ConversationSandbox, error)
	DestroyConversationSandbox func(ctx context.Context, ownerUserID string, conversationID string) (*domain.ConversationSandbox, error)
	ExecConversationSandbox    func(ctx context.Context, ownerUserID string, input ExecConversationSandboxInput) (*domain.SandboxCommandResult, error)
}

type TurnUseCases struct {
	GetTurn               func(ctx context.Context, ownerUserID string, turnID string) (*domain.Turn, error)
	GetTurnExecutionTrace func(ctx context.Context, ownerUserID string, turnID string) (*TurnExecutionTrace, error)
	GetTurnTimeline       func(ctx context.Context, ownerUserID string, turnID string) (*TurnTimeline, error)
}

type ModelUseCases struct {
	ListModels          func(ctx context.Context, limit int, cursor string) (*PageResult[domain.Model], error)
	GetModel            func(ctx context.Context, modelID string) (*domain.Model, error)
	ListAdminModels     func(ctx context.Context, actor *domain.User, limit int, cursor string) (*PageResult[domain.Model], error)
	GetAdminModel       func(ctx context.Context, actor *domain.User, modelID string) (*domain.Model, error)
	CreateModel         func(ctx context.Context, actor *domain.User, input CreateModelInput) (*domain.Model, error)
	UpdateModel         func(ctx context.Context, actor *domain.User, input UpdateModelInput) (*domain.Model, error)
	ListModelPrices     func(ctx context.Context, actor *domain.User, modelID string) ([]domain.ModelPriceVersion, error)
	GetModelPrice       func(ctx context.Context, actor *domain.User, modelID string, priceID string) (*domain.ModelPriceVersion, error)
	CreateModelPrice    func(ctx context.Context, actor *domain.User, input CreateModelPriceInput) (*domain.ModelPriceVersion, error)
	PublishModelPrice   func(ctx context.Context, actor *domain.User, modelID string, priceID string, effectiveFrom *time.Time) (*domain.ModelPriceVersion, error)
	ArchiveModelPrice   func(ctx context.Context, actor *domain.User, modelID string, priceID string) (*domain.ModelPriceVersion, error)
	GetModelSettings    func(ctx context.Context, actor *domain.User) (*domain.ModelSettings, error)
	UpdateModelSettings func(ctx context.Context, actor *domain.User, input UpdateModelSettingsInput) (*domain.ModelSettings, error)
}

type CredentialUseCases struct {
	ListProviderCredentials    func(ctx context.Context, actor *domain.User, limit int, cursor string) (*PageResult[domain.ProviderCredential], error)
	GetProviderCredential      func(ctx context.Context, actor *domain.User, credentialID string) (*domain.ProviderCredential, error)
	CreateProviderCredential   func(ctx context.Context, actor *domain.User, input CreateProviderCredentialInput) (*domain.ProviderCredential, error)
	UpdateProviderCredential   func(ctx context.Context, actor *domain.User, input UpdateProviderCredentialInput) (*domain.ProviderCredential, error)
	RotateProviderCredential   func(ctx context.Context, actor *domain.User, credentialID string, apiKey string) (*domain.ProviderCredential, error)
	ValidateProviderCredential func(ctx context.Context, actor *domain.User, credentialID string) (*domain.ProviderCredential, error)
	RevokeProviderCredential   func(ctx context.Context, actor *domain.User, credentialID string) (*domain.ProviderCredential, error)
}

type BillingUseCases struct {
	GetBillingAccount       func(ctx context.Context, userID string) (*domain.BillingAccount, error)
	ListBillingAccounts     func(ctx context.Context, actor *domain.User, limit int, cursor string) (*PageResult[domain.BillingAccount], error)
	UpdateBillingAccount    func(ctx context.Context, actor *domain.User, input UpdateBillingAccountInput) (*domain.BillingAccount, error)
	ApplyManualTopup        func(ctx context.Context, actor *domain.User, input ManualBillingInput) (*domain.BillingTransaction, error)
	ApplyManualRefund       func(ctx context.Context, actor *domain.User, input ManualBillingInput) (*domain.BillingTransaction, error)
	ListBillingTransactions func(ctx context.Context, input BillingListInput) (*PageResult[domain.BillingTransaction], error)
	GetBillingTransaction   func(ctx context.Context, transactionID string, userID string) (*domain.BillingTransaction, error)
	ListBillingUsageEvents  func(ctx context.Context, input BillingListInput) (*PageResult[domain.BillingUsageEvent], error)
	GetBillingUsageEvent    func(ctx context.Context, usageEventID string, userID string) (*domain.BillingUsageEvent, error)
}

type AuditUseCases struct {
	ListAuditEvents func(ctx context.Context, input AuditListInput) (*PageResult[domain.AuditEvent], error)
	GetAuditEvent   func(ctx context.Context, eventID string, viewerUserID string, viewerRole string) (*domain.AuditEvent, error)
	RecordAudit     func(ctx context.Context, input RecordAuditInput) error
}

type MailUseCases struct {
	GetMailSettings    func(ctx context.Context, actor *domain.User) (*assistantmail.Settings, error)
	UpdateMailSettings func(ctx context.Context, actor *domain.User, input assistantmail.UpdateSettingsInput) (*assistantmail.Settings, error)
	TestMailSettings   func(ctx context.Context, actor *domain.User, recipient string) error
}

type UseCases struct {
	Auth          AuthUseCases
	Users         UserUseCases
	Conversations ConversationUseCases
	Attachments   AttachmentUseCases
	Sandboxes     SandboxUseCases
	Turns         TurnUseCases
	Models        ModelUseCases
	Credentials   CredentialUseCases
	Billing       BillingUseCases
	Audit         AuditUseCases
	Mail          MailUseCases
}

func (u UseCases) validate() error {
	root := reflect.ValueOf(u)
	typeOfRoot := root.Type()
	for groupIndex := 0; groupIndex < root.NumField(); groupIndex++ {
		group := root.Field(groupIndex)
		groupType := typeOfRoot.Field(groupIndex)
		for dependencyIndex := 0; dependencyIndex < group.NumField(); dependencyIndex++ {
			if group.Field(dependencyIndex).IsNil() {
				dependencyType := group.Type().Field(dependencyIndex)
				return fmt.Errorf("server dependency %s.%s is required", groupType.Name, dependencyType.Name)
			}
		}
	}
	return nil
}
