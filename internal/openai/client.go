package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/credential"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

const (
	defaultReasoningEffort  = "xhigh"
	defaultReasoningSummary = "detailed"
)

type Client struct {
	userAgent   string
	httpClient  *http.Client
	credentials CredentialResolver
}

type CredentialResolver interface {
	ResolveCredential(ctx context.Context, credentialID string) (*credential.Resolved, error)
}

var _ llm.ModelClient = (*Client)(nil)

type outputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseOutputItem struct {
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	Status            string          `json:"status"`
	Role              string          `json:"role"`
	Phase             string          `json:"phase"`
	Name              string          `json:"name"`
	Namespace         string          `json:"namespace"`
	CallID            string          `json:"call_id"`
	Arguments         json.RawMessage `json:"arguments"`
	Output            json.RawMessage `json:"output"`
	Result            string          `json:"result"`
	RevisedPrompt     string          `json:"revised_prompt"`
	ServerLabel       string          `json:"server_label"`
	ApprovalRequestID string          `json:"approval_request_id"`
	Content           []outputContent `json:"content"`
	Error             json.RawMessage `json:"error"`
}

func New(settings Settings) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if settings.HTTPClientTimeout > 0 {
		transport.ResponseHeaderTimeout = settings.HTTPClientTimeout
	}
	return &Client{
		userAgent: strings.TrimSpace(settings.UserAgent),
		httpClient: &http.Client{
			Transport: transport,
		},
	}
}

