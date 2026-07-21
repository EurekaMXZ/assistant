package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/credential"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
)

const (
	parametersPurpose = "mcp-parameters"
	headersPurpose    = "mcp-headers"
)

type SecretInput struct {
	Name  string
	Value *string
}

type CreateServerInput struct {
	Name        string
	Slug        string
	EndpointURL string
	Enabled     bool
	Parameters  []SecretInput
	Headers     []SecretInput
}

type UpdateServerInput struct {
	Name         *string
	Slug         *string
	EndpointURL  *string
	Enabled      *bool
	Parameters   *[]SecretInput
	Headers      *[]SecretInput
	EnabledTools *[]string
}

type Service struct {
	Repository Repository
	Cipher     *credential.Cipher
	ToolLister ToolLister
}

func (s *Service) Create(ctx context.Context, ownerUserID string, input CreateServerInput) (*domain.UserMCPServer, error) {
	name, slug, err := validateServerFields(input.Name, input.Slug)
	if err != nil {
		return nil, err
	}
	endpointURL, err := ValidateEndpointURL(input.EndpointURL)
	if err != nil {
		return nil, err
	}
	parameters, err := validateSecretInputs(input.Parameters, false, true)
	if err != nil {
		return nil, err
	}
	headers, err := validateSecretInputs(input.Headers, true, true)
	if err != nil {
		return nil, err
	}
	id := uuid.NewString()
	server := domain.UserMCPServer{
		ID: id, OwnerUserID: ownerUserID, Name: name, Slug: slug, EndpointURL: endpointURL,
		Enabled: input.Enabled, Revision: 1, LastValidationStatus: domain.MCPValidationUntested,
	}
	server.EncryptedParameters, server.ParametersNonce, err = s.encryptSecrets(id, parametersPurpose, valuesFromInputs(parameters))
	if err != nil {
		return nil, err
	}
	server.EncryptedHeaders, server.HeadersNonce, err = s.encryptSecrets(id, headersPurpose, valuesFromInputs(headers))
	if err != nil {
		return nil, err
	}
	stored, err := s.Repository.Create(ctx, server)
	if err != nil {
		return nil, err
	}
	return s.decorate(stored, nil)
}

func (s *Service) List(ctx context.Context, ownerUserID string) ([]domain.UserMCPServer, error) {
	servers, err := s.Repository.List(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	for index := range servers {
		decorated, err := s.decorate(&servers[index], nil)
		if err != nil {
			return nil, err
		}
		servers[index] = *decorated
	}
	return servers, nil
}

func (s *Service) Get(ctx context.Context, ownerUserID string, serverID string) (*domain.UserMCPServer, error) {
	if !validUUID(serverID) {
		return nil, domain.ErrNotFound
	}
	server, err := s.Repository.Get(ctx, ownerUserID, serverID)
	if err != nil {
		return nil, err
	}
	tools, err := s.Repository.ListTools(ctx, ownerUserID, serverID)
	if err != nil {
		return nil, err
	}
	return s.decorate(server, tools)
}

func (s *Service) Update(ctx context.Context, ownerUserID string, serverID string, input UpdateServerInput) (*domain.UserMCPServer, error) {
	if !validUUID(serverID) {
		return nil, domain.ErrNotFound
	}
	current, err := s.Repository.Get(ctx, ownerUserID, serverID)
	if err != nil {
		return nil, err
	}
	updated := *current
	if input.Name != nil || input.Slug != nil {
		name, slug := updated.Name, updated.Slug
		if input.Name != nil {
			name = *input.Name
		}
		if input.Slug != nil {
			slug = *input.Slug
		}
		updated.Name, updated.Slug, err = validateServerFields(name, slug)
		if err != nil {
			return nil, err
		}
	}
	if input.EndpointURL != nil {
		updated.EndpointURL, err = ValidateEndpointURL(*input.EndpointURL)
		if err != nil {
			return nil, err
		}
		resetValidation(&updated)
	}
	if input.Enabled != nil {
		updated.Enabled = *input.Enabled
	}
	if input.Parameters != nil {
		updated.EncryptedParameters, updated.ParametersNonce, err = s.mergeAndEncrypt(current, parametersPurpose, *input.Parameters, false)
		if err != nil {
			return nil, err
		}
		resetValidation(&updated)
	}
	if input.Headers != nil {
		updated.EncryptedHeaders, updated.HeadersNonce, err = s.mergeAndEncrypt(current, headersPurpose, *input.Headers, true)
		if err != nil {
			return nil, err
		}
		resetValidation(&updated)
	}
	if input.EnabledTools != nil {
		if err := validateToolNames(*input.EnabledTools); err != nil {
			return nil, err
		}
	}
	stored, err := s.Repository.Update(ctx, ownerUserID, updated, current.Revision, input.EnabledTools)
	if err != nil {
		return nil, err
	}
	tools, err := s.Repository.ListTools(ctx, ownerUserID, serverID)
	if err != nil {
		return nil, err
	}
	return s.decorate(stored, tools)
}

func (s *Service) Delete(ctx context.Context, ownerUserID string, serverID string) error {
	if !validUUID(serverID) {
		return domain.ErrNotFound
	}
	return s.Repository.Delete(ctx, ownerUserID, serverID)
}

func (s *Service) Test(ctx context.Context, ownerUserID string, serverID string) (*domain.UserMCPServer, error) {
	if !validUUID(serverID) {
		return nil, domain.ErrNotFound
	}
	server, err := s.Repository.Get(ctx, ownerUserID, serverID)
	if err != nil {
		return nil, err
	}
	parameters, err := s.decryptSecrets(server, parametersPurpose)
	if err != nil {
		return nil, err
	}
	headers, err := s.decryptSecrets(server, headersPurpose)
	if err != nil {
		return nil, err
	}
	testCtx, cancel := context.WithTimeout(ctx, connectionTimeout)
	defer cancel()
	tools, validationErr := s.ToolLister.ListTools(testCtx, server.EndpointURL, parameters, headers)
	status := domain.MCPValidationValid
	message := ""
	if validationErr != nil {
		status = domain.MCPValidationInvalid
		switch {
		case errors.Is(validationErr, errMCPConnect):
			message = errMCPConnect.Error()
		case errors.Is(validationErr, errMCPToolsList):
			message = errMCPToolsList.Error()
		default:
			message = "MCP server validation failed"
		}
		tools = nil
	}
	stored, err := s.Repository.RecordValidation(ctx, ownerUserID, serverID, status, message, tools)
	if err != nil {
		return nil, err
	}
	storedTools, err := s.Repository.ListTools(ctx, ownerUserID, serverID)
	if err != nil {
		return nil, err
	}
	return s.decorate(stored, storedTools)
}

func (s *Service) mergeAndEncrypt(server *domain.UserMCPServer, purpose string, inputs []SecretInput, header bool) ([]byte, []byte, error) {
	normalized, err := validateSecretInputs(inputs, header, false)
	if err != nil {
		return nil, nil, err
	}
	existing, err := s.decryptSecrets(server, purpose)
	if err != nil {
		return nil, nil, err
	}
	merged := make(map[string]string, len(normalized))
	for _, input := range normalized {
		if input.Value != nil {
			merged[input.Name] = *input.Value
			continue
		}
		value, ok := lookupSecret(existing, input.Name, header)
		if !ok {
			return nil, nil, domain.NewValidationError("secret value is required for a new entry")
		}
		merged[input.Name] = value
	}
	return s.encryptSecrets(server.ID, purpose, merged)
}

func (s *Service) encryptSecrets(serverID string, purpose string, values map[string]string) ([]byte, []byte, error) {
	payload, err := json.Marshal(values)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal MCP secrets: %w", err)
	}
	ciphertext, nonce, err := s.Cipher.Encrypt(serverID, purpose, string(payload))
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt MCP secrets: %w", err)
	}
	return ciphertext, nonce, nil
}

