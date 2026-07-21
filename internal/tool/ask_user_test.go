package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestAskUserHandlerReturnsPendingPrompt(t *testing.T) {
	result, err := (AskUserHandler{}).Execute(t.Context(), ToolScope{}, ToolCall{
		CallID: "call-1",
		Arguments: json.RawMessage(`{
			"prompt":"请确认支付状态",
			"kind":"external_action",
			"options":[
				{"id":"paid","label":"已支付","tone":"primary"},
				{"id":"cancel","label":"取消","tone":"neutral"}
			],
			"action":{"label":"打开微信支付","url":"weixin://wap/pay?prepayid=example"}
		}`),
	})
	if err != nil {
		t.Fatalf("execute ask_user: %v", err)
	}
	if result.AwaitingInput == nil || result.AwaitingInput.CallID != "call-1" || len(result.AwaitingInput.Options) != 2 {
		t.Fatalf("pending prompt = %#v", result.AwaitingInput)
	}
}

func TestAskUserHandlerAcceptsStrictSingleChoiceArguments(t *testing.T) {
	result, err := (AskUserHandler{}).Execute(t.Context(), ToolScope{}, ToolCall{
		CallID: "call-1",
		Arguments: json.RawMessage(`{
			"prompt":"Choose one",
			"kind":"single_choice",
			"options":[
				{"id":"a","label":"A","tone":"primary"},
				{"id":"b","label":"B","tone":"neutral"}
			],
			"action":null
		}`),
	})
	if err != nil {
		t.Fatalf("execute strict single-choice ask_user: %v", err)
	}
	if result.AwaitingInput == nil || result.AwaitingInput.Action != nil {
		t.Fatalf("pending prompt = %#v", result.AwaitingInput)
	}
}

func TestAskUserDefinitionIsStrictSchemaCompatible(t *testing.T) {
	definition := askUserDefinition()
	if !definition.Strict {
		t.Fatal("ask_user definition must remain strict")
	}
	var schema struct {
		Properties           map[string]json.RawMessage `json:"properties"`
		Required             []string                   `json:"required"`
		AdditionalProperties bool                       `json:"additionalProperties"`
	}
	if err := json.Unmarshal(definition.Parameters, &schema); err != nil {
		t.Fatalf("decode ask_user schema: %v", err)
	}
	required := make(map[string]struct{}, len(schema.Required))
	for _, name := range schema.Required {
		required[name] = struct{}{}
	}
	for name := range schema.Properties {
		if _, ok := required[name]; !ok {
			t.Fatalf("strict ask_user property %q is not required", name)
		}
	}
	if schema.AdditionalProperties {
		t.Fatal("strict ask_user schema allows additional properties")
	}
	var action struct {
		Type []string `json:"type"`
	}
	if err := json.Unmarshal(schema.Properties["action"], &action); err != nil {
		t.Fatalf("decode ask_user action schema: %v", err)
	}
	if len(action.Type) != 2 || action.Type[0] != "object" || action.Type[1] != "null" {
		t.Fatalf("ask_user action type = %#v, want object or null", action.Type)
	}
}

func TestAskUserHandlerRestrictsExternalActionTargets(t *testing.T) {
	valid := []string{
		"https://pay.example.com/checkout",
		"weixin://wap/pay?prepayid=example",
	}
	invalid := []string{
		"https://localhost/pay",
		"https://127.0.0.1/pay",
		"https://127.1/pay",
		"https://2130706433/pay",
		"https://10.0.0.8/pay",
		"https://169.254.169.254/latest/meta-data",
		"https://[::1]/pay",
		"https://metadata.google.internal/computeMetadata/v1/",
		"weixin://pay/example",
		"weixin://dl/business/?ticket=example",
	}
	arguments := func(target string) json.RawMessage {
		payload, err := json.Marshal(map[string]any{
			"prompt": "Pay?",
			"kind":   AskUserKindExternalAction,
			"options": []map[string]string{
				{"id": "paid", "label": "Paid", "tone": AskUserTonePrimary},
				{"id": "cancel", "label": "Cancel", "tone": AskUserToneNeutral},
			},
			"action": map[string]string{"label": "Open payment", "url": target},
		})
		if err != nil {
			t.Fatal(err)
		}
		return payload
	}
	for _, target := range valid {
		if _, err := (AskUserHandler{}).Execute(t.Context(), ToolScope{}, ToolCall{Arguments: arguments(target)}); err != nil {
			t.Errorf("valid target %q rejected: %v", target, err)
		}
	}
	for _, target := range invalid {
		if _, err := (AskUserHandler{}).Execute(t.Context(), ToolScope{}, ToolCall{Arguments: arguments(target)}); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("unsafe target %q error = %v, want validation error", target, err)
		}
	}
}

func TestAskUserHandlerRejectsInvalidPrompt(t *testing.T) {
	tests := []json.RawMessage{
		json.RawMessage(`{"prompt":"x","kind":"single_choice","options":[{"id":"same","label":"A","tone":"neutral"},{"id":"same","label":"B","tone":"neutral"}]}`),
		json.RawMessage(`{"prompt":"x","kind":"external_action","options":[{"id":"a","label":"A","tone":"neutral"},{"id":"b","label":"B","tone":"neutral"}],"action":{"label":"open","url":"javascript:alert(1)"}}`),
		json.RawMessage(`{"prompt":"x","kind":"single_choice","options":[{"id":"a","label":"A","tone":"neutral"}]}`),
	}
	for _, arguments := range tests {
		_, err := (AskUserHandler{}).Execute(context.Background(), ToolScope{}, ToolCall{Arguments: arguments})
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("arguments %s error = %v, want validation error", arguments, err)
		}
	}
}

func TestDefaultToolsIncludesAskUser(t *testing.T) {
	for _, definition := range DefaultTools() {
		if definition.Name == AskUser {
			return
		}
	}
	t.Fatal("DefaultTools does not include ask_user")
}
