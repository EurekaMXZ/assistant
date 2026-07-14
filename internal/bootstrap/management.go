package bootstrap

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/billing"
	"github.com/EurekaMXZ/assistant/internal/credential"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/postgres"
	"github.com/EurekaMXZ/assistant/internal/server"
	"github.com/google/uuid"
)

type managementDependencies struct {
	models      *postgres.ModelRepository
	credentials *postgres.ProviderCredentialRepository
	billing     *postgres.BillingAccountRepository
	audit       *postgres.AuditRepository
	cipher      *credential.Cipher
	currency    string
}

func attachManagementUseCases(useCases *server.UseCases, deps managementDependencies) {
	validator := credential.NewValidator(15 * time.Second)
	useCases.Models.ListModels = func(ctx context.Context, limit int, cursor string) (*server.PageResult[domain.Model], error) {
		items, next, err := deps.models.List(ctx, true, limit, cursor)
		if err != nil {
			return nil, err
		}
		settings, err := deps.models.GetSettings(ctx)
		if err != nil {
			return nil, err
		}
		for index := range items {
			items[index].CredentialID = ""
			items[index].IsDefault = items[index].ID == settings.DefaultChatModelID
		}
		return &server.PageResult[domain.Model]{Items: items, NextCursor: next}, nil
	}
	useCases.Models.GetModel = func(ctx context.Context, modelID string) (*domain.Model, error) {
		item, err := deps.models.Get(ctx, modelID)
		if err != nil {
			return nil, err
		}
		if item.Status != domain.ModelStatusEnabled {
			return nil, domain.ErrNotFound
		}
		settings, err := deps.models.GetSettings(ctx)
		if err != nil {
			return nil, err
		}
		item.CredentialID = ""
		item.IsDefault = item.ID == settings.DefaultChatModelID
		return item, nil
	}
	useCases.Models.ListAdminModels = func(ctx context.Context, actor *domain.User, limit int, cursor string) (*server.PageResult[domain.Model], error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		items, next, err := deps.models.List(ctx, false, limit, cursor)
		return &server.PageResult[domain.Model]{Items: items, NextCursor: next}, err
	}
	useCases.Models.GetAdminModel = func(ctx context.Context, actor *domain.User, modelID string) (*domain.Model, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		return deps.models.Get(ctx, modelID)
	}
	useCases.Models.CreateModel = func(ctx context.Context, actor *domain.User, input server.CreateModelInput) (*domain.Model, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		if strings.TrimSpace(input.Provider) != domain.ProviderOpenAI || strings.TrimSpace(input.CredentialID) == "" ||
			strings.TrimSpace(input.Slug) == "" || strings.TrimSpace(input.UpstreamModel) == "" || strings.TrimSpace(input.DisplayName) == "" ||
			input.ContextWindowTokens <= 0 || input.MaxOutputTokens <= 0 || input.MaxOutputTokens > input.ContextWindowTokens {
			return nil, domain.NewValidationError("valid provider, credential, model names, and token limits are required")
		}
		efforts, err := normalizeReasoningEfforts(input.SupportedReasoningEfforts)
		if err != nil {
			return nil, err
		}
		if err := validateModelDefaultParameters(input.DefaultParameters, efforts); err != nil {
			return nil, err
		}
		return deps.models.Create(ctx, postgres.CreateModelParams{
			Provider: input.Provider, CredentialID: input.CredentialID, Slug: input.Slug,
			UpstreamModel: input.UpstreamModel, DisplayName: input.DisplayName, Description: input.Description,
			InputModalities: input.InputModalities, OutputModalities: input.OutputModalities,
			SupportsTools: input.SupportsTools, SupportsParallelTools: input.SupportsParallelTools,
			SupportedReasoningEfforts: efforts, ContextWindowTokens: input.ContextWindowTokens,
			MaxOutputTokens: input.MaxOutputTokens, DefaultParameters: input.DefaultParameters, ActorUserID: actor.ID,
		})
	}
	useCases.Models.UpdateModel = func(ctx context.Context, actor *domain.User, input server.UpdateModelInput) (*domain.Model, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		if input.ContextWindowTokens != nil && *input.ContextWindowTokens <= 0 {
			return nil, domain.NewValidationError("context_window_tokens must be greater than zero")
		}
		if input.MaxOutputTokens != nil && *input.MaxOutputTokens <= 0 {
			return nil, domain.NewValidationError("max_output_tokens must be greater than zero")
		}
		efforts := input.SupportedReasoningEfforts
		if efforts != nil {
			normalized, err := normalizeReasoningEfforts(efforts)
			if err != nil {
				return nil, err
			}
			efforts = normalized
		}
		current, err := deps.models.Get(ctx, input.ID)
		if err != nil {
			return nil, err
		}
		contextWindowTokens := current.ContextWindowTokens
		maxOutputTokens := current.MaxOutputTokens
		if input.ContextWindowTokens != nil {
			contextWindowTokens = *input.ContextWindowTokens
		}
		if input.MaxOutputTokens != nil {
			maxOutputTokens = *input.MaxOutputTokens
		}
		if maxOutputTokens > contextWindowTokens {
			return nil, domain.NewValidationError("max_output_tokens cannot exceed context_window_tokens")
		}
		effectiveEfforts := current.SupportedReasoningEfforts
		if efforts != nil {
			effectiveEfforts = efforts
		}
		effectiveParameters := current.DefaultParameters
		if len(input.DefaultParameters) > 0 {
			effectiveParameters = input.DefaultParameters
		}
		if err := validateModelDefaultParameters(effectiveParameters, effectiveEfforts); err != nil {
			return nil, err
		}
		return deps.models.Update(ctx, postgres.UpdateModelParams{
			ID: input.ID, CredentialID: input.CredentialID, DisplayName: input.DisplayName,
			Description: input.Description, InputModalities: input.InputModalities, OutputModalities: input.OutputModalities,
			SupportsTools: input.SupportsTools, SupportsParallelTools: input.SupportsParallelTools,
			SupportedReasoningEfforts: efforts, ContextWindowTokens: input.ContextWindowTokens,
			MaxOutputTokens: input.MaxOutputTokens, DefaultParameters: input.DefaultParameters,
			Status: input.Status, ActorUserID: actor.ID,
		})
	}
	useCases.Models.ListModelPrices = func(ctx context.Context, actor *domain.User, modelID string) ([]domain.ModelPriceVersion, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		return deps.models.ListPrices(ctx, modelID)
	}
	useCases.Models.GetModelPrice = func(ctx context.Context, actor *domain.User, modelID string, priceID string) (*domain.ModelPriceVersion, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		return deps.models.GetPrice(ctx, modelID, priceID)
	}
	useCases.Models.CreateModelPrice = func(ctx context.Context, actor *domain.User, input server.CreateModelPriceInput) (*domain.ModelPriceVersion, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		currency := strings.ToUpper(strings.TrimSpace(input.Currency))
		if currency != deps.currency || input.InputPerMillionNanos < 0 || input.CacheReadInputPerMillionNanos < 0 ||
			input.CacheCreationInputPerMillionNanos < 0 || input.OutputPerMillionNanos < 0 ||
			(input.ImageInputPerMillionNanos != nil && *input.ImageInputPerMillionNanos < 0) ||
			(input.ImageOutputPerImageNanos != nil && *input.ImageOutputPerImageNanos < 0) {
			return nil, domain.NewValidationError("currency and non-negative price rates are required")
		}
		return deps.models.CreatePrice(ctx, postgres.CreateModelPriceParams{
			ModelID: input.ModelID, Currency: currency,
			InputPerMillionNanos: input.InputPerMillionNanos, CacheReadInputPerMillionNanos: input.CacheReadInputPerMillionNanos,
			CacheCreationInputPerMillionNanos: input.CacheCreationInputPerMillionNanos, OutputPerMillionNanos: input.OutputPerMillionNanos,
			ImageInputPerMillionNanos: input.ImageInputPerMillionNanos, ImageOutputPerImageNanos: input.ImageOutputPerImageNanos,
			ActorUserID: actor.ID,
		})
	}
	useCases.Models.PublishModelPrice = func(ctx context.Context, actor *domain.User, modelID string, priceID string, effectiveFrom *time.Time) (*domain.ModelPriceVersion, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		return deps.models.SetPriceStatus(ctx, modelID, priceID, domain.ModelPriceStatusPublished, actor.ID, effectiveFrom)
	}
	useCases.Models.ArchiveModelPrice = func(ctx context.Context, actor *domain.User, modelID string, priceID string) (*domain.ModelPriceVersion, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		return deps.models.SetPriceStatus(ctx, modelID, priceID, domain.ModelPriceStatusArchived, actor.ID, nil)
	}
	useCases.Models.GetModelSettings = func(ctx context.Context, actor *domain.User) (*domain.ModelSettings, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		return deps.models.GetSettings(ctx)
	}
	useCases.Models.UpdateModelSettings = func(ctx context.Context, actor *domain.User, input server.UpdateModelSettingsInput) (*domain.ModelSettings, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		return deps.models.UpdateSettings(ctx, input.DefaultChatModelID, input.CompactionModelID, actor.ID)
	}

	useCases.Credentials.ListProviderCredentials = func(ctx context.Context, actor *domain.User, limit int, cursor string) (*server.PageResult[domain.ProviderCredential], error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		items, next, err := deps.credentials.List(ctx, limit, cursor)
		return &server.PageResult[domain.ProviderCredential]{Items: items, NextCursor: next}, err
	}
	useCases.Credentials.GetProviderCredential = func(ctx context.Context, actor *domain.User, credentialID string) (*domain.ProviderCredential, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		return deps.credentials.Get(ctx, credentialID)
	}
	useCases.Credentials.CreateProviderCredential = func(ctx context.Context, actor *domain.User, input server.CreateProviderCredentialInput) (*domain.ProviderCredential, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		provider := strings.ToLower(strings.TrimSpace(input.Provider))
		name := strings.TrimSpace(input.Name)
		baseURL, err := normalizeProviderBaseURL(input.BaseURL)
		if provider != domain.ProviderOpenAI || name == "" || strings.TrimSpace(input.APIKey) == "" || err != nil {
			return nil, domain.NewValidationError("provider, name, base_url, and api_key are required")
		}
		id := uuid.NewString()
		encrypted, nonce, err := deps.cipher.Encrypt(id, provider, input.APIKey)
		if err != nil {
			return nil, err
		}
		return deps.credentials.Create(ctx, postgres.CreateProviderCredentialParams{
			ID: id, Provider: provider, Name: name, BaseURL: baseURL,
			EncryptedAPIKey: encrypted, Nonce: nonce, KeyVersion: 1,
			KeyHint: credential.KeyHint(input.APIKey), ActorUserID: actor.ID,
		})
	}
	useCases.Credentials.UpdateProviderCredential = func(ctx context.Context, actor *domain.User, input server.UpdateProviderCredentialInput) (*domain.ProviderCredential, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		if input.Name != nil {
			name := strings.TrimSpace(*input.Name)
			if name == "" {
				return nil, domain.NewValidationError("name must not be empty")
			}
			input.Name = &name
		}
		if input.BaseURL != nil {
			baseURL, err := normalizeProviderBaseURL(*input.BaseURL)
			if err != nil {
				return nil, domain.NewValidationError("base_url is invalid")
			}
			input.BaseURL = &baseURL
		}
		if input.Status != nil {
			status := strings.ToLower(strings.TrimSpace(*input.Status))
			if status != domain.CredentialStatusEnabled && status != domain.CredentialStatusDisabled {
				return nil, domain.NewValidationError("status is invalid")
			}
			input.Status = &status
		}
		return deps.credentials.Update(ctx, postgres.UpdateProviderCredentialParams{ID: input.ID, Name: input.Name, BaseURL: input.BaseURL, Status: input.Status, ActorUserID: actor.ID})
	}
	useCases.Credentials.RotateProviderCredential = func(ctx context.Context, actor *domain.User, credentialID string, apiKey string) (*domain.ProviderCredential, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		stored, err := deps.credentials.GetStored(ctx, credentialID)
		if err != nil {
			return nil, err
		}
		encrypted, nonce, err := deps.cipher.Encrypt(stored.ID, stored.Provider, apiKey)
		if err != nil {
			return nil, err
		}
		return deps.credentials.Rotate(ctx, stored.ID, actor.ID, encrypted, nonce, stored.KeyVersion+1, credential.KeyHint(apiKey))
	}
	useCases.Credentials.ValidateProviderCredential = func(ctx context.Context, actor *domain.User, credentialID string) (*domain.ProviderCredential, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		stored, err := deps.credentials.GetStored(ctx, credentialID)
		if err != nil {
			return nil, err
		}
		apiKey, err := deps.cipher.Decrypt(stored.ID, stored.Provider, stored.EncryptedAPIKey, stored.Nonce)
		if err != nil {
			return nil, err
		}
		validationErr := validator.Validate(ctx, stored.BaseURL, apiKey)
		message := ""
		if validationErr != nil {
			message = validationErr.Error()
		}
		item, recordErr := deps.credentials.RecordValidation(ctx, stored.ID, message)
		if recordErr != nil {
			return nil, recordErr
		}
		if validationErr != nil {
			return nil, domain.NewValidationError(message)
		}
		return item, nil
	}
	useCases.Credentials.RevokeProviderCredential = func(ctx context.Context, actor *domain.User, credentialID string) (*domain.ProviderCredential, error) {
		if err := requireSystemActor(actor); err != nil {
			return nil, err
		}
		return deps.credentials.Revoke(ctx, credentialID, actor.ID)
	}

	useCases.Billing.GetBillingAccount = func(ctx context.Context, userID string) (*domain.BillingAccount, error) {
		return deps.billing.GetOrCreateAccount(ctx, userID, deps.currency)
	}
	useCases.Billing.ListBillingAccounts = func(ctx context.Context, actor *domain.User, limit int, cursor string) (*server.PageResult[domain.BillingAccount], error) {
		if err := requireAdminActor(actor); err != nil {
			return nil, err
		}
		items, next, err := deps.billing.ListAccounts(ctx, limit, cursor)
		return &server.PageResult[domain.BillingAccount]{Items: items, NextCursor: next}, err
	}
	useCases.Billing.UpdateBillingAccount = func(ctx context.Context, actor *domain.User, input server.UpdateBillingAccountInput) (*domain.BillingAccount, error) {
		if err := requireAdminActor(actor); err != nil {
			return nil, err
		}
		if input.Status != nil && *input.Status != "active" && *input.Status != "frozen" {
			return nil, domain.NewValidationError("billing account status must be active or frozen")
		}
		if _, err := deps.billing.GetOrCreateAccount(ctx, input.UserID, deps.currency); err != nil {
			return nil, err
		}
		return deps.billing.UpdateAccount(ctx, input.UserID, deps.currency, input.Status)
	}
	manual := func(ctx context.Context, actor *domain.User, input server.ManualBillingInput, kind string) (*domain.BillingTransaction, error) {
		if err := requireAdminActor(actor); err != nil {
			return nil, err
		}
		amountNanos, err := billing.ParseAmount(input.Amount)
		if err != nil {
			return nil, domain.NewValidationError(err.Error())
		}
		currency := strings.ToUpper(strings.TrimSpace(input.Currency))
		if currency == "" {
			currency = deps.currency
		}
		return deps.billing.ApplyManualTransaction(ctx, postgres.ManualBillingTransactionParams{
			UserID: input.UserID, ActorUserID: actor.ID, ActorRole: actor.Role, Currency: currency,
			Kind: kind, AmountNanos: amountNanos, Reason: input.Reason, Reference: input.Reference,
			IdempotencyKey: input.IdempotencyKey, RequestID: input.RequestID,
		})
	}
	useCases.Billing.ApplyManualTopup = func(ctx context.Context, actor *domain.User, input server.ManualBillingInput) (*domain.BillingTransaction, error) {
		return manual(ctx, actor, input, domain.BillingTransactionManualTopup)
	}
	useCases.Billing.ApplyManualRefund = func(ctx context.Context, actor *domain.User, input server.ManualBillingInput) (*domain.BillingTransaction, error) {
		return manual(ctx, actor, input, domain.BillingTransactionManualRefund)
	}
	useCases.Billing.IssueRedemptionCodes = func(ctx context.Context, actor *domain.User, input server.IssueRedemptionCodeInput) ([]domain.BillingRedemptionCodeIssue, error) {
		if err := requireAdminActor(actor); err != nil {
			return nil, err
		}
		amountNanos, err := billing.ParseAmount(input.Amount)
		if err != nil {
			return nil, domain.NewValidationError(err.Error())
		}
		return deps.billing.IssueRedemptionCodes(ctx, postgres.IssueRedemptionCodeParams{
			ActorUserID: actor.ID, ActorRole: actor.Role, Currency: deps.currency,
			AmountNanos: amountNanos, Quantity: input.Quantity, ExpiresAt: input.ExpiresAt, RequestID: input.RequestID,
		})
	}
	useCases.Billing.ListRedemptionCodes = func(ctx context.Context, actor *domain.User, limit int, cursor string) (*server.PageResult[domain.BillingRedemptionCode], error) {
		if err := requireAdminActor(actor); err != nil {
			return nil, err
		}
		items, next, err := deps.billing.ListRedemptionCodes(ctx, postgres.RedemptionCodeListParams{Limit: limit, Cursor: cursor})
		return &server.PageResult[domain.BillingRedemptionCode]{Items: items, NextCursor: next}, err
	}
	useCases.Billing.DisableRedemptionCode = func(ctx context.Context, actor *domain.User, codeID string, requestID string) (*domain.BillingRedemptionCode, error) {
		if err := requireAdminActor(actor); err != nil {
			return nil, err
		}
		return deps.billing.DisableRedemptionCode(ctx, postgres.DisableRedemptionCodeParams{
			CodeID: codeID, ActorUserID: actor.ID, ActorRole: actor.Role, RequestID: requestID,
		})
	}
	useCases.Billing.RedeemBillingCode = func(ctx context.Context, actor *domain.User, input server.RedeemBillingCodeInput) (*domain.BillingRedemptionResult, error) {
		return deps.billing.RedeemCode(ctx, actor.ID, actor.Role, input.Code, input.RequestID)
	}
	useCases.Billing.ListBillingTransactions = func(ctx context.Context, input server.BillingListInput) (*server.PageResult[domain.BillingTransaction], error) {
		items, next, err := deps.billing.ListTransactions(ctx, postgres.BillingListParams(input))
		return &server.PageResult[domain.BillingTransaction]{Items: items, NextCursor: next}, err
	}
	useCases.Billing.GetBillingTransaction = deps.billing.GetTransaction
	useCases.Billing.ListBillingUsageEvents = func(ctx context.Context, input server.BillingListInput) (*server.PageResult[domain.BillingUsageEvent], error) {
		items, next, err := deps.billing.ListUsageEvents(ctx, postgres.BillingListParams(input))
		return &server.PageResult[domain.BillingUsageEvent]{Items: items, NextCursor: next}, err
	}
	useCases.Billing.GetBillingUsageEvent = deps.billing.GetUsageEvent
	useCases.Audit.ListAuditEvents = func(ctx context.Context, input server.AuditListInput) (*server.PageResult[domain.AuditEvent], error) {
		items, next, err := deps.audit.List(ctx, postgres.AuditListParams(input))
		return &server.PageResult[domain.AuditEvent]{Items: items, NextCursor: next}, err
	}
	useCases.Audit.GetAuditEvent = deps.audit.Get
	useCases.Audit.RecordAudit = func(ctx context.Context, input server.RecordAuditInput) error {
		_, err := deps.audit.Record(ctx, postgres.RecordAuditParams(input))
		return err
	}
}

