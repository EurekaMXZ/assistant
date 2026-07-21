package domain

import "time"

const ContextAnchorTypeCompressedHistory = "compressed_history"

type ContextHead struct {
	ConversationID            string    `json:"conversation_id"`
	Version                   int64     `json:"version"`
	AnchorGeneration          int64     `json:"anchor_generation"`
	AnchorKey                 string    `json:"anchor_key,omitempty"`
	CoveredUntilSeq           int64     `json:"covered_until_seq"`
	RawTailStartSeq           int64     `json:"raw_tail_start_seq"`
	LastSeq                   int64     `json:"last_seq"`
	ActiveContextTokens       int       `json:"active_context_tokens"`
	LatestRequestRunID        string    `json:"latest_request_run_id,omitempty"`
	LatestSuccessfulRunID     string    `json:"latest_successful_run_id,omitempty"`
	LatestCheckpointKey       string    `json:"latest_checkpoint_key,omitempty"`
	LatestCheckpointChecksum  string    `json:"latest_checkpoint_checksum,omitempty"`
	CheckpointCoveredEventSeq int64     `json:"checkpoint_covered_event_seq"`
	LastContextEventSeq       int64     `json:"last_context_event_seq"`
	ContextSchemaVersion      int       `json:"context_schema_version"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

type AnchorObject struct {
	Type               string `json:"type"`
	ConversationID     string `json:"conversation_id"`
	Generation         int64  `json:"generation"`
	CoveredFromSeq     int64  `json:"covered_from_seq"`
	CoveredUntilSeq    int64  `json:"covered_until_seq"`
	Role               string `json:"role"`
	Content            string `json:"content"`
	TokenCount         int    `json:"token_count"`
	ObjectKey          string `json:"object_key,omitempty"`
	CheckpointKey      string `json:"checkpoint_key,omitempty"`
	CheckpointChecksum string `json:"checkpoint_checksum,omitempty"`
}
