package mcpconfig

import (
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestValidateEndpointURL(t *testing.T) {
	for _, raw := range []string{
		"https://mcp.example.com/v1/mcp",
		"http://mcp.example.com:8080/mcp",
	} {
		if _, err := ValidateEndpointURL(raw); err != nil {
			t.Errorf("ValidateEndpointURL(%q) returned %v", raw, err)
		}
	}

	for _, raw := range []string{
		"ftp://mcp.example.com/mcp",
		"https://user:secret@mcp.example.com/mcp",
		"https://mcp.example.com/mcp?token=secret",
		"https://mcp.example.com/mcp#fragment",
		"http://localhost:8080/mcp",
		"http://service.localhost/mcp",
		"http://127.0.0.1/mcp",
		"http://10.0.0.1/mcp",
		"http://169.254.169.254/latest/meta-data",
		"http://metadata.google.internal/computeMetadata/v1",
	} {
		if _, err := ValidateEndpointURL(raw); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("ValidateEndpointURL(%q) error = %v, want invalid input", raw, err)
		}
	}
}

func TestValidateSecretInputsHeaders(t *testing.T) {
	authorization := "Bearer secret"
	xKey := "value"
	inputs, err := validateSecretInputs([]SecretInput{
		{Name: "authorization", Value: &authorization},
		{Name: "x-api-key", Value: &xKey},
	}, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if inputs[0].Name != "Authorization" || inputs[1].Name != "X-Api-Key" {
		t.Fatalf("canonical headers = %#v", inputs)
	}

	for _, name := range []string{"Host", "Content-Length", "Connection", "Transfer-Encoding", "Accept", "Content-Type", "MCP-Session-Id", "MCP-Protocol-Version"} {
		value := "blocked"
		if _, err := validateSecretInputs([]SecretInput{{Name: name, Value: &value}}, true, true); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("header %q error = %v, want invalid input", name, err)
		}
	}

	newline := "secret\r\ninjected: true"
	if _, err := validateSecretInputs([]SecretInput{{Name: "X-Key", Value: &newline}}, true, true); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("newline header error = %v, want invalid input", err)
	}
	tooLong := strings.Repeat("x", MaxSecretValue+1)
	if _, err := validateSecretInputs([]SecretInput{{Name: "X-Key", Value: &tooLong}}, true, true); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("long header error = %v, want invalid input", err)
	}
	many := make([]SecretInput, MaxSecretEntries+1)
	for index := range many {
		name := "X-Key-" + strings.Repeat("x", index)
		many[index] = SecretInput{Name: name, Value: &xKey}
	}
	if _, err := validateSecretInputs(many, true, true); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("too many headers error = %v, want invalid input", err)
	}
}

func TestValidateSecretInputsParameters(t *testing.T) {
	value := "secret"
	if _, err := validateSecretInputs([]SecretInput{{Name: "api-key.v1", Value: &value}}, false, true); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bad key", "bad&key", "bad=key", ""} {
		if _, err := validateSecretInputs([]SecretInput{{Name: name, Value: &value}}, false, true); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("parameter %q error = %v, want invalid input", name, err)
		}
	}
}

func TestIsPublicIP(t *testing.T) {
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !isPublicIP(net.ParseIP(raw)) {
			t.Errorf("isPublicIP(%q) = false, want true", raw)
		}
	}
	for _, raw := range []string{
		"0.0.0.0", "10.0.0.1", "100.100.100.200", "127.0.0.1", "169.254.169.254",
		"172.16.0.1", "192.168.1.1", "224.0.0.1", "::", "::1", "fc00::1", "fe80::1",
	} {
		if isPublicIP(net.ParseIP(raw)) {
			t.Errorf("isPublicIP(%q) = true, want false", raw)
		}
	}
}
