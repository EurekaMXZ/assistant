package domain

import (
	"encoding/json"
	"time"
)

const (
	MCPValidationUntested = "untested"
	MCPValidationValid    = "valid"
	MCPValidationInvalid  = "invalid"
)

type MCPSecret struct {
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
	KeyHint    string `json:"key_hint,omitempty"`
}

type UserMCPTool struct {
	ServerID    string          `json:"-"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Enabled     bool            `json:"enabled"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type UserMCPServer struct {
	ID                   string        `json:"id"`
	OwnerUserID          string        `json:"-"`
	Name                 string        `json:"name"`
	Slug                 string        `json:"slug"`
	EndpointURL          string        `json:"endpoint_url"`
	Enabled              bool          `json:"enabled"`
	Revision             int64         `json:"revision"`
	Parameters           []MCPSecret   `json:"parameters"`
	Headers              []MCPSecret   `json:"headers"`
	Tools                []UserMCPTool `json:"tools"`
	LastValidationStatus string        `json:"last_validation_status"`
	LastValidationError  string        `json:"last_validation_error,omitempty"`
	LastValidatedAt      *time.Time    `json:"last_validated_at,omitempty"`
	CreatedAt            time.Time     `json:"created_at"`
	UpdatedAt            time.Time     `json:"updated_at"`
	EncryptedParameters  []byte        `json:"-"`
	ParametersNonce      []byte        `json:"-"`
	EncryptedHeaders     []byte        `json:"-"`
	HeadersNonce         []byte        `json:"-"`
}