func (s *Service) decryptSecrets(server *domain.UserMCPServer, purpose string) (map[string]string, error) {
	ciphertext, nonce := server.EncryptedParameters, server.ParametersNonce
	if purpose == headersPurpose {
		ciphertext, nonce = server.EncryptedHeaders, server.HeadersNonce
	}
	payload, err := s.Cipher.Decrypt(server.ID, purpose, ciphertext, nonce)
	if err != nil {
		return nil, fmt.Errorf("decrypt MCP secrets: %w", err)
	}
	values := map[string]string{}
	if err := json.Unmarshal([]byte(payload), &values); err != nil {
		return nil, errors.New("decode MCP secrets")
	}
	return values, nil
}

func (s *Service) decorate(server *domain.UserMCPServer, tools []domain.UserMCPTool) (*domain.UserMCPServer, error) {
	parameters, err := s.decryptSecrets(server, parametersPurpose)
	if err != nil {
		return nil, err
	}
	headers, err := s.decryptSecrets(server, headersPurpose)
	if err != nil {
		return nil, err
	}
	result := *server
	result.Parameters = secretMetadata(parameters)
	result.Headers = secretMetadata(headers)
	result.Tools = tools
	result.EncryptedParameters = nil
	result.ParametersNonce = nil
	result.EncryptedHeaders = nil
	result.HeadersNonce = nil
	if result.Parameters == nil {
		result.Parameters = []domain.MCPSecret{}
	}
	if result.Headers == nil {
		result.Headers = []domain.MCPSecret{}
	}
	if result.Tools == nil {
		result.Tools = []domain.UserMCPTool{}
	}
	return &result, nil
}

func valuesFromInputs(inputs []SecretInput) map[string]string {
	values := make(map[string]string, len(inputs))
	for _, input := range inputs {
		values[input.Name] = *input.Value
	}
	return values
}

func secretMetadata(values map[string]string) []domain.MCPSecret {
	secrets := make([]domain.MCPSecret, 0, len(values))
	for name, value := range values {
		secrets = append(secrets, domain.MCPSecret{Name: name, Configured: true, KeyHint: credential.KeyHint(value)})
	}
	sort.Slice(secrets, func(i int, j int) bool { return secrets[i].Name < secrets[j].Name })
	return secrets
}

func lookupSecret(values map[string]string, name string, header bool) (string, bool) {
	if !header {
		value, ok := values[name]
		return value, ok
	}
	for existingName, value := range values {
		if strings.EqualFold(existingName, name) {
			return value, true
		}
	}
	return "", false
}

func validateToolNames(names []string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if strings.TrimSpace(name) != name || name == "" || len(name) > 255 || containsControl(name) {
			return domain.NewValidationError("enabled_tools contains an invalid tool name")
		}
		if _, duplicate := seen[name]; duplicate {
			return domain.NewValidationError("enabled_tools contains duplicates")
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validUUID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}

func resetValidation(server *domain.UserMCPServer) {
	server.LastValidationStatus = domain.MCPValidationUntested
	server.LastValidationError = ""
	server.LastValidatedAt = nil
}
