package mcpconfig

import (
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"golang.org/x/net/http/httpguts"
)

const (
	MaxSecretEntries = 32
	MaxSecretName    = 128
	MaxSecretValue   = 8192
)

var (
	slugPattern      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	parameterPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
	managedHeaders   = map[string]struct{}{
		"accept": {}, "connection": {}, "content-length": {}, "content-type": {},
		"host": {}, "keep-alive": {}, "mcp-protocol-version": {}, "mcp-session-id": {},
		"proxy-authorization": {}, "proxy-connection": {}, "te": {}, "trailer": {},
		"transfer-encoding": {}, "upgrade": {},
	}
)

func ValidateEndpointURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > 2048 {
		return "", domain.NewValidationError("endpoint_url is invalid")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", domain.NewValidationError("endpoint_url is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", domain.NewValidationError("endpoint_url must use http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", domain.NewValidationError("endpoint_url must not contain userinfo, query, or fragment")
	}
	if err := validateURLHost(parsed); err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func validateURLHost(parsed *url.URL) error {
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || isMetadataHostname(host) {
		return domain.NewValidationError("endpoint_url host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil && !isPublicIP(ip) {
		return domain.NewValidationError("endpoint_url host is not allowed")
	}
	return nil
}

func isMetadataHostname(host string) bool {
	switch host {
	case "metadata", "metadata.google.internal", "metadata.google", "instance-data", "instance-data.ec2.internal":
		return true
	default:
		return strings.HasSuffix(host, ".internal") && strings.Contains(host, "metadata")
	}
}

func validateServerFields(name string, slug string) (string, string, error) {
	name = strings.TrimSpace(name)
	slug = strings.TrimSpace(slug)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 100 {
		return "", "", domain.NewValidationError("name is invalid")
	}
	if len(slug) > 64 || !slugPattern.MatchString(slug) {
		return "", "", domain.NewValidationError("slug must contain lowercase letters, numbers, and single hyphens")
	}
	return name, slug, nil
}

func validateSecretInputs(inputs []SecretInput, header bool, requireValues bool) ([]SecretInput, error) {
	if len(inputs) > MaxSecretEntries {
		return nil, domain.NewValidationError("too many secret entries")
	}
	seen := make(map[string]struct{}, len(inputs))
	normalized := make([]SecretInput, 0, len(inputs))
	for _, input := range inputs {
		name := strings.TrimSpace(input.Name)
		if name == "" || len(name) > MaxSecretName || !utf8.ValidString(name) {
			return nil, domain.NewValidationError("secret name is invalid")
		}
		lookupName := name
		if header {
			if !httpguts.ValidHeaderFieldName(name) {
				return nil, domain.NewValidationError("header name is invalid")
			}
			lookupName = strings.ToLower(name)
			if _, blocked := managedHeaders[lookupName]; blocked {
				return nil, domain.NewValidationError("header is managed by the MCP transport")
			}
			name = http.CanonicalHeaderKey(name)
		} else if !parameterPattern.MatchString(name) {
			return nil, domain.NewValidationError("parameter name is invalid")
		}
		if _, exists := seen[lookupName]; exists {
			return nil, domain.NewValidationError("secret names must be unique")
		}
		seen[lookupName] = struct{}{}
		if requireValues && input.Value == nil {
			return nil, domain.NewValidationError("secret value is required")
		}
		if input.Value != nil {
			if len(*input.Value) > MaxSecretValue || !utf8.ValidString(*input.Value) {
				return nil, domain.NewValidationError("secret value is invalid")
			}
			if header && !httpguts.ValidHeaderFieldValue(*input.Value) {
				return nil, domain.NewValidationError("header value is invalid")
			}
		}
		normalized = append(normalized, SecretInput{Name: name, Value: input.Value})
	}
	return normalized, nil
}

func containsControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}
