package bootstrap

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestNormalizeReasoningEfforts(t *testing.T) {
	tests := []struct {
		name     string
		values   []string
		expected []string
		wantErr  bool
	}{
		{name: "disabled", expected: []string{}},
		{name: "canonical order and duplicates", values: []string{" xhigh ", "LOW", "low"}, expected: []string{"low", "xhigh"}},
		{name: "invalid", values: []string{"minimal"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := normalizeReasoningEfforts(test.values)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeReasoningEfforts() error = %v, wantErr %v", err, test.wantErr)
			}
			if !test.wantErr && !reflect.DeepEqual(actual, test.expected) {
				t.Fatalf("normalizeReasoningEfforts() = %#v, want %#v", actual, test.expected)
			}
		})
	}
}

func TestNormalizeProviderBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "trim and trailing slash", value: "  HTTPS://api.example.com/v1/  ", want: "https://api.example.com/v1"},
		{name: "http", value: "http://localhost:8080/v1", want: "http://localhost:8080/v1"},
		{name: "relative", value: "/v1", wantErr: true},
		{name: "unsupported scheme", value: "ftp://api.example.com/v1", wantErr: true},
		{name: "credentials", value: "https://user:secret@api.example.com/v1", wantErr: true},
		{name: "query", value: "https://api.example.com/v1?key=value", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeProviderBaseURL(test.value)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeProviderBaseURL() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("normalizeProviderBaseURL() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestApplyRequestedReasoningEffort(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		snapshot  domain.ModelExecutionSnapshot
		want      string
		wantErr   bool
	}{
		{name: "default", snapshot: domain.ModelExecutionSnapshot{SupportedReasoningEfforts: []string{"high"}}},
		{name: "supported", requested: " HIGH ", snapshot: domain.ModelExecutionSnapshot{SupportedReasoningEfforts: []string{"low", "high"}}, want: "high"},
		{name: "unsupported effort", requested: "medium", snapshot: domain.ModelExecutionSnapshot{SupportedReasoningEfforts: []string{"low", "high"}}, wantErr: true},
		{name: "reasoning disabled", requested: "low", snapshot: domain.ModelExecutionSnapshot{}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := applyRequestedReasoningEffort(&test.snapshot, test.requested)
			if (err != nil) != test.wantErr {
				t.Fatalf("applyRequestedReasoningEffort() error = %v, wantErr %v", err, test.wantErr)
			}
			if test.snapshot.ReasoningEffort != test.want {
				t.Fatalf("reasoning effort = %q, want %q", test.snapshot.ReasoningEffort, test.want)
			}
		})
	}
}

func TestValidateModelDefaultParameters(t *testing.T) {
	tests := []struct {
		name    string
		raw     json.RawMessage
		efforts []string
		wantErr bool
	}{
		{name: "empty"},
		{name: "no reasoning default", raw: json.RawMessage(`{"text_verbosity":"low"}`)},
		{name: "supported", raw: json.RawMessage(`{"reasoning_effort":"high"}`), efforts: []string{"low", "high"}},
		{name: "unsupported", raw: json.RawMessage(`{"reasoning_effort":"medium"}`), efforts: []string{"low", "high"}, wantErr: true},
		{name: "wrong type", raw: json.RawMessage(`{"reasoning_effort":true}`), efforts: []string{"low"}, wantErr: true},
		{name: "not object", raw: json.RawMessage(`[]`), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateModelDefaultParameters(test.raw, test.efforts)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateModelDefaultParameters() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
