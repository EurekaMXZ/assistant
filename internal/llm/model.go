package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

var ErrUpstreamRequestFailed = errors.New("upstream request failed")

const (
	ModelItemMessage             = "message"
	ModelItemReasoning           = "reasoning"
	ModelItemFunctionCall        = "function_call"
	ModelItemFunctionCallOutput  = "function_call_output"
	ModelItemMCPApprovalRequest  = "mcp_approval_request"
	ModelItemMCPListTools        = "mcp_list_tools"
	ModelItemMCPCall             = "mcp_call"
	ModelItemImageGenerationCall = "image_generation_call"
	ModelItemToolSearchCall      = "tool_search_call"
)

const (
	ModelToolTypeFunction        = "function"
	ModelToolTypeNamespace       = "namespace"
	ModelToolTypeMCP             = "mcp"
	ModelToolTypeImageGeneration = "image_generation"
	ModelToolTypeToolSearch      = "tool_search"
)

const (
	ModelIncludeReasoningEncryptedContent = "reasoning.encrypted_content"
)

type ModelItem struct {
	ID                string          `json:"id,omitempty"`
	Type              string          `json:"type"`
	Status            string          `json:"status,omitempty"`
	Role              string          `json:"role,omitempty"`
	Phase             string          `json:"phase,omitempty"`
	Content           string          `json:"content,omitempty"`
	CallID            string          `json:"call_id,omitempty"`
	Name              string          `json:"name,omitempty"`
	Namespace         string          `json:"namespace,omitempty"`
	Arguments         json.RawMessage `json:"arguments,omitempty"`
	Output            string          `json:"output,omitempty"`
	Result            string          `json:"result,omitempty"`
	RevisedPrompt     string          `json:"revised_prompt,omitempty"`
	ServerLabel       string          `json:"server_label,omitempty"`
	ApprovalRequestID string          `json:"approval_request_id,omitempty"`
	Error             string          `json:"error,omitempty"`
	Raw               json.RawMessage `json:"raw,omitempty"`
}

type ModelTool struct {
	Type              string            `json:"type"`
	Name              string            `json:"name,omitempty"`
	Description       string            `json:"description,omitempty"`
	Parameters        json.RawMessage   `json:"parameters,omitempty"`
	Strict            bool              `json:"strict,omitempty"`
	DeferLoading      bool              `json:"defer_loading,omitempty"`
	Tools             []ModelTool       `json:"tools,omitempty"`
	ServerLabel       string            `json:"server_label,omitempty"`
	ServerDescription string            `json:"server_description,omitempty"`
	ServerURL         string            `json:"server_url,omitempty"`
	AllowedTools      []string          `json:"allowed_tools,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	Execution         string            `json:"execution,omitempty"`
	Size              string            `json:"size,omitempty"`
	Quality           string            `json:"quality,omitempty"`
	OutputFormat      string            `json:"output_format,omitempty"`
	OutputCompression *int              `json:"output_compression,omitempty"`
	Background        string            `json:"background,omitempty"`
	Moderation        string            `json:"moderation,omitempty"`
	PartialImages     int               `json:"partial_images,omitempty"`
	Raw               json.RawMessage   `json:"raw,omitempty"`
}

type ModelRequest struct {
	Model               string            `json:"model"`
	ContextWindowTokens int               `json:"context_window_tokens,omitempty"`
	CatalogModelID      string            `json:"catalog_model_id,omitempty"`
	ModelRevision       int64             `json:"model_revision,omitempty"`
	ModelPriceID        string            `json:"model_price_id,omitempty"`
	PricingSnapshot     json.RawMessage   `json:"pricing_snapshot,omitempty"`
	CredentialID        string            `json:"credential_id,omitempty"`
	ProviderBaseURL     string            `json:"provider_base_url,omitempty"`
	Instructions        string            `json:"instructions,omitempty"`
	Input               []ModelItem       `json:"input"`
	Tools               []ModelTool       `json:"tools,omitempty"`
	Include             []string          `json:"include,omitempty"`
	PromptCacheKey      string            `json:"prompt_cache_key,omitempty"`
	ToolChoice          string            `json:"tool_choice,omitempty"`
	ReasoningEffort     string            `json:"reasoning_effort,omitempty"`
	ReasoningSummary    string            `json:"reasoning_summary,omitempty"`
	TextVerbosity       string            `json:"text_verbosity,omitempty"`
	MaxOutputTokens     int               `json:"max_output_tokens,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	ParallelToolCalls   *bool             `json:"parallel_tool_calls,omitempty"`
}

type ModelUsage struct {
	InputTokens              int             `json:"input_tokens"`
	CacheReadInputTokens     int             `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int             `json:"cache_creation_input_tokens,omitempty"`
	OutputTokens             int             `json:"output_tokens"`
	ReasoningOutputTokens    int             `json:"reasoning_output_tokens,omitempty"`
	TotalTokens              int             `json:"total_tokens"`
	Raw                      json.RawMessage `json:"raw,omitempty"`
}

type ModelResult struct {
	ResponseID       string            `json:"response_id,omitempty"`
	FinalText        string            `json:"final_text,omitempty"`
	TextItems        []ModelTextItem   `json:"text_items,omitempty"`
	Usage            ModelUsage        `json:"usage"`
	RequestSizeBytes int64             `json:"request_size_bytes,omitempty"`
	RequestSHA256    string            `json:"request_sha256,omitempty"`
	RawResponse      json.RawMessage   `json:"raw_response,omitempty"`
	OutputItems      []ModelItem       `json:"output_items,omitempty"`
	StreamEvents     []json.RawMessage `json:"-"`
}

type ModelTextItem struct {
	ResponseID   string `json:"response_id,omitempty"`
	ItemID       string `json:"item_id,omitempty"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Text         string `json:"text"`
}

type ModelEvent struct {
	Type         string
	Delta        string
	Text         string
	ResponseID   string
	ItemID       string
	OutputIndex  int
	ContentIndex int
	Error        string
	Raw          json.RawMessage
	Image        *ModelImageEvent
}

type ModelImageEvent struct {
	PartialIndex int
	Base64       string
}

type ModelEventHandler func(ModelEvent) error

type ModelClient interface {
	MarshalRequest(request ModelRequest) (json.RawMessage, error)
	StreamResponse(ctx context.Context, request ModelRequest, handler ModelEventHandler) (*ModelResult, error)
}

func SafeToolName(name string) string {
	name = strings.TrimSpace(name)
	var builder strings.Builder
	builder.Grow(len(name))
	for _, r := range name {
		if isSafeToolNameRune(r) {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('_')
	}
	return builder.String()
}

func isSafeToolNameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '_' ||
		r == '-'
}
