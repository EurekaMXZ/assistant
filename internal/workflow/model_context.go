package workflow

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

const turnModelContextContentType = "application/json"

const (
	modelContextSafetyMultiplierNumerator   = 140
	modelContextSafetyMultiplierDenominator = 100
	modelContextSafetyOverheadTokens        = 256
)

func buildModelContextItems(initialInput []llm.ModelItem, currentInput []llm.ModelItem, final *llm.ModelResult) []llm.ModelItem {
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
	return items
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

func estimateSafeModelContextTokens(instructions string, items []llm.ModelItem, tools []llm.ModelTool) int {
	base := estimateModelContextTokens(instructions, items, tools)
	if base <= 0 {
		return 0
	}
	return (base*modelContextSafetyMultiplierNumerator+modelContextSafetyMultiplierDenominator-1)/modelContextSafetyMultiplierDenominator + modelContextSafetyOverheadTokens
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

func compactTriggerTokenLimit(configured int, contextWindow int) int {
	automatic := contextWindow * 8 / 10
	if automatic <= 0 {
		return max(0, configured)
	}
	if configured <= 0 || configured > automatic {
		return automatic
	}
	return configured
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
