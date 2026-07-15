package domain

import (
	"encoding/json"
	"time"
)

const (
	SandboxStatusActive    = "active"
	SandboxStatusStopped   = "stopped"
	SandboxStatusReleasing = "releasing"
	SandboxStatusDestroyed = "destroyed"
	SandboxFileMaxBytes    = int64(128 << 20)
)

type SandboxHandle struct {
	Provider  string          `json:"provider"`
	RuntimeID string          `json:"runtime_id"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	Reused    bool            `json:"-"`
}

type ConversationSandbox struct {
	ID                    string          `json:"id"`
	ConversationID        string          `json:"conversation_id"`
	Provider              string          `json:"provider"`
	RuntimeID             string          `json:"runtime_id"`
	Status                string          `json:"status"`
	RuntimeMetadata       json.RawMessage `json:"runtime_metadata"`
	LastActivityAt        time.Time       `json:"last_activity_at"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
	StoppedAt             *time.Time      `json:"stopped_at,omitempty"`
	DestroyedAt           *time.Time      `json:"destroyed_at,omitempty"`
	ExecutionToken        string          `json:"-"`
	ExecutionLeaseUntil   *time.Time      `json:"-"`
	ReleasePreviousStatus string          `json:"-"`
	ReleaseToken          string          `json:"-"`
	ReleaseLeaseUntil     *time.Time      `json:"-"`
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
	Output           string   `json:"output,omitempty"`
	// Stdout and Stderr are retained for decoding results from older sandbox agents.
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out,omitempty"`
}
