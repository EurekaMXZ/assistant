package workflow

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

const turnModelContextContentType = "application/json"

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