func requireAdminActor(actor *domain.User) error {
	if actor == nil || !domain.UserRoleSatisfies(actor.Role, domain.UserRoleAdmin) {
		return domain.NewForbiddenError("administrator privileges are required")
	}
	return nil
}

func requireSystemActor(actor *domain.User) error {
	if actor == nil || actor.Role != domain.UserRoleSystem {
		return domain.NewForbiddenError("system privileges are required")
	}
	return nil
}

func normalizeProviderBaseURL(value string) (string, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", domain.NewValidationError("base_url is invalid")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", domain.NewValidationError("base_url is invalid")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func normalizeReasoningEfforts(values []string) ([]string, error) {
	if values == nil {
		return []string{}, nil
	}
	requested := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		switch value {
		case "low", "medium", "high", "xhigh":
			requested[value] = struct{}{}
		default:
			return nil, domain.NewValidationError("supported_reasoning_efforts may contain only low, medium, high, and xhigh")
		}
	}
	ordered := make([]string, 0, len(requested))
	for _, value := range []string{"low", "medium", "high", "xhigh"} {
		if _, ok := requested[value]; ok {
			ordered = append(ordered, value)
		}
	}
	return ordered, nil
}

func validateModelDefaultParameters(raw json.RawMessage, supportedEfforts []string) error {
	if len(raw) == 0 {
		return nil
	}
	var parameters map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parameters); err != nil || parameters == nil {
		return domain.NewValidationError("default_parameters must be a JSON object")
	}
	encoded, ok := parameters["reasoning_effort"]
	if !ok {
		return nil
	}
	var effort string
	if err := json.Unmarshal(encoded, &effort); err != nil {
		return domain.NewValidationError("default_parameters.reasoning_effort must be a string")
	}
	effort = strings.TrimSpace(effort)
	for _, supported := range supportedEfforts {
		if effort == supported {
			return nil
		}
	}
	return domain.NewValidationError("default_parameters.reasoning_effort must be included in supported_reasoning_efforts")
}
