package bootstrap

import (
	"context"
	"errors"
	"strconv"
	"time"

	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	"github.com/EurekaMXZ/assistant/internal/credential"
	"github.com/EurekaMXZ/assistant/internal/domain"
	assistantmail "github.com/EurekaMXZ/assistant/internal/mail"
	"github.com/EurekaMXZ/assistant/internal/mcpconfig"
	"github.com/EurekaMXZ/assistant/internal/postgres"
	"github.com/EurekaMXZ/assistant/internal/profile"
	assistantsandbox "github.com/EurekaMXZ/assistant/internal/sandbox"
	"github.com/EurekaMXZ/assistant/internal/server"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/EurekaMXZ/assistant/internal/tool"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/jackc/pgx/v5/pgxpool"
)

type workflowAdapters struct {
	Outbox               workflow.WorkflowOutboxRepository
	Turns                workflow.TurnWorkflowRepository
	Contexts             workflow.WorkflowContextRepository
	CompleteEvents       workflow.CompleteEventStore
	Attachments          workflow.AttachmentStore
	AttachmentCleanup    assistantattachment.CleanupRepository
	GeneratedAttachments workflow.GeneratedAttachmentStore
	TurnRuns             workflow.TurnRunWorkflowStore
	ToolCalls            workflow.ToolCallStore
	StreamEvents         workflow.TurnStreamEventStore
	StaleTurns           workflow.StaleTurnRepository
	Locker               workflow.ConversationLocker
	ConversationReader   workflow.ConversationReader
	Conversations        tool.ConversationTitleUpdater
	Sandboxes            tool.ConversationSandboxStore
	Models               *postgres.ModelRepository
	ProviderCredentials  *postgres.ProviderCredentialRepository
	CredentialCipher     *credential.Cipher
	BillingUsage         *postgres.BillingAccountRepository
	MCP                  mcpconfig.RuntimeRepository
	Profiles             workflow.PersonalizationReader
}

