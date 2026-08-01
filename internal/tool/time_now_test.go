package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/llm"
)

func TestTimeNowHandlerReturnsCurrentTimeInUTCByDefault(t *testing.T) {
	current := time.Date(2026, time.July, 31, 12, 34, 56, 123000000, time.UTC)
	handler := TimeNowHandler{Now: func() time.Time { return current }}

	result, err := handler.Execute(t.Context(), ToolScope{}, ToolCall{
		CallID:    "call-1",
		Arguments: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("execute time.now: %v", err)
	}

	var output struct {
		Timezone    string `json:"timezone"`
		ISO8601     string `json:"iso8601"`
		UnixSeconds int64  `json:"unix_seconds"`
	}
	if err := json.Unmarshal([]byte(result.OutputItem.Output), &output); err != nil {
		t.Fatalf("decode time.now output: %v", err)
	}
	if output.Timezone != "UTC" || output.ISO8601 != "2026-07-31T12:34:56.123Z" || output.UnixSeconds != current.Unix() {
		t.Fatalf("unexpected UTC output: %#v", output)
	}
}

func TestTimeNowHandlerAppliesRequestedTimezone(t *testing.T) {
	current := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	handler := TimeNowHandler{Now: func() time.Time { return current }}

	result, err := handler.Execute(t.Context(), ToolScope{}, ToolCall{
		CallID:    "call-1",
		Arguments: json.RawMessage(`{"timezone":"Asia/Shanghai"}`),
	})
	if err != nil {
		t.Fatalf("execute time.now with timezone: %v", err)
	}

	var output struct {
		Timezone    string `json:"timezone"`
		ISO8601     string `json:"iso8601"`
		UnixSeconds int64  `json:"unix_seconds"`
	}
	if err := json.Unmarshal([]byte(result.OutputItem.Output), &output); err != nil {
		t.Fatalf("decode time.now output: %v", err)
	}
	if output.Timezone != "Asia/Shanghai" || output.ISO8601 != "2026-01-02T11:04:05+08:00" || output.UnixSeconds != current.Unix() {
		t.Fatalf("unexpected localized output: %#v", output)
	}
}

func TestTimeNowHandlerRejectsInvalidTimezone(t *testing.T) {
	_, err := (TimeNowHandler{}).Execute(t.Context(), ToolScope{}, ToolCall{
		Arguments: json.RawMessage(`{"timezone":"not/a-timezone"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "time.now timezone") {
		t.Fatalf("invalid timezone error = %v", err)
	}
}

func TestTimeNowIsRegisteredInDefaultToolsAndLocalExecutor(t *testing.T) {
	var found bool
	for _, definition := range DefaultTools() {
		if definition.Type != llm.ModelToolTypeNamespace || definition.Name != timeNamespace {
			continue
		}
		found = true
		if len(definition.Tools) != 1 || definition.Tools[0].Name != timeNowName {
			t.Fatalf("unexpected time namespace: %#v", definition)
		}
	}
	if !found {
		t.Fatal("DefaultTools does not include the time namespace")
	}

	executor, err := NewLocalExecutor(TimeNowHandler{Now: func() time.Time {
		return time.Unix(0, 0).UTC()
	}})
	if err != nil {
		t.Fatalf("new local executor: %v", err)
	}
	result, err := executor.Execute(context.Background(), ToolScope{}, ToolCall{
		CallID:    "call-1",
		Namespace: timeNamespace,
		Name:      timeNowName,
		Arguments: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("execute registered time.now: %v", err)
	}
	if result.OutputItem.Type != llm.ModelItemFunctionCallOutput || result.OutputItem.CallID != "call-1" {
		t.Fatalf("unexpected execution result: %#v", result.OutputItem)
	}
}

func TestTimeNowDefinitionUsesSchema(t *testing.T) {
	definition := timeNowDefinition()
	if definition.Type != llm.ModelToolTypeFunction || definition.Name != timeNowName {
		t.Fatalf("unexpected time.now definition: %#v", definition)
	}

	var schema struct {
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties bool                       `json:"additionalProperties"`
	}
	if err := json.Unmarshal(definition.Parameters, &schema); err != nil {
		t.Fatalf("decode time.now schema: %v", err)
	}
	if _, ok := schema.Properties["timezone"]; !ok || schema.AdditionalProperties {
		t.Fatalf("unexpected time.now schema: %#v", schema)
	}
}
