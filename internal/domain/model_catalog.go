package domain

import (
	"encoding/json"
	"time"
)

const (
	ProviderOpenAI = "openai"

	CredentialStatusEnabled  = "enabled"
	CredentialStatusDisabled = "disabled"
	CredentialStatusRevoked  = "revoked"

	ModelStatusEnabled  = "enabled"
	ModelStatusDisabled = "disabled"

	ModelPriceStatusPublished = "published"
	ModelPriceStatusArchived  = "archived"
)

type ProviderCredential struct {
	ID                  string     `json:"id"`
	Provider            string     `json:"provider"`
	Name                string     `json:"name"`
	BaseURL             string     `json:"base_url"`
	MaskedKey           string     `json:"masked_key"`
	Status              string     `json:"status"`
	LastValidatedAt     *time.Time `json:"last_validated_at,omitempty"`
	LastValidationError string     `json:"last_validation_error,omitempty"`
	CreatedByUserID     string     `json:"created_by_user_id"`
	UpdatedByUserID     string     `json:"updated_by_user_id"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type StoredProviderCredential struct {
	ProviderCredential
	EncryptedAPIKey []byte
	Nonce           []byte
	KeyVersion      int
}

type Model struct {
	ID                        string          `json:"id"`
	Provider                  string          `json:"provider"`
	CredentialID              string          `json:"credential_id,omitempty"`
	Slug                      string          `json:"slug"`
	UpstreamModel             string          `json:"upstream_model"`
	DisplayName               string          `json:"display_name"`
	Description               string          `json:"description"`
	InputModalities           []string        `json:"input_modalities"`
	OutputModalities          []string        `json:"output_modalities"`
	SupportsTools             bool            `json:"supports_tools"`
	SupportsParallelTools     bool            `json:"supports_parallel_tools"`
	SupportedReasoningEfforts []string        `json:"supported_reasoning_efforts"`
	ContextWindowTokens       int             `json:"context_window_tokens"`
	MaxOutputTokens           int             `json:"max_output_tokens"`
	DefaultParameters         json.RawMessage `json:"default_parameters"`
	Status                    string          `json:"status"`
	IsDefault                 bool            `json:"is_default,omitempty"`
	Revision                  int64           `json:"revision"`
	CreatedByUserID           string          `json:"created_by_user_id"`
	UpdatedByUserID           string          `json:"updated_by_user_id"`
	CreatedAt                 time.Time       `json:"created_at"`
	UpdatedAt                 time.Time       `json:"updated_at"`
}

type ModelPriceVersion struct {
	ID                                string          `json:"id"`
	ModelID                           string          `json:"model_id"`
	Version                           int64           `json:"version"`
	Currency                          string          `json:"currency"`
	InputPerMillionNanos              int64           `json:"input_per_million_nanos"`
	CacheReadInputPerMillionNanos     int64           `json:"cache_read_input_per_million_nanos"`
	CacheCreationInputPerMillionNanos int64           `json:"cache_creation_input_per_million_nanos"`
	OutputPerMillionNanos             int64           `json:"output_per_million_nanos"`
	ImageInputPerMillionNanos         *int64          `json:"image_input_per_million_nanos,omitempty"`
	ImageOutputPerImageNanos          *int64          `json:"image_output_per_image_nanos,omitempty"`
	Status                            string          `json:"status"`
	EffectiveFrom                     *time.Time      `json:"effective_from,omitempty"`
	PricingSnapshot                   json.RawMessage `json:"pricing_snapshot"`
	CreatedByUserID                   string          `json:"created_by_user_id"`
	PublishedByUserID                 string          `json:"published_by_user_id,omitempty"`
	PublishedAt                       *time.Time      `json:"published_at,omitempty"`
	ArchivedAt                        *time.Time      `json:"archived_at,omitempty"`
	CreatedAt                         time.Time       `json:"created_at"`
}

type ModelSettings struct {
	DefaultChatModelID string    `json:"default_chat_model_id,omitempty"`
	CompactionModelID  string    `json:"compaction_model_id,omitempty"`
	UpdatedByUserID    string    `json:"updated_by_user_id,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ModelExecutionSnapshot struct {
	ModelID                   string          `json:"model_id"`
	ModelRevision             int64           `json:"model_revision"`
	Provider                  string          `json:"provider"`
	CredentialID              string          `json:"credential_id"`
	BaseURL                   string          `json:"base_url"`
	UpstreamModel             string          `json:"upstream_model"`
	ContextWindowTokens       int             `json:"context_window_tokens"`
	MaxOutputTokens           int             `json:"max_output_tokens"`
	SupportsTools             bool            `json:"supports_tools"`
	SupportsParallelTools     bool            `json:"supports_parallel_tools"`
	SupportedReasoningEfforts []string        `json:"supported_reasoning_efforts"`
	ReasoningEffort           string          `json:"reasoning_effort,omitempty"`
	DefaultParameters         json.RawMessage `json:"default_parameters"`
	ModelPriceID              string          `json:"model_price_id,omitempty"`
	Currency                  string          `json:"currency,omitempty"`
	PricingSnapshot           json.RawMessage `json:"pricing_snapshot,omitempty"`
}
