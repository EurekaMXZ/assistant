package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	"github.com/EurekaMXZ/assistant/internal/domain"
	assistantmail "github.com/EurekaMXZ/assistant/internal/mail"
	"github.com/EurekaMXZ/assistant/internal/mcpconfig"
	"github.com/EurekaMXZ/assistant/internal/profile"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

type UpdateConversationInput struct {
	ConversationID string
	Title          *string
	Archived       *bool
}

type CreateConversationShareResult struct {
	Share    domain.ConversationShare `json:"share"`
	Replayed bool                     `json:"replayed"`
}

type ExecConversationSandboxInput struct {
	ConversationID   string
	Command          string
	Args             []string
	WorkingDirectory string
	TimeoutSeconds   int
}

type CreateConversationAttachmentUploadInput struct {
	IdempotencyKey string
	Filename       string
	ContentType    string
	SizeBytes      int64
	SHA256         string
	ContentMD5     string
}

type CompleteConversationAttachmentUploadInput struct{}

type PresignedObjectURL struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers,omitempty"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type ConversationAttachmentUpload struct {
	Attachment domain.Attachment   `json:"attachment"`
	Upload     *PresignedObjectURL `json:"upload,omitempty"`
}

type ConversationAttachmentDownload struct {
	Attachment domain.Attachment  `json:"attachment"`
	Download   PresignedObjectURL `json:"download"`
}