func (c *Client) SetCredentialResolver(resolver CredentialResolver) {
	c.credentials = resolver
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func (c *Client) MarshalRequest(request llm.ModelRequest) (json.RawMessage, error) {
	input, err := marshalInputItems(request.Input)
	if err != nil {
		return nil, err
	}
	tools, err := marshalTools(request.Tools)
	if err != nil {
		return nil, err
	}
	payload := struct {
		Model             string            `json:"model"`
		Instructions      string            `json:"instructions,omitempty"`
		Reasoning         *reasoningConfig  `json:"reasoning,omitempty"`
		Text              *textConfig       `json:"text,omitempty"`
		Input             []any             `json:"input"`
		Tools             []any             `json:"tools,omitempty"`
		Include           []string          `json:"include,omitempty"`
		PromptCacheKey    string            `json:"prompt_cache_key,omitempty"`
		ToolChoice        string            `json:"tool_choice,omitempty"`
		Stream            bool              `json:"stream"`
		Store             bool              `json:"store"`
		MaxOutputTokens   int               `json:"max_output_tokens,omitempty"`
		Metadata          map[string]string `json:"metadata,omitempty"`
		ParallelToolCalls *bool             `json:"parallel_tool_calls,omitempty"`
	}{
		Model:             request.Model,
		Instructions:      strings.TrimSpace(request.Instructions),
		Reasoning:         requestReasoningConfig(request),
		Text:              requestTextConfig(request),
		Input:             input,
		Tools:             tools,
		Include:           modelRequestIncludes(request.Include),
		PromptCacheKey:    strings.TrimSpace(request.PromptCacheKey),
		ToolChoice:        strings.TrimSpace(request.ToolChoice),
		Stream:            true,
		Store:             false,
		MaxOutputTokens:   request.MaxOutputTokens,
		Metadata:          request.Metadata,
		ParallelToolCalls: request.ParallelToolCalls,
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal model request: %w", err)
	}

	return raw, nil
}

type reasoningConfig struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type textConfig struct {
	Verbosity string `json:"verbosity,omitempty"`
}

func defaultReasoningConfig() *reasoningConfig {
	return &reasoningConfig{
		Effort:  defaultReasoningEffort,
		Summary: defaultReasoningSummary,
	}
}

func requestReasoningConfig(request llm.ModelRequest) *reasoningConfig {
	config := defaultReasoningConfig()
	if value := strings.TrimSpace(request.ReasoningEffort); value != "" {
		config.Effort = value
	}
	if value := strings.TrimSpace(request.ReasoningSummary); value != "" {
		config.Summary = value
	}
	return config
}

func requestTextConfig(request llm.ModelRequest) *textConfig {
	if value := strings.TrimSpace(request.TextVerbosity); value != "" {
		return &textConfig{Verbosity: value}
	}
	return nil
}

func modelRequestIncludes(includes []string) []string {
	out := make([]string, 0, len(includes)+1)
	seen := map[string]struct{}{}
	for _, include := range includes {
		include = strings.TrimSpace(include)
		if include == "" {
			continue
		}
		if _, ok := seen[include]; ok {
			continue
		}
		seen[include] = struct{}{}
		out = append(out, include)
	}
	if _, ok := seen[llm.ModelIncludeReasoningEncryptedContent]; !ok {
		out = append(out, llm.ModelIncludeReasoningEncryptedContent)
	}
	return out
}

func (c *Client) StreamResponse(ctx context.Context, request llm.ModelRequest, handler llm.ModelEventHandler) (*llm.ModelResult, error) {
	rawRequest, err := c.MarshalRequest(request)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(request.CredentialID) == "" {
		return nil, upstreamRequestError("resolve provider credential", errors.New("model request has no catalog credential"))
	}
	if c.credentials == nil {
		return nil, upstreamRequestError("resolve provider credential", errors.New("provider credential resolver is not configured"))
	}
	resolved, resolveErr := c.credentials.ResolveCredential(ctx, request.CredentialID)
	if resolveErr != nil {
		return nil, upstreamRequestError("resolve provider credential", resolveErr)
	}
	baseURL := resolved.BaseURL
	apiKey := resolved.APIKey
	if value := strings.TrimSpace(request.ProviderBaseURL); value != "" {
		baseURL = value
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, normalizeBaseURL(baseURL)+"/responses", bytes.NewReader(rawRequest))
	if err != nil {
		return nil, fmt.Errorf("create openai request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	if c.userAgent != "" {
		httpRequest.Header.Set("User-Agent", c.userAgent)
	}

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, upstreamRequestError("send openai request", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return nil, upstreamRequestError(
			"openai request failed",
			fmt.Errorf("status=%d body=%s", response.StatusCode, strings.TrimSpace(string(body))),
		)
	}

	result := &llm.ModelResult{
		RawRequest: rawRequest,
	}

	reader := bufio.NewReader(response.Body)

	var (
		eventType     string
		dataLines     []string
		textItemOrder []string
		textItems     = map[string]*llm.ModelTextItem{}
	)

	ensureTextItem := func(responseID string, itemID string, outputIndex int, contentIndex int) *llm.ModelTextItem {
		key := modelTextItemKey(responseID, itemID, outputIndex, contentIndex)
		if item, ok := textItems[key]; ok {
			return item
		}
		item := &llm.ModelTextItem{
			ResponseID:   responseID,
			ItemID:       itemID,
			OutputIndex:  outputIndex,
			ContentIndex: contentIndex,
		}
		textItems[key] = item
		textItemOrder = append(textItemOrder, key)
		return item
	}

	processFrame := func() error {
		if len(dataLines) == 0 {
			return nil
		}

		data := strings.Join(dataLines, "\n")
		dataLines = nil

		if strings.TrimSpace(data) == "[DONE]" {
			eventType = ""
			return nil
		}

		raw := json.RawMessage(data)
		result.StreamEvents = append(result.StreamEvents, append(json.RawMessage(nil), raw...))

		var envelope struct {
			Type         string `json:"type"`
			ResponseID   string `json:"response_id"`
			ItemID       string `json:"item_id"`
			OutputIndex  int    `json:"output_index"`
			ContentIndex int    `json:"content_index"`
			Delta        string `json:"delta"`
			Text         string `json:"text"`
			Error        *struct {
				Message string `json:"message"`
			} `json:"error"`
			Response *struct {
				ID     string            `json:"id"`
				Status string            `json:"status"`
				Usage  json.RawMessage   `json:"usage"`
				Output []json.RawMessage `json:"output"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"response"`
		}

		if err := json.Unmarshal(raw, &envelope); err != nil {
			return upstreamRequestError("decode stream event", err)
		}

		if envelope.Type == "" {
			envelope.Type = eventType
		}
		responseID := strings.TrimSpace(envelope.ResponseID)
		if responseID == "" && envelope.Response != nil {
			responseID = strings.TrimSpace(envelope.Response.ID)
		}
		if responseID != "" {
			result.ResponseID = responseID
		}

		emit := func(event llm.ModelEvent) error {
			if handler == nil {
				return nil
			}
			if event.Type == "" {
				event.Type = envelope.Type
			}
			if event.ResponseID == "" {
				event.ResponseID = responseID
			}
			if event.ItemID == "" {
				event.ItemID = strings.TrimSpace(envelope.ItemID)
			}
			if event.OutputIndex == 0 {
				event.OutputIndex = envelope.OutputIndex
			}
			if event.ContentIndex == 0 {
				event.ContentIndex = envelope.ContentIndex
			}
			if len(event.Raw) == 0 {
				event.Raw = append(json.RawMessage(nil), raw...)
			}
			return handler(event)
		}

		switch envelope.Type {
		case "response.created", "response.in_progress":
			if err := emit(llm.ModelEvent{Type: envelope.Type}); err != nil {
				return err
			}
		case "response.output_text.delta":
			item := ensureTextItem(responseID, envelope.ItemID, envelope.OutputIndex, envelope.ContentIndex)
			item.Text += envelope.Delta
			result.FinalText += envelope.Delta
			if err := emit(llm.ModelEvent{Type: envelope.Type, Delta: envelope.Delta}); err != nil {
				return err
			}
		case "response.output_text.done":
			item := ensureTextItem(responseID, envelope.ItemID, envelope.OutputIndex, envelope.ContentIndex)
			if envelope.Text != "" {
				item.Text = envelope.Text
			}
			if result.FinalText == "" && envelope.Text != "" {
				result.FinalText = envelope.Text
			}
			if err := emit(llm.ModelEvent{Type: envelope.Type, Text: envelope.Text}); err != nil {
				return err
			}
		case "response.completed":
			result.RawResponse = raw
			if envelope.Response != nil {
				usage, err := parseModelUsage(envelope.Response.Usage)
				if err != nil {
					return upstreamRequestError("decode usage", err)
				}
				result.Usage = usage
				result.OutputItems = parseOutputItems(envelope.Response.Output)
				result.TextItems = parseOutputTextItems(result.ResponseID, envelope.Response.Output)
				if len(result.TextItems) == 0 {
					result.TextItems = streamedOutputTextItems(textItemOrder, textItems)
				}
				if len(result.TextItems) > 0 {
					result.FinalText = flattenModelTextItems(result.TextItems)
				} else if result.FinalText == "" {
					result.FinalText = flattenOutputText(result.OutputItems)
				}
			}
			if err := emit(llm.ModelEvent{Type: envelope.Type, Text: result.FinalText}); err != nil {
				return err
			}
		case "response.failed", "error":
			message := "openai streaming failed"
			if envelope.Error != nil && strings.TrimSpace(envelope.Error.Message) != "" {
				message = envelope.Error.Message
			} else if envelope.Response != nil && envelope.Response.Error != nil && strings.TrimSpace(envelope.Response.Error.Message) != "" {
				message = envelope.Response.Error.Message
			}
			if err := emit(llm.ModelEvent{Type: envelope.Type, Error: domain.TurnPublicErrorUpstreamRequestFailed}); err != nil {
				return err
			}
			return upstreamRequestError("openai streaming failed", errors.New(message))
		default:
			if err := emit(llm.ModelEvent{Type: envelope.Type}); err != nil {
				return err
			}
		}

		eventType = ""
		return nil
	}

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
		}
		if len(line) > 0 || err == nil {
			if line == "" {
				if err := processFrame(); err != nil {
					return result, err
				}
			} else {
				switch {
				case strings.HasPrefix(line, "event:"):
					eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				case strings.HasPrefix(line, "data:"):
					dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return result, upstreamRequestError("read openai stream", err)
		}
	}

	if err := processFrame(); err != nil {
		return result, err
	}

	return result, nil
}

func upstreamRequestError(operation string, err error) error {
	return fmt.Errorf("%w: %s: %v", llm.ErrUpstreamRequestFailed, operation, err)
}

func marshalInputItems(items []llm.ModelItem) ([]any, error) {
	input := make([]any, 0, len(items))
	for _, item := range items {
		payload, err := marshalInputItem(item)
		if err != nil {
			return nil, err
		}
		input = append(input, payload)
	}
	return input, nil
}

func marshalInputItem(item llm.ModelItem) (any, error) {
	if len(item.Raw) > 0 {
		return item.Raw, nil
	}

	switch item.Type {
	case "", llm.ModelItemMessage:
		return struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Phase   string `json:"phase,omitempty"`
			Content string `json:"content"`
		}{
			Type:    llm.ModelItemMessage,
			Role:    item.Role,
			Phase:   item.Phase,
			Content: item.Content,
		}, nil
	case llm.ModelItemFunctionCall:
		name := item.Name
		if item.Namespace != "" {
			name = qualifiedToolName(item.Namespace, item.Name)
		}
		name = llm.SafeToolName(name)
		return struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id,omitempty"`
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		}{
			Type:      llm.ModelItemFunctionCall,
			CallID:    item.CallID,
			Name:      name,
			Arguments: strings.TrimSpace(string(item.Arguments)),
		}, nil
	case llm.ModelItemFunctionCallOutput:
		return struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Output string `json:"output"`
		}{
			Type:   llm.ModelItemFunctionCallOutput,
			CallID: item.CallID,
			Output: item.Output,
		}, nil
	case llm.ModelItemImageGenerationCall:
		return struct {
			Type string `json:"type"`
			ID   string `json:"id,omitempty"`
		}{
			Type: llm.ModelItemImageGenerationCall,
			ID:   item.ID,
		}, nil
	default:
		return nil, fmt.Errorf("marshal input item type %q: unsupported item type", item.Type)
	}
}

