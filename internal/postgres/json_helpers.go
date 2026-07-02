package postgres

import (
	"encoding/json"
	"strings"
)

func marshalJSON(value any, fallback json.RawMessage) (json.RawMessage, error) {
	if value == nil {
		return cloneJSON(fallback), nil
	}

	switch typed := value.(type) {
	case json.RawMessage:
		return normalizedJSON(typed), nil
	case []byte:
		return normalizedJSON(typed), nil
	}

	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	return normalizedJSON(data), nil
}

func normalizedJSON(raw []byte) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	return cloneJSON([]byte(trimmed))
}

func cloneJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	cloned := make([]byte, len(raw))
	copy(cloned, raw)
	return json.RawMessage(cloned)
}

func nullableID(id string) any {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return id
}

func nullableText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func clampLimit(value int, fallback int, max int) int {
	if value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

func decodeMetadata(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
		return map[string]any{}
	}
	return decoded
}