type GeneratedImageDownload struct {
	Asset    domain.GeneratedImageAsset `json:"asset"`
	Download PresignedObjectURL         `json:"download"`
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

type ConversationEventPage struct {
	Items         []domain.ConversationEvent
	NextBefore    string
	NextAfter     string
	HasMoreBefore bool
	HasMoreAfter  bool
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

type BillingToolPriceInput struct {
	ToolKey           string `json:"tool_key"`
	PricePerCallNanos int64  `json:"price_per_call_nanos"`
	Enabled           bool   `json:"enabled"`
	Version           int64  `json:"version"`
}

type UpdateBillingToolPricesInput struct {
	Prices    []BillingToolPriceInput `json:"tool_prices"`
	RequestID string                  `json:"-"`
}

type IssueRedemptionCodeInput struct {
	Amount    string
	Quantity  int
	ExpiresAt *time.Time
	RequestID string
}

type RedeemBillingCodeInput struct {
	Code      string
	RequestID string
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
	ListManagedUsers     func(ctx context.Context, actor *domain.User, limit int, cursor string) (*PageResult[domain.User], error)
	GetManagedUser       func(ctx context.Context, actor *domain.User, userID string) (*domain.User, error)
	CreateManagedUser    func(ctx context.Context, actor *domain.User, input assistantauth.CreateManagedUserInput) (*domain.User, error)
	UpdateManagedUser    func(ctx context.Context, actor *domain.User, input assistantauth.UpdateManagedUserInput) (*domain.User, error)
	ResetManagedPassword func(ctx context.Context, actor *domain.User, input assistantauth.ResetManagedPasswordInput) (*domain.User, error)
	DeleteManagedUser    func(ctx context.Context, actor *domain.User, userID string) error
}

type ProfileUseCases struct {
	GetPersonalization    func(ctx context.Context, userID string) (*domain.UserPreferences, error)
	UpdatePersonalization func(ctx context.Context, userID string, input profile.UpdatePreferencesInput) (*domain.UserPreferences, error)
	GetLocation           func(ctx context.Context, userID string) (*domain.UserLocation, error)
	UpdateLocation        func(ctx context.Context, userID string, input profile.UpdateLocationInput) (*domain.UserLocation, error)
	DeleteLocation        func(ctx context.Context, userID string) error
}

type MCPUseCases struct {
	ListServers  func(ctx context.Context, ownerUserID string) ([]domain.UserMCPServer, error)
	CreateServer func(ctx context.Context, ownerUserID string, input mcpconfig.CreateServerInput) (*domain.UserMCPServer, error)
	GetServer    func(ctx context.Context, ownerUserID string, serverID string) (*domain.UserMCPServer, error)
	UpdateServer func(ctx context.Context, ownerUserID string, serverID string, input mcpconfig.UpdateServerInput) (*domain.UserMCPServer, error)
	DeleteServer func(ctx context.Context, ownerUserID string, serverID string) error
	TestServer   func(ctx context.Context, ownerUserID string, serverID string) (*domain.UserMCPServer, error)
}

type ConversationUseCases struct {
	CreateConversation      func(ctx context.Context, ownerUserID string, title string, metadata json.RawMessage) (*domain.Conversation, error)
	CreateConversationShare func(ctx context.Context, ownerUserID string, conversationID string, idempotencyKey string) (*CreateConversationShareResult, error)
	GetConversationShare    func(ctx context.Context, shareID string) (*domain.ConversationShareSnapshot, error)
	InitialTurn             func(ctx context.Context, ownerUserID string, idempotencyKey string, input InitialTurnInput) (*InitialTurnResult, error)
	ListConversations       func(ctx context.Context, ownerUserID string, limit int) ([]domain.Conversation, error)
	GetConversation         func(ctx context.Context, ownerUserID string, conversationID string) (*domain.Conversation, error)
	UpdateConversation      func(ctx context.Context, ownerUserID string, input UpdateConversationInput) (*domain.Conversation, error)
	DeleteConversation      func(ctx context.Context, ownerUserID string, conversationID string) error
	SendMessage             func(ctx context.Context, ownerUserID string, conversationID string, input SendMessageInput) (*domain.EnqueuedTurn, error)
	RetryTurn               func(ctx context.Context, ownerUserID string, sourceTurnID string) (*domain.EnqueuedRetryTurn, error)
	EditTurn                func(ctx context.Context, ownerUserID string, sourceTurnID string, content string) (*domain.EnqueuedRetryTurn, error)
	ListMessages            func(ctx context.Context, ownerUserID string, conversationID string, limit int) ([]domain.Message, error)
	ListConversationEvents  func(ctx context.Context, ownerUserID string, conversationID string, limit int, beforeSeq int64, afterSeq int64) (*ConversationEventPage, error)
}

type AttachmentUseCases struct {
	CreateConversationAttachmentUpload   func(ctx context.Context, ownerUserID string, conversationID string, input CreateConversationAttachmentUploadInput) (*ConversationAttachmentUpload, error)
	CompleteConversationAttachmentUpload func(ctx context.Context, ownerUserID string, conversationID string, attachmentID string, input CompleteConversationAttachmentUploadInput) (*domain.Attachment, error)
	GetConversationAttachmentDownload    func(ctx context.Context, ownerUserID string, conversationID string, attachmentID string, download bool) (*ConversationAttachmentDownload, error)
	GetGeneratedImageDownload            func(ctx context.Context, ownerUserID string, conversationID string, assetID string) (*GeneratedImageDownload, error)
}

type StorageUseCases struct {
	GetStorageUsage        func(ctx context.Context, userID string) (*domain.StorageUsage, error)
	ListStorageAttachments func(ctx context.Context, userID string, limit int, cursor string) (*PageResult[domain.StorageAttachment], error)
	DeleteAttachment       func(ctx context.Context, userID string, attachmentID string) error
}

type SandboxUseCases struct {
	GetConversationSandbox     func(ctx context.Context, ownerUserID string, conversationID string) (*domain.ConversationSandbox, error)
	CreateConversationSandbox  func(ctx context.Context, ownerUserID string, conversationID string) (*domain.ConversationSandbox, error)
	DestroyConversationSandbox func(ctx context.Context, ownerUserID string, conversationID string) (*domain.ConversationSandbox, error)
	ExecConversationSandbox    func(ctx context.Context, ownerUserID string, input ExecConversationSandboxInput) (*domain.SandboxCommandResult, error)
}

type TurnUseCases struct {
	GetTurn                 func(ctx context.Context, ownerUserID string, turnID string) (*domain.Turn, error)
	RequestTurnCancellation func(ctx context.Context, ownerUserID string, turnID string) (*domain.Turn, error)
	AnswerToolCall          func(ctx context.Context, ownerUserID string, turnID string, toolCallID string, optionID string, idempotencyKey string) (*tool.AskUserInteraction, error)
	GetTurnExecutionTrace   func(ctx context.Context, ownerUserID string, turnID string) (*TurnExecutionTrace, error)
	GetTurnTimeline         func(ctx context.Context, ownerUserID string, turnID string) (*TurnTimeline, error)
}

type ModelUseCases struct {
	ListModels          func(ctx context.Context, limit int, cursor string) (*PageResult[domain.Model], error)
	GetModel            func(ctx context.Context, modelID string) (*domain.Model, error)
	ListAdminModels     func(ctx context.Context, actor *domain.User, limit int, cursor string) (*PageResult[domain.Model], error)
	GetAdminModel       func(ctx context.Context, actor *domain.User, modelID string) (*domain.Model, error)
	CreateModel         func(ctx context.Context, actor *domain.User, input CreateModelInput) (*domain.Model, error)
	UpdateModel         func(ctx context.Context, actor *domain.User, input UpdateModelInput) (*domain.Model, error)
	DeleteModel         func(ctx context.Context, actor *domain.User, modelID string) error
	ListModelPrices     func(ctx context.Context, actor *domain.User, modelID string, limit int, cursor string) (*PageResult[domain.ModelPriceVersion], error)
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
	IssueRedemptionCodes    func(ctx context.Context, actor *domain.User, input IssueRedemptionCodeInput) ([]domain.BillingRedemptionCodeIssue, error)
	ListRedemptionCodes     func(ctx context.Context, actor *domain.User, limit int, cursor string) (*PageResult[domain.BillingRedemptionCode], error)
	DisableRedemptionCode   func(ctx context.Context, actor *domain.User, codeID string, requestID string) (*domain.BillingRedemptionCode, error)
	RedeemBillingCode       func(ctx context.Context, actor *domain.User, input RedeemBillingCodeInput) (*domain.BillingRedemptionResult, error)
	ListBillingTransactions func(ctx context.Context, input BillingListInput) (*PageResult[domain.BillingTransaction], error)
	GetBillingTransaction   func(ctx context.Context, transactionID string, userID string) (*domain.BillingTransaction, error)
	ListBillingUsageEvents  func(ctx context.Context, input BillingListInput) (*PageResult[domain.BillingUsageEvent], error)
	GetBillingUsageEvent    func(ctx context.Context, usageEventID string, userID string) (*domain.BillingUsageEvent, error)
	ListBillingToolPrices   func(ctx context.Context, actor *domain.User) ([]domain.BillingToolPrice, error)
	UpdateBillingToolPrices func(ctx context.Context, actor *domain.User, input UpdateBillingToolPricesInput) ([]domain.BillingToolPrice, error)
}

type AuditUseCases struct {
	ListAuditEvents func(ctx context.Context, input AuditListInput) (*PageResult[domain.AuditEvent], error)
	GetAuditEvent   func(ctx context.Context, eventID string, viewerUserID string, viewerRole string) (*domain.AuditEvent, error)
	RecordAudit     func(ctx context.Context, input RecordAuditInput) error
}

type AdminOverviewResult struct {
	Users          int64               `json:"users"`
	EnabledModels  *int64              `json:"enabled_models,omitempty"`
	Credentials    *int64              `json:"credentials,omitempty"`
	ActiveAccounts int64               `json:"active_accounts"`
	AuditEvents    int64               `json:"audit_events"`
	Audit          []domain.AuditEvent `json:"audit"`
}

type AdminOverviewUseCases struct {
	GetAdminOverview func(ctx context.Context, actor *domain.User) (*AdminOverviewResult, error)
}

type MailUseCases struct {
	GetMailSettings    func(ctx context.Context, actor *domain.User) (*assistantmail.Settings, error)
	UpdateMailSettings func(ctx context.Context, actor *domain.User, input assistantmail.UpdateSettingsInput) (*assistantmail.Settings, error)
	TestMailSettings   func(ctx context.Context, actor *domain.User, recipient string) error
}

type UseCases struct {
	Auth          AuthUseCases
	Users         UserUseCases
	Profile       ProfileUseCases
	MCP           MCPUseCases
	Conversations ConversationUseCases
	Attachments   AttachmentUseCases
	Storage       StorageUseCases
	Sandboxes     SandboxUseCases
	Turns         TurnUseCases
	Models        ModelUseCases
	Credentials   CredentialUseCases
	Billing       BillingUseCases
	Audit         AuditUseCases
	Overview      AdminOverviewUseCases
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