func buildApplication(pool *pgxpool.Pool, toolArtifacts workflow.ToolArtifactStore, attachmentSigner assistantattachment.URLSigner, publisher stream.Publisher, billingCurrency string, authService *assistantauth.Service, sandboxRuntime tool.SandboxManager, sandboxLifecycle assistantsandbox.LifecycleSettings, credentialCipher *credential.Cipher, publicURL string) (server.UseCases, workflowAdapters) {
	conversationRepository := postgres.NewConversationRepository(pool)
	conversationShareRepository := postgres.NewConversationShareRepository(pool)
	conversationSandboxRepository := postgres.NewConversationSandboxRepository(pool)
	conversationLocker := postgres.NewConversationLocker(pool)
	attachmentRepository := postgres.NewAttachmentRepository(pool)
	messageRepository := postgres.NewMessageRepository(pool)
	turnRepository := postgres.NewTurnRepository(pool)
	initialTurnRepository := postgres.NewInitialTurnRepository(pool)
	workflowTurnRepository := postgres.NewWorkflowTurnRepository(pool)
	turnRunRepository := postgres.NewTurnRunRepository(pool)
	toolCallRepository := postgres.NewToolCallRepository(pool)
	turnStreamEventRepository := postgres.NewTurnStreamEventRepository(pool)
	conversationEventRepository := postgres.NewConversationEventRepository(pool)
	userRepository := postgres.NewUserRepository(pool)
	modelRepository := postgres.NewModelRepository(pool)
	providerCredentialRepository := postgres.NewProviderCredentialRepository(pool)
	billingAccountRepository := postgres.NewBillingAccountRepository(pool)
	auditRepository := postgres.NewAuditRepository(pool)
	actionTokenRepository := postgres.NewActionTokenRepository(pool)
	smtpSettingsRepository := postgres.NewSMTPSettingsRepository(pool)
	profileRepository := postgres.NewProfileRepository(pool)
	profileService := &profile.Service{Repository: profileRepository}
	mcpRepository := postgres.NewMCPServerRepository(pool)
	mcpService := &mcpconfig.Service{
		Repository: mcpRepository,
		Cipher:     credentialCipher,
		ToolLister: &mcpconfig.SDKToolLister{},
	}
	mailService := &assistantmail.Service{
		Settings:  smtpSettingsRepository,
		Cipher:    assistantmail.NewPasswordCipher(credentialCipher),
		Sender:    assistantmail.SMTPSender{Timeout: 10 * time.Second},
		PublicURL: publicURL,
	}

	if authService == nil {
		authService = &assistantauth.Service{Users: userRepository}
	}
	authService.ActionTokens = actionTokenRepository
	authService.Mailer = mailService

	getTurnExecutionTrace := server.GetTurnExecutionTrace{
		Turns:     turnRepository,
		Runs:      turnRunRepository,
		ToolCalls: toolCallRepository,
		Artifacts: toolArtifacts,
	}
	getTurnTimeline := server.GetTurnTimeline{
		Turns:          turnRepository,
		Events:         turnStreamEventRepository,
		CompleteEvents: conversationEventRepository,
		Runs:           turnRunRepository,
		ToolCalls:      toolCallRepository,
		Messages:       messageRepository,
		Artifacts:      toolArtifacts,
	}
	createSandbox := tool.CreateSandbox{
		Sandboxes: conversationSandboxRepository,
		Runtime:   sandboxRuntime,
		Locker:    conversationLocker,
	}
	destroySandbox := tool.DestroySandbox{
		Sandboxes: conversationSandboxRepository,
		Runtime:   sandboxRuntime,
		Locker:    conversationLocker,
	}
	execSandbox := tool.ExecSandboxCommand{
		Sandboxes:      conversationSandboxRepository,
		Runtime:        sandboxRuntime,
		Locker:         conversationLocker,
		DefaultTimeout: sandboxLifecycle.CommandDefault,
		MaximumTimeout: sandboxLifecycle.CommandMaximum,
	}
	attachmentService := assistantattachment.Service{
		Repo:   attachmentRepository,
		Signer: attachmentSigner,
	}
	if objectDeleter, ok := attachmentSigner.(assistantattachment.ObjectDeleter); ok {
		attachmentService.Objects = objectDeleter
	}

	ensureOwnedConversation := func(ctx context.Context, ownerUserID string, conversationID string) (*domain.Conversation, error) {
		return conversationRepository.GetConversationByOwner(ctx, conversationID, ownerUserID)
	}
	ensureOwnedTurn := func(ctx context.Context, ownerUserID string, turnID string) (*domain.Turn, error) {
		turn, err := turnRepository.GetTurn(ctx, turnID)
		if err != nil {
			return nil, err
		}
		if _, err := ensureOwnedConversation(ctx, ownerUserID, turn.ConversationID); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, domain.ErrNotFound
			}
			return nil, err
		}
		return turn, nil
	}
	messageService := &MessageService{
		Conversations: conversationRepository,
		Attachments:   attachmentRepository,
		Models:        modelRepository,
		Billing:       billingAccountRepository,
		Turns:         turnRepository,
		RetryTurns:    turnRepository,
		Messages:      messageRepository,
	}
	initialTurnService := &InitialTurnService{Messages: messageService, Store: initialTurnRepository}
	answerService := workflow.AskUserAnswerService{
		Calls: toolCallRepository, Turns: workflowTurnRepository, Artifacts: toolArtifacts,
		Publisher: workflow.NewArchivingStreamPublisher(publisher, nil, turnStreamEventRepository, conversationEventRepository),
	}

	useCases := server.UseCases{
		Auth: server.AuthUseCases{
			AuthenticateAccessToken: authService.AuthenticateAccessToken,
			Register:                authService.Register,
			Login:                   authService.Login,
			VerifyEmail:             authService.VerifyEmail,
			ResendVerification:      authService.ResendVerification,
			ForgotPassword:          authService.ForgotPassword,
			ResetPassword:           authService.ResetPassword,
			ChangeOwnPassword:       authService.ChangeOwnPassword,
		},
		Users: server.UserUseCases{
			ListManagedUsers: func(ctx context.Context, actor *domain.User, limit int, cursor string) (*server.PageResult[domain.User], error) {
				items, next, err := authService.ListManagedUsers(ctx, actor, limit, cursor)
				if err != nil {
					return nil, err
				}
				return &server.PageResult[domain.User]{Items: items, NextCursor: next}, nil
			},
			GetManagedUser:       authService.GetManagedUser,
			CreateManagedUser:    authService.CreateManagedUser,
			UpdateManagedUser:    authService.UpdateManagedUser,
			ResetManagedPassword: authService.ResetManagedPassword,
			DeleteManagedUser:    authService.DeleteManagedUser,
		},
		Profile: server.ProfileUseCases{
			GetPersonalization:    profileService.GetPreferences,
			UpdatePersonalization: profileService.UpdatePreferences,
			GetLocation:           profileService.GetLocation,
			UpdateLocation:        profileService.UpdateLocation,
			DeleteLocation:        profileService.DeleteLocation,
		},
		MCP: server.MCPUseCases{
			ListServers:  mcpService.List,
			CreateServer: mcpService.Create,
			GetServer:    mcpService.Get,
			UpdateServer: mcpService.Update,
			DeleteServer: mcpService.Delete,
			TestServer:   mcpService.Test,
		},
		Conversations: server.ConversationUseCases{
			CreateConversation: conversationRepository.CreateConversation,
			CreateConversationShare: func(ctx context.Context, ownerUserID string, conversationID string, idempotencyKey string) (*server.CreateConversationShareResult, error) {
				share, replayed, err := conversationShareRepository.CreateConversationShare(ctx, postgres.CreateConversationShareParams{
					ConversationID: conversationID, CreatedByUserID: ownerUserID, IdempotencyKey: idempotencyKey,
				})
				if err != nil {
					return nil, err
				}
				return &server.CreateConversationShareResult{Share: *share, Replayed: replayed}, nil
			},
			GetConversationShare: conversationShareRepository.GetConversationShare,
			InitialTurn:          initialTurnService.Execute,
			ListConversations:    conversationRepository.ListConversationsByOwner,
			GetConversation:      ensureOwnedConversation,
			UpdateConversation: func(ctx context.Context, ownerUserID string, input server.UpdateConversationInput) (*domain.Conversation, error) {
				if _, err := ensureOwnedConversation(ctx, ownerUserID, input.ConversationID); err != nil {
					return nil, err
				}
				return conversationRepository.UpdateConversation(ctx, postgres.UpdateConversationParams{
					ConversationID: input.ConversationID,
					Title:          input.Title,
					Archived:       input.Archived,
				})
			},
			DeleteConversation: conversationRepository.DeleteConversation,
			SendMessage:        messageService.SendMessage,
			RetryTurn:          messageService.RetryTurn,
			EditTurn:           messageService.EditTurn,
			ListMessages: func(ctx context.Context, ownerUserID string, conversationID string, limit int) ([]domain.Message, error) {
				if _, err := ensureOwnedConversation(ctx, ownerUserID, conversationID); err != nil {
					return nil, err
				}
				return messageRepository.ListMessages(ctx, conversationID, limit)
			},
			ListConversationEvents: func(ctx context.Context, ownerUserID string, conversationID string, limit int, beforeSeq int64, afterSeq int64) (*server.ConversationEventPage, error) {
				if _, err := ensureOwnedConversation(ctx, ownerUserID, conversationID); err != nil {
					return nil, err
				}
				if limit <= 0 {
					limit = 100
				} else if limit > 1000 {
					limit = 1000
				}
				events, err := conversationEventRepository.ListConversationEvents(ctx, conversationID, limit+1, beforeSeq, afterSeq)
				if err != nil {
					return nil, err
				}
				page := &server.ConversationEventPage{Items: events}
				if beforeSeq > 0 || afterSeq == 0 {
					if len(page.Items) > limit {
						page.HasMoreBefore = true
						page.Items = page.Items[len(page.Items)-limit:]
					}
					if len(page.Items) > 0 && page.HasMoreBefore {
						page.NextBefore = strconv.FormatInt(page.Items[0].EventSeq, 10)
					}
				} else if len(page.Items) > limit {
					page.HasMoreAfter = true
					page.Items = page.Items[:limit]
				}
				if len(page.Items) > 0 && afterSeq > 0 && page.HasMoreAfter {
					page.NextAfter = strconv.FormatInt(page.Items[len(page.Items)-1].EventSeq, 10)
				}
				return page, nil
			},
		},
		Storage: server.StorageUseCases{
			GetStorageUsage: attachmentService.GetStorageUsage,
			ListStorageAttachments: func(ctx context.Context, userID string, limit int, cursor string) (*server.PageResult[domain.StorageAttachment], error) {
				items, next, err := attachmentService.ListStorageAttachments(ctx, userID, limit, cursor)
				if err != nil {
					return nil, err
				}
				return &server.PageResult[domain.StorageAttachment]{Items: items, NextCursor: next}, nil
			},
			DeleteAttachment: attachmentService.DeleteStorageAttachment,
		},
		Attachments: server.AttachmentUseCases{
			CreateConversationAttachmentUpload: func(ctx context.Context, ownerUserID string, conversationID string, input server.CreateConversationAttachmentUploadInput) (*server.ConversationAttachmentUpload, error) {
				if _, err := ensureOwnedConversation(ctx, ownerUserID, conversationID); err != nil {
					return nil, err
				}
				intent, err := attachmentService.CreateUpload(ctx, assistantattachment.CreateUploadInput{
					ConversationID:   conversationID,
					UploadedByUserID: ownerUserID,
					IdempotencyKey:   input.IdempotencyKey,
					Filename:         input.Filename,
					ContentType:      input.ContentType,
					SizeBytes:        input.SizeBytes,
					SHA256:           input.SHA256,
					ContentMD5:       input.ContentMD5,
				})
				if err != nil {
					return nil, err
				}
				result := &server.ConversationAttachmentUpload{Attachment: *intent.Attachment}
				if intent.Upload != nil {
					result.Upload = &server.PresignedObjectURL{URL: intent.Upload.URL, Method: intent.Upload.Method, Headers: intent.Upload.Headers, ExpiresAt: intent.Upload.ExpiresAt}
				}
				return result, nil
			},
			CompleteConversationAttachmentUpload: func(ctx context.Context, ownerUserID string, conversationID string, attachmentID string, input server.CompleteConversationAttachmentUploadInput) (*domain.Attachment, error) {
				if _, err := ensureOwnedConversation(ctx, ownerUserID, conversationID); err != nil {
					return nil, err
				}
				return attachmentService.CompleteUpload(ctx, assistantattachment.CompleteUploadInput{
					ConversationID: conversationID, UploadedByUserID: ownerUserID, AttachmentID: attachmentID,
				})
			},
			GetConversationAttachmentDownload: func(ctx context.Context, ownerUserID string, conversationID string, attachmentID string, download bool) (*server.ConversationAttachmentDownload, error) {
				if _, err := ensureOwnedConversation(ctx, ownerUserID, conversationID); err != nil {
					return nil, err
				}
				attachment, presigned, err := attachmentService.DownloadURL(ctx, conversationID, ownerUserID, attachmentID, download)
				if err != nil {
					return nil, err
				}
				return &server.ConversationAttachmentDownload{Attachment: *attachment, Download: server.PresignedObjectURL{URL: presigned.URL, Method: presigned.Method, Headers: presigned.Headers, ExpiresAt: presigned.ExpiresAt}}, nil
			},
		},
		Sandboxes: server.SandboxUseCases{
			GetConversationSandbox: func(ctx context.Context, ownerUserID string, conversationID string) (*domain.ConversationSandbox, error) {
				if _, err := ensureOwnedConversation(ctx, ownerUserID, conversationID); err != nil {
					return nil, err
				}
				return conversationSandboxRepository.GetUsableConversationSandbox(ctx, conversationID)
			},
			CreateConversationSandbox: func(ctx context.Context, ownerUserID string, conversationID string) (*domain.ConversationSandbox, error) {
				if _, err := ensureOwnedConversation(ctx, ownerUserID, conversationID); err != nil {
					return nil, err
				}
				return createSandbox.Execute(ctx, tool.CreateSandboxInput{ConversationID: conversationID})
			},
			DestroyConversationSandbox: func(ctx context.Context, ownerUserID string, conversationID string) (*domain.ConversationSandbox, error) {
				if _, err := ensureOwnedConversation(ctx, ownerUserID, conversationID); err != nil {
					return nil, err
				}
				return destroySandbox.Execute(ctx, tool.DestroySandboxInput{ConversationID: conversationID})
			},
			ExecConversationSandbox: func(ctx context.Context, ownerUserID string, input server.ExecConversationSandboxInput) (*domain.SandboxCommandResult, error) {
				if _, err := ensureOwnedConversation(ctx, ownerUserID, input.ConversationID); err != nil {
					return nil, err
				}
				return execSandbox.Execute(ctx, tool.ExecSandboxCommandInput{
					ConversationID:   input.ConversationID,
					Command:          input.Command,
					Args:             input.Args,
					WorkingDirectory: input.WorkingDirectory,
					TimeoutSeconds:   input.TimeoutSeconds,
				})
			},
		},
		Turns: server.TurnUseCases{
			GetTurn: func(ctx context.Context, ownerUserID string, turnID string) (*domain.Turn, error) {
				return ensureOwnedTurn(ctx, ownerUserID, turnID)
			},
			RequestTurnCancellation: func(ctx context.Context, ownerUserID string, turnID string) (*domain.Turn, error) {
				if _, err := ensureOwnedTurn(ctx, ownerUserID, turnID); err != nil {
					return nil, err
				}
				return turnRepository.RequestTurnCancellation(ctx, turnID)
			},
			AnswerToolCall: func(ctx context.Context, ownerUserID string, turnID string, toolCallID string, optionID string, idempotencyKey string) (*tool.AskUserInteraction, error) {
				return answerService.Answer(ctx, workflow.AskUserAnswerInput{
					OwnerUserID: ownerUserID, TurnID: turnID, ToolCallID: toolCallID,
					OptionID: optionID, IdempotencyKey: idempotencyKey,
				})
			},
			GetTurnExecutionTrace: func(ctx context.Context, ownerUserID string, turnID string) (*server.TurnExecutionTrace, error) {
				if _, err := ensureOwnedTurn(ctx, ownerUserID, turnID); err != nil {
					return nil, err
				}
				return getTurnExecutionTrace.Execute(ctx, turnID)
			},
			GetTurnTimeline: func(ctx context.Context, ownerUserID string, turnID string) (*server.TurnTimeline, error) {
				if _, err := ensureOwnedTurn(ctx, ownerUserID, turnID); err != nil {
					return nil, err
				}
				return getTurnTimeline.Execute(ctx, turnID)
			},
		},
		Mail: server.MailUseCases{
			GetMailSettings:    mailService.GetSettings,
			UpdateMailSettings: mailService.UpdateSettings,
			TestMailSettings:   mailService.TestSettings,
		},
	}
	attachManagementUseCases(&useCases, managementDependencies{
		models: modelRepository, credentials: providerCredentialRepository, billing: billingAccountRepository,
		audit: auditRepository, overview: postgres.NewAdminOverviewRepository(pool), cipher: credentialCipher, currency: billingCurrency,
	})
	adapters := workflowAdapters{
		Outbox:               postgres.NewWorkflowOutboxRepository(pool),
		Turns:                workflowTurnRepository,
		Contexts:             postgres.NewWorkflowContextRepository(pool),
		CompleteEvents:       conversationEventRepository,
		Attachments:          attachmentRepository,
		AttachmentCleanup:    attachmentRepository,
		GeneratedAttachments: attachmentRepository,
		TurnRuns:             turnRunRepository,
		ToolCalls:            toolCallRepository,
		StreamEvents:         turnStreamEventRepository,
		StaleTurns:           postgres.NewStaleTurnRepository(pool),
		Locker:               conversationLocker,
		ConversationReader:   conversationRepository,
		Conversations:        conversationRepository,
		Sandboxes:            conversationSandboxRepository,
		Models:               modelRepository,
		ProviderCredentials:  providerCredentialRepository,
		CredentialCipher:     credentialCipher,
		BillingUsage:         billingAccountRepository,
		MCP:                  mcpRepository,
		Profiles:             profileRepository,
	}
	return useCases, adapters
}
