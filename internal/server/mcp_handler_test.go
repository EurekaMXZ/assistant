package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/mcpconfig"
)

func TestCreateMCPServerUsesAuthenticatedOwnerAndDoesNotEchoSecrets(t *testing.T) {
	const ownerID = "8cf49f98-f07c-4caf-bf37-8d49bb3d6d6e"
	server := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: ownerID, Role: domain.UserRoleUser}, nil
		}},
		MCP: MCPUseCases{CreateServer: func(_ context.Context, actualOwnerID string, input mcpconfig.CreateServerInput) (*domain.UserMCPServer, error) {
			if actualOwnerID != ownerID {
				t.Fatalf("owner ID = %q, want %q", actualOwnerID, ownerID)
			}
			if len(input.Parameters) != 1 || input.Parameters[0].Value == nil || *input.Parameters[0].Value != "query-secret" {
				t.Fatalf("parameters = %#v", input.Parameters)
			}
			if len(input.Headers) != 1 || input.Headers[0].Value == nil || *input.Headers[0].Value != "header-secret" {
				t.Fatalf("headers = %#v", input.Headers)
			}
			return &domain.UserMCPServer{
				ID: "83906332-a121-4d89-bf3b-c7a147fb13d4", Name: input.Name, Slug: input.Slug,
				EndpointURL: input.EndpointURL, Enabled: input.Enabled, Revision: 1,
				Parameters: []domain.MCPSecret{{Name: "api_key", Configured: true, KeyHint: "...cret"}},
				Headers:    []domain.MCPSecret{{Name: "Authorization", Configured: true, KeyHint: "...cret"}},
				Tools:      []domain.UserMCPTool{},
			}, nil
		}},
	})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/mcp-servers", strings.NewReader(`{
		"name":"Example","slug":"example","endpoint_url":"https://mcp.example.com/mcp",
		"parameters":[{"name":"api_key","value":"query-secret"}],
		"headers":[{"name":"Authorization","value":"header-secret"}]
	}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "query-secret") || strings.Contains(response.Body.String(), "header-secret") {
		t.Fatalf("response leaked a secret: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"configured":true`) {
		t.Fatalf("response missing configured metadata: %s", response.Body.String())
	}
}