func marshalTools(tools []llm.ModelTool) ([]any, error) {
	if len(tools) == 0 {
		return nil, nil
	}

	payloads := make([]any, 0, len(tools))
	for _, tool := range tools {
		items, err := marshalTool(tool, "")
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, items...)
	}

	return payloads, nil
}

func marshalTool(tool llm.ModelTool, namespace string) ([]any, error) {
	if len(tool.Raw) > 0 {
		return []any{tool.Raw}, nil
	}

	switch tool.Type {
	case llm.ModelToolTypeFunction:
		name := llm.SafeToolName(qualifiedToolName(namespace, tool.Name))
		if name == "" {
			return nil, errors.New("marshal function tool: name is empty")
		}
		payload := map[string]any{
			"type":        llm.ModelToolTypeFunction,
			"name":        name,
			"description": tool.Description,
		}
		if len(tool.Parameters) > 0 {
			payload["parameters"] = tool.Parameters
		}
		if tool.Strict {
			payload["strict"] = true
		}
		if tool.DeferLoading {
			payload["defer_loading"] = true
		}
		return []any{payload}, nil
	case llm.ModelToolTypeNamespace:
		nextNamespace := qualifiedToolName(namespace, tool.Name)
		payloads := make([]any, 0, len(tool.Tools))
		for _, child := range tool.Tools {
			items, err := marshalTool(child, nextNamespace)
			if err != nil {
				return nil, err
			}
			payloads = append(payloads, items...)
		}
		return payloads, nil
	case llm.ModelToolTypeMCP:
		payload := map[string]any{
			"type":               llm.ModelToolTypeMCP,
			"server_label":       tool.ServerLabel,
			"server_description": tool.ServerDescription,
			"server_url":         tool.ServerURL,
		}
		if len(tool.AllowedTools) > 0 {
			payload["allowed_tools"] = tool.AllowedTools
		}
		if len(tool.Headers) > 0 {
			payload["headers"] = tool.Headers
		}
		if tool.DeferLoading {
			payload["defer_loading"] = true
		}
		return []any{payload}, nil
	case llm.ModelToolTypeImageGeneration:
		payload := map[string]any{
			"type": llm.ModelToolTypeImageGeneration,
		}
		if tool.Size != "" {
			payload["size"] = tool.Size
		}
		if tool.Quality != "" {
			payload["quality"] = tool.Quality
		}
		if tool.OutputFormat != "" {
			payload["output_format"] = tool.OutputFormat
		}
		if tool.OutputCompression != nil {
			payload["output_compression"] = *tool.OutputCompression
		}
		if tool.Background != "" {
			payload["background"] = tool.Background
		}
		if tool.Moderation != "" {
			payload["moderation"] = tool.Moderation
		}
		if tool.PartialImages > 0 {
			payload["partial_images"] = tool.PartialImages
		}
		return []any{payload}, nil
	case llm.ModelToolTypeToolSearch:
		payload := map[string]any{
			"type":        llm.ModelToolTypeToolSearch,
			"description": tool.Description,
		}
		if tool.Execution != "" {
			payload["execution"] = tool.Execution
		}
		if len(tool.Parameters) > 0 {
			payload["parameters"] = tool.Parameters
		}
		return []any{payload}, nil
	default:
		return nil, fmt.Errorf("marshal tool type %q: unsupported tool type", tool.Type)
	}
}

