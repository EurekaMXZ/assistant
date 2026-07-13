package domain

import (
	"encoding/json"
	"time"
)

const (
	SandboxStatusActive    = "active"
	SandboxStatusDestroyed = "destroyed"
)

type SandboxHandle struct {
	Provider  string          `json:"provider"`
	RuntimeID string          `json:"runtime_id"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	Reused    bool            `json:"-"`
}

type ConversationSandbox struct {
	ID              string          `json:"id"`
	ConversationID  string          `json:"conversation_id"`
	Provider        string          `json:"provider"`
	RuntimeID       string          `json:"runtime_id"`
	Status          string          `json:"status"`
	RuntimeMetadata json.RawMessage `json:"runtime_metadata"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	DestroyedAt     *time.Time      `json:"destroyed_at,omitempty"`
}

type SandboxCommandRequest struct {
	Command          string   `json:"command"`
	Args             []string `json:"args,omitempty"`
	WorkingDirectory string   `json:"working_directory,omitempty"`
	TimeoutSeconds   int      `json:"timeout_seconds,omitempty"`
}

type SandboxCommandResult struct {
	RuntimeID        string   `json:"runtime_id"`
	Command          string   `json:"command"`
	Args             []string `json:"args,omitempty"`
	WorkingDirectory string   `json:"working_directory,omitempty"`
	Stdout           string   `json:"stdout,omitempty"`
	Stderr           string   `json:"stderr,omitempty"`
	ExitCode         int      `json:"exit_code"`
	TimedOut         bool     `json:"timed_out,omitempty"`
}
