package mcpconfig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/credential"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
)

type memoryRepository struct {
	server          domain.UserMCPServer
	updated         domain.UserMCPServer
	validationError string
}

func (r *memoryRepository) Create(context.Context, domain.UserMCPServer) (*domain.UserMCPServer, error) {
	panic("unexpected Create")
}

func (r *memoryRepository) List(context.Context, string) ([]domain.UserMCPServer, error) {
	panic("unexpected List")
}

func (r *memoryRepository) Get(context.Context, string, string) (*domain.UserMCPServer, error) {
	server := r.server
	return &server, nil
}

func (r *memoryRepository) Update(_ context.Context, _ string, server domain.UserMCPServer, _ int64, _ *[]string) (*domain.UserMCPServer, error) {
	server.Revision++
	r.updated = server
	return &server, nil
}

func (r *memoryRepository) Delete(context.Context, string, string) error {
	panic("unexpected Delete")
}

func (r *memoryRepository) RecordValidation(_ context.Context, _ string, _ string, status string, validationError string, _ []domain.UserMCPTool) (*domain.UserMCPServer, error) {
	r.validationError = validationError
	server := r.server
	server.LastValidationStatus = status
	server.LastValidationError = validationError
	return &server, nil
}

type failingToolLister struct{}

func (failingToolLister) ListTools(context.Context, string, map[string]string, map[string]string) ([]domain.UserMCPTool, error) {
	return nil, errors.New("request https://mcp.example.com/mcp?token=secret-token failed with Authorization: secret-header")
}

func (r *memoryRepository) ListTools(context.Context, string, string) ([]domain.UserMCPTool, error) {
	return []domain.UserMCPTool{}, nil
}

func TestUpdatePreservesConfiguredSecretWithoutValueAndDeletesMissingRows(t *testing.T) {
	cipher, err := credential.NewCipher(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	parameters, parametersNonce, err := cipher.Encrypt(id, parametersPurpose, `{"api_key":"original-secret","removed":"old-secret"}`)
	if err != nil {
		t.Fatal(err)
	}
	headers, headersNonce, err := cipher.Encrypt(id, headersPurpose, `{"Authorization":"Bearer original-header"}`)
	if err != nil {
		t.Fatal(err)
	}
	repository := &memoryRepository{server: domain.UserMCPServer{
		ID: id, OwnerUserID: uuid.NewString(), Name: "Server", Slug: "server", EndpointURL: "https://mcp.example.com/mcp",
		Enabled: true, Revision: 4, EncryptedParameters: parameters, ParametersNonce: parametersNonce,
		EncryptedHeaders: headers, HeadersNonce: headersNonce, LastValidationStatus: domain.MCPValidationUntested,
	}}
	service := &Service{Repository: repository, Cipher: cipher}
	newValue := "new-secret"
	parameterPatch := []SecretInput{{Name: "api_key"}, {Name: "new_key", Value: &newValue}}
	headerPatch := []SecretInput{{Name: "authorization"}}
	result, err := service.Update(t.Context(), repository.server.OwnerUserID, id, UpdateServerInput{
		Parameters: &parameterPatch,
		Headers:    &headerPatch,
	})
	if err != nil {
		t.Fatal(err)
	}

	parameterJSON, err := cipher.Decrypt(id, parametersPurpose, repository.updated.EncryptedParameters, repository.updated.ParametersNonce)
	if err != nil {
		t.Fatal(err)
	}
	var parameterValues map[string]string
	if err := json.Unmarshal([]byte(parameterJSON), &parameterValues); err != nil {
		t.Fatal(err)
	}
	if parameterValues["api_key"] != "original-secret" || parameterValues["new_key"] != "new-secret" {
		t.Fatalf("parameters = %#v", parameterValues)
	}
	if _, exists := parameterValues["removed"]; exists {
		t.Fatalf("removed parameter was preserved: %#v", parameterValues)
	}
	headerJSON, err := cipher.Decrypt(id, headersPurpose, repository.updated.EncryptedHeaders, repository.updated.HeadersNonce)
	if err != nil {
		t.Fatal(err)
	}
	if headerJSON != `{"Authorization":"Bearer original-header"}` {
		t.Fatalf("headers = %s", headerJSON)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"original-secret", "new-secret", "original-header"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("response leaked secret %q: %s", secret, encoded)
		}
	}
	if len(result.Parameters) != 2 || !result.Parameters[0].Configured || len(result.Headers) != 1 || !result.Headers[0].Configured {
		t.Fatalf("secret metadata = parameters %#v headers %#v", result.Parameters, result.Headers)
	}
}

func TestConnectionTestStoresSanitizedError(t *testing.T) {
	cipher, err := credential.NewCipher(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	parameters, parametersNonce, err := cipher.Encrypt(id, parametersPurpose, `{"token":"secret-token"}`)
	if err != nil {
		t.Fatal(err)
	}
	headers, headersNonce, err := cipher.Encrypt(id, headersPurpose, `{"Authorization":"secret-header"}`)
	if err != nil {
		t.Fatal(err)
	}
	repository := &memoryRepository{server: domain.UserMCPServer{
		ID: id, OwnerUserID: uuid.NewString(), Name: "Server", Slug: "server", EndpointURL: "https://mcp.example.com/mcp",
		Enabled: true, Revision: 1, EncryptedParameters: parameters, ParametersNonce: parametersNonce,
		EncryptedHeaders: headers, HeadersNonce: headersNonce, LastValidationStatus: domain.MCPValidationUntested,
	}}
	service := &Service{Repository: repository, Cipher: cipher, ToolLister: failingToolLister{}}
	result, err := service.Test(t.Context(), repository.server.OwnerUserID, id)
	if err != nil {
		t.Fatal(err)
	}
	if result.LastValidationStatus != domain.MCPValidationInvalid || repository.validationError != "MCP server validation failed" {
		t.Fatalf("validation status=%q error=%q", result.LastValidationStatus, repository.validationError)
	}
	if strings.Contains(repository.validationError, "secret") || strings.Contains(repository.validationError, "token=") {
		t.Fatalf("validation error leaked credentials: %q", repository.validationError)
	}
}