func qualifiedToolName(namespace string, name string) string {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" {
		return name
	}
	if name == "" {
		return namespace
	}
	if strings.Contains(name, ".") {
		return name
	}
	return namespace + "." + name
}

func parseOutputItems(rawItems []json.RawMessage) []llm.ModelItem {
	items := make([]llm.ModelItem, 0, len(rawItems))
	for _, raw := range rawItems {
		var output responseOutputItem
		if err := json.Unmarshal(raw, &output); err != nil {
			continue
		}

		item := llm.ModelItem{
			ID:                output.ID,
			Type:              output.Type,
			Status:            output.Status,
			Role:              output.Role,
			Phase:             output.Phase,
			Name:              output.Name,
			Namespace:         output.Namespace,
			CallID:            output.CallID,
			Output:            decodeJSONStringOrRaw(output.Output),
			Result:            strings.TrimSpace(output.Result),
			RevisedPrompt:     strings.TrimSpace(output.RevisedPrompt),
			ServerLabel:       output.ServerLabel,
			ApprovalRequestID: output.ApprovalRequestID,
			Raw:               append(json.RawMessage(nil), raw...),
		}
		if len(output.Arguments) > 0 && string(output.Arguments) != "null" {
			item.Arguments = decodeRawJSON(output.Arguments)
		}
		item.Content = flattenContentText(output.Content)
		item.Error = decodeModelItemError(output.Error)

		items = append(items, item)
	}
	return items
}

