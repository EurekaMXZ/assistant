package server

import (
	"net/http"

	"github.com/EurekaMXZ/assistant/internal/mcpconfig"
	"github.com/gin-gonic/gin"
)

type mcpSecretRequest struct {
	Name  string  `json:"name"`
	Value *string `json:"value"`
}

func (a *API) handleListMCPServers(c *gin.Context) {
	servers, err := a.useCases.MCP.ListServers(c.Request.Context(), currentUser(c).ID)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"servers": servers})
}

func (a *API) handleCreateMCPServer(c *gin.Context) {
	var request struct {
		Name        string             `json:"name"`
		Slug        string             `json:"slug"`
		EndpointURL string             `json:"endpoint_url"`
		Enabled     *bool              `json:"enabled"`
		Parameters  []mcpSecretRequest `json:"parameters"`
		Headers     []mcpSecretRequest `json:"headers"`
	}
	if err := bindStrictJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	server, err := a.useCases.MCP.CreateServer(c.Request.Context(), currentUser(c).ID, mcpconfig.CreateServerInput{
		Name: request.Name, Slug: request.Slug, EndpointURL: request.EndpointURL, Enabled: enabled,
		Parameters: mcpSecretInputs(request.Parameters), Headers: mcpSecretInputs(request.Headers),
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"server": server})
}

func (a *API) handleGetMCPServer(c *gin.Context) {
	server, err := a.useCases.MCP.GetServer(c.Request.Context(), currentUser(c).ID, c.Param("serverID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"server": server})
}

func (a *API) handleUpdateMCPServer(c *gin.Context) {
	var request struct {
		Name         *string             `json:"name"`
		Slug         *string             `json:"slug"`
		EndpointURL  *string             `json:"endpoint_url"`
		Enabled      *bool               `json:"enabled"`
		Parameters   *[]mcpSecretRequest `json:"parameters"`
		Headers      *[]mcpSecretRequest `json:"headers"`
		EnabledTools *[]string           `json:"enabled_tools"`
	}
	if err := bindStrictJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	server, err := a.useCases.MCP.UpdateServer(c.Request.Context(), currentUser(c).ID, c.Param("serverID"), mcpconfig.UpdateServerInput{
		Name: request.Name, Slug: request.Slug, EndpointURL: request.EndpointURL, Enabled: request.Enabled,
		Parameters: optionalMCPSecretInputs(request.Parameters), Headers: optionalMCPSecretInputs(request.Headers),
		EnabledTools: request.EnabledTools,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"server": server})
}

func (a *API) handleDeleteMCPServer(c *gin.Context) {
	if err := a.useCases.MCP.DeleteServer(c.Request.Context(), currentUser(c).ID, c.Param("serverID")); err != nil {
		writeAPIError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *API) handleTestMCPServer(c *gin.Context) {
	server, err := a.useCases.MCP.TestServer(c.Request.Context(), currentUser(c).ID, c.Param("serverID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"server": server})
}

func mcpSecretInputs(requests []mcpSecretRequest) []mcpconfig.SecretInput {
	inputs := make([]mcpconfig.SecretInput, 0, len(requests))
	for _, request := range requests {
		inputs = append(inputs, mcpconfig.SecretInput{Name: request.Name, Value: request.Value})
	}
	return inputs
}

func optionalMCPSecretInputs(requests *[]mcpSecretRequest) *[]mcpconfig.SecretInput {
	if requests == nil {
		return nil
	}
	inputs := mcpSecretInputs(*requests)
	return &inputs
}
