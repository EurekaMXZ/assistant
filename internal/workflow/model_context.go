package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

const turnModelContextContentType = "application/json"

func buildModelContextItems(initialInput []llm.ModelItem, currentInput []llm.ModelItem, final *llm.ModelResult, toolOutputMaxTokens int) []llm.ModelItem {
	var items []llm.ModelItem
	if len(currentInput) > len(initialInput) {
		items = append(items, cloneModelItems(currentInput[len(initialInput):])...)
	}
	if final != nil && len(final.OutputItems) > 0 {
		items = append(items, cloneModelItems(final.OutputItems)...)
	}
	if final != nil && strings.TrimSpace(final.FinalText) != "" && !hasAssistantMessageItem(items) {
		items = append(items, llm.ModelItem{
			Type:    llm.ModelItemMessage,
			Role:    domain.RoleAssistant,
			Content: strings.TrimSpace(final.FinalText),
		})
	}
	return truncateModelContextItems(items, toolOutputMaxTokens)
}

func estimateModelContextTokens(instructions string, items []llm.ModelItem, tools []llm.ModelTool) int {
	tokens := domain.EstimateTokens(instructions)
	for _, item := range items {
		if len(item.Raw) > 0 {
			if rawTokens, ok := estimateStructuredMessageTokens(item.Raw); ok {
				tokens += rawTokens
			} else {
				tokens += domain.EstimateByteTokens(len(item.Raw))
			}
			continue
		}
		if raw, err := json.Marshal(item); err == nil {
			tokens += domain.EstimateByteTokens(len(raw))
		}
	}
	for _, tool := range tools {
		if len(tool.Raw) > 0 {
			tokens += domain.EstimateByteTokens(len(tool.Raw))
			continue
		}
		if raw, err := json.Marshal(tool); err == nil {
			tokens += domain.EstimateByteTokens(len(raw))
		}
	}
	return tokens
}

func estimateStructuredMessageTokens(raw json.RawMessage) (int, bool) {
	var message struct {
		Type    string `json:"type"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL string `json:"image_url"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &message) != nil || message.Type != llm.ModelItemMessage || len(message.Content) == 0 {
		return 0, false
	}
	tokens := 8
	for _, part := range message.Content {
		switch part.Type {
		case "input_image":
			tokens += domain.EstimatedImageInputTokens
		default:
			tokens += domain.EstimateTokens(part.Text)
		}
	}
	return tokens, true
}

func truncateModelContextItems(items []llm.ModelItem, maxTokens int) []llm.ModelItem {
	truncated := cloneModelItems(items)
	for index := range truncated {
		truncated[index] = truncateModelContextItem(truncated[index], maxTokens)
	}
	return truncated
}

func truncateModelContextItem(item llm.ModelItem, maxTokens int) llm.ModelItem {
	if item.Type != llm.ModelItemFunctionCallOutput {
		return item
	}
	if item.Output == "" && len(item.Raw) > 0 {
		var payload struct {
			CallID string `json:"call_id"`
			Output string `json:"output"`
		}
		if json.Unmarshal(item.Raw, &payload) == nil {
			item.CallID = payload.CallID
			item.Output = payload.Output
		}
	}
	if maxTokens <= 0 {
		item.Output = ""
		item.Raw = nil
		return item
	}
	maxBytes := maxTokens * 4
	output := strings.ToValidUTF8(item.Output, "\ufffd")
	if len(output) <= maxBytes {
		item.Output = output
		return item
	}

	originalTokens := domain.EstimateByteTokens(len(output))
	totalLines := strings.Count(output, "\n") + 1
	header := fmt.Sprintf(
		"Warning: truncated output (original token count: %d)\nTotal output lines: %d\n\n",
		originalTokens,
		totalLines,
	)
	if len(header) >= maxBytes {
		item.Output = truncateMiddle(header, maxBytes)
		item.Raw = nil
		return item
	}
	item.Output = header + truncateMiddle(output, max(0, maxBytes-len(header)))
	item.Raw = nil
	return item
}

func truncateMiddle(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	marker := fmt.Sprintf("... %d tokens truncated ...", domain.EstimateByteTokens(len(value)-maxBytes))
	for range 2 {
		if len(marker) >= maxBytes {
			return marker[:maxBytes]
		}
		retainedBytes := maxBytes - len(marker)
		leftBytes := retainedBytes / 2
		rightBytes := retainedBytes - leftBytes
		for leftBytes > 0 && !utf8.ValidString(value[:leftBytes]) {
			leftBytes--
		}
		rightStart := len(value) - rightBytes
		for rightStart < len(value) && !utf8.ValidString(value[rightStart:]) {
			rightStart++
		}
		nextMarker := fmt.Sprintf("... %d tokens truncated ...", domain.EstimateByteTokens(rightStart-leftBytes))
		if len(nextMarker) == len(marker) {
			return value[:leftBytes] + nextMarker + value[rightStart:]
		}
		marker = nextMarker
	}
	return marker[:min(len(marker), maxBytes)]
}

func compactTriggerTokenLimit(configured int, contextWindow int) int {
	automatic := contextWindow * 9 / 10
	if automatic <= 0 {
		return max(0, configured)
	}
	if configured <= 0 || configured > automatic {
		return automatic
	}
	return configured
}

func remainingToolOutputTokens(request llm.ModelRequest, input []llm.ModelItem, providerTotalTokens int) int {
	if request.ContextWindowTokens <= 0 {
		return -1
	}
	usedTokens := providerTotalTokens
	if usedTokens <= 0 {
		usedTokens = estimateModelContextTokens(request.Instructions, input, request.Tools)
	}
	usableTokens := request.ContextWindowTokens * 95 / 100
	return max(0, usableTokens-usedTokens)
}

func modelRequestInputLimit(contextWindowTokens int, maxOutputTokens int) int {
	if contextWindowTokens <= 0 {
		return 0
	}
	limit := contextWindowTokens * 95 / 100
	if maxOutputTokens > 0 {
		limit = min(limit, contextWindowTokens-maxOutputTokens)
	}
	return max(0, limit)
}

func marshalModelContextItems(items []llm.ModelItem) ([]byte, error) {
	if len(items) == 0 {
		return nil, nil
	}
	return json.Marshal(cloneModelItems(items))
}

func unmarshalModelContextItems(data []byte) ([]llm.ModelItem, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}

	var items []llm.ModelItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return cloneModelItems(items), nil
}

func hasAssistantMessageItem(items []llm.ModelItem) bool {
	for _, item := range items {
		if item.Type == llm.ModelItemMessage && item.Role == domain.RoleAssistant {
			return true
		}
	}
	return false
}
