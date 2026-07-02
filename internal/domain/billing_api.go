package domain

import (
	"encoding/json"
	"time"
)

const (
	BillingTransactionManualTopup  = "manual_topup"
	BillingTransactionManualRefund = "manual_refund"
)

type BillingAccount struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	Currency     string    `json:"currency"`
	Status       string    `json:"status"`
	BalanceNanos int64     `json:"balance_nanos"`
	Balance      string    `json:"balance"`
	Version      int64     `json:"version"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type BillingTransaction struct {
	ID                string    `json:"id"`
	AccountID         string    `json:"account_id"`
	UserID            string    `json:"user_id"`
	Currency          string    `json:"currency"`
	AccountSequence   int64     `json:"account_sequence"`
	Kind              string    `json:"kind"`
	Direction         string    `json:"direction"`
	AmountNanos       int64     `json:"amount_nanos"`
	Amount            string    `json:"amount"`
	BalanceAfterNanos int64     `json:"balance_after_nanos"`
	BalanceAfter      string    `json:"balance_after"`
	ActorUserID       string    `json:"actor_user_id,omitempty"`
	Reason            string    `json:"reason"`
	Reference         string    `json:"reference"`
	CreatedAt         time.Time `json:"created_at"`
}

type BillingUsageEvent struct {
	ID                       string          `json:"id"`
	RequestKey               string          `json:"request_key"`
	OwnerUserID              string          `json:"owner_user_id,omitempty"`
	ConversationID           string          `json:"conversation_id,omitempty"`
	TurnID                   string          `json:"turn_id,omitempty"`
	TurnRunID                string          `json:"turn_run_id,omitempty"`
	Workflow                 string          `json:"workflow"`
	Attempt                  int             `json:"attempt"`
	Provider                 string          `json:"provider"`
	ModelID                  string          `json:"model_id,omitempty"`
	ModelRevision            int64           `json:"model_revision,omitempty"`
	ModelPriceID             string          `json:"model_price_id,omitempty"`
	UpstreamModel            string          `json:"upstream_model"`
	ProviderResponseID       string          `json:"provider_response_id,omitempty"`
	Status                   string          `json:"status"`
	Currency                 string          `json:"currency,omitempty"`
	AmountNanos              *int64          `json:"amount_nanos,omitempty"`
	InputTokens              int             `json:"input_tokens"`
	CacheReadInputTokens     int             `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int             `json:"cache_creation_input_tokens"`
	OutputTokens             int             `json:"output_tokens"`
	ReasoningOutputTokens    int             `json:"reasoning_output_tokens"`
	TotalTokens              int             `json:"total_tokens"`
	PricingSnapshot          json.RawMessage `json:"pricing_snapshot"`
	Usage                    json.RawMessage `json:"usage"`
	BillingTransactionID     string          `json:"billing_transaction_id,omitempty"`
	ErrorCode                string          `json:"error_code,omitempty"`
	CreatedAt                time.Time       `json:"created_at"`
}

type AuditEvent struct {
	ID               string          `json:"id"`
	ActorUserID      string          `json:"actor_user_id,omitempty"`
	ActorRole        string          `json:"actor_role,omitempty"`
	SubjectUserID    string          `json:"subject_user_id,omitempty"`
	Action           string          `json:"action"`
	ResourceType     string          `json:"resource_type,omitempty"`
	ResourceID       string          `json:"resource_id,omitempty"`
	Outcome          string          `json:"outcome"`
	RequestID        string          `json:"request_id,omitempty"`
	ClientIP         string          `json:"client_ip,omitempty"`
	UserAgent        string          `json:"user_agent,omitempty"`
	Reason           string          `json:"reason,omitempty"`
	VisibleToSubject bool            `json:"visible_to_subject"`
	RequiredRole     string          `json:"required_role"`
	Metadata         json.RawMessage `json:"metadata"`
	CreatedAt        time.Time       `json:"created_at"`
}

type CursorPage struct {
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}