func parseOutputTextItems(responseID string, rawItems []json.RawMessage) []llm.ModelTextItem {
	items := make([]llm.ModelTextItem, 0, len(rawItems))
	for outputIndex, raw := range rawItems {
		var output responseOutputItem
		if err := json.Unmarshal(raw, &output); err != nil {
			continue
		}
		if output.Type != llm.ModelItemMessage || output.Role != "assistant" {
			continue
		}
		for contentIndex, content := range output.Content {
			if content.Type != "output_text" && content.Type != "text" {
				continue
			}
			if strings.TrimSpace(content.Text) == "" {
				continue
			}
			items = append(items, llm.ModelTextItem{
				ResponseID:   strings.TrimSpace(responseID),
				ItemID:       strings.TrimSpace(output.ID),
				OutputIndex:  outputIndex,
				ContentIndex: contentIndex,
				Text:         content.Text,
			})
		}
	}
	return items
}

func streamedOutputTextItems(order []string, byKey map[string]*llm.ModelTextItem) []llm.ModelTextItem {
	if len(order) == 0 {
		return nil
	}
	items := make([]llm.ModelTextItem, 0, len(order))
	for _, key := range order {
		item := byKey[key]
		if item == nil || strings.TrimSpace(item.Text) == "" {
			continue
		}
		items = append(items, *item)
	}
	return items
}

