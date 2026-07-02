package domain

import "time"

const ContextAnchorTypeCompressedHistory = "compressed_history"

type ContextHead struct {
	ConversationID      string    `json:"conversation_id"`
	AnchorGeneration    int64     `json:"anchor_generation"`
	AnchorKey           string    `json:"anchor_key,omitempty"`
	CoveredUntilSeq     int64     `json:"covered_until_seq"`
	RawTailStartSeq     int64     `json:"raw_tail_start_seq"`
	LastSeq             int64     `json:"last_seq"`
	ActiveContextTokens int       `json:"active_context_tokens"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type AnchorObject struct {
	Type            string `json:"type"`
	ConversationID  string `json:"conversation_id"`
	Generation      int64  `json:"generation"`
	CoveredFromSeq  int64  `json:"covered_from_seq"`
	CoveredUntilSeq int64  `json:"covered_until_seq"`
	Role            string `json:"role"`
	Content         string `json:"content"`
	TokenCount      int    `json:"token_count"`
	ObjectKey       string `json:"object_key,omitempty"`
}