func modelTextItemKey(responseID string, itemID string, outputIndex int, contentIndex int) string {
	if strings.TrimSpace(itemID) != "" {
		return fmt.Sprintf("%s:%s:%d", strings.TrimSpace(responseID), strings.TrimSpace(itemID), contentIndex)
	}
	return fmt.Sprintf("%s:%d:%d", strings.TrimSpace(responseID), outputIndex, contentIndex)
}

func decodeJSONStringOrRaw(raw json.RawMessage) string {
	decoded := decodeRawJSON(raw)
	if len(decoded) == 0 {
		return ""
	}
	return string(decoded)
}

func decodeRawJSON(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}

	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return json.RawMessage(strings.TrimSpace(value))
		}
	}

	return append(json.RawMessage(nil), raw...)
}

func decodeModelItemError(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}

	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return strings.TrimSpace(value)
		}
	}

	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && strings.TrimSpace(payload.Message) != "" {
		return strings.TrimSpace(payload.Message)
	}

	return trimmed
}

func flattenOutputText(items []llm.ModelItem) string {
	var builder strings.Builder
	for _, item := range items {
		if item.Type != llm.ModelItemMessage || item.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(item.Content) == "" {
			continue
		}
		builder.WriteString(item.Content)
	}
	return builder.String()
}

func flattenModelTextItems(items []llm.ModelTextItem) string {
	var builder strings.Builder
	for _, item := range items {
		builder.WriteString(item.Text)
	}
	return builder.String()
}

func flattenContentText(content []outputContent) string {
	var builder strings.Builder
	for _, item := range content {
		if item.Type != "output_text" && item.Type != "text" {
			continue
		}
		builder.WriteString(item.Text)
	}
	return builder.String()
}

func parseModelUsage(raw json.RawMessage) (llm.ModelUsage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return llm.ModelUsage{}, nil
	}

	var payload struct {
		InputTokens              int `json:"input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		TotalTokens              int `json:"total_tokens"`
		InputTokensDetails       struct {
			CachedTokens        int `json:"cached_tokens"`
			CacheReadTokens     int `json:"cache_read_tokens"`
			CacheCreationTokens int `json:"cache_creation_tokens"`
		} `json:"input_tokens_details"`
		OutputTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return llm.ModelUsage{}, err
	}

	cacheRead := payload.CacheReadInputTokens
	if cacheRead == 0 {
		cacheRead = payload.InputTokensDetails.CacheReadTokens
	}
	if cacheRead == 0 {
		cacheRead = payload.InputTokensDetails.CachedTokens
	}
	cacheCreation := payload.CacheCreationInputTokens
	if cacheCreation == 0 {
		cacheCreation = payload.InputTokensDetails.CacheCreationTokens
	}
	return llm.ModelUsage{
		InputTokens:              payload.InputTokens,
		CacheReadInputTokens:     cacheRead,
		CacheCreationInputTokens: cacheCreation,
		OutputTokens:             payload.OutputTokens,
		ReasoningOutputTokens:    payload.OutputTokensDetails.ReasoningTokens,
		TotalTokens:              payload.TotalTokens,
		Raw:                      append(json.RawMessage(nil), raw...),
	}, nil
}
