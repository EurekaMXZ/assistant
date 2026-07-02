package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/gin-gonic/gin"
)

func pagePayload(items any, nextCursor string) gin.H {
	return gin.H{"data": items, "page": domain.CursorPage{NextCursor: nextCursor, HasMore: nextCursor != ""}}
}

func (a *API) handleListModels(c *gin.Context) {
	result, err := a.useCases.Models.ListModels(c.Request.Context(), parseLimit(c, 50, 200), strings.TrimSpace(c.Query("cursor")))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, pagePayload(result.Items, result.NextCursor))
}

func (a *API) handleGetModel(c *gin.Context) {
	model, err := a.useCases.Models.GetModel(c.Request.Context(), c.Param("modelID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"model": model})
}

func (a *API) handleListAdminModels(c *gin.Context) {
	result, err := a.useCases.Models.ListAdminModels(c.Request.Context(), currentUser(c), parseLimit(c, 50, 200), strings.TrimSpace(c.Query("cursor")))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, pagePayload(result.Items, result.NextCursor))
}

func (a *API) handleCreateModel(c *gin.Context) {
	var request struct {
		Provider                  string          `json:"provider"`
		CredentialID              string          `json:"credential_id"`
		Slug                      string          `json:"slug"`
		UpstreamModel             string          `json:"upstream_model"`
		DisplayName               string          `json:"display_name"`
		Description               string          `json:"description"`
		InputModalities           []string        `json:"input_modalities"`
		OutputModalities          []string        `json:"output_modalities"`
		SupportsTools             bool            `json:"supports_tools"`
		SupportsParallelTools     bool            `json:"supports_parallel_tools"`
		SupportedReasoningEfforts []string        `json:"supported_reasoning_efforts"`
		ContextWindowTokens       int             `json:"context_window_tokens"`
		MaxOutputTokens           int             `json:"max_output_tokens"`
		DefaultParameters         json.RawMessage `json:"default_parameters"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	model, err := a.useCases.Models.CreateModel(c.Request.Context(), currentUser(c), CreateModelInput{
		Provider: request.Provider, CredentialID: request.CredentialID, Slug: request.Slug,
		UpstreamModel: request.UpstreamModel, DisplayName: request.DisplayName, Description: request.Description,
		InputModalities: request.InputModalities, OutputModalities: request.OutputModalities,
		SupportsTools: request.SupportsTools, SupportsParallelTools: request.SupportsParallelTools,
		SupportedReasoningEfforts: request.SupportedReasoningEfforts, ContextWindowTokens: request.ContextWindowTokens,
		MaxOutputTokens: request.MaxOutputTokens, DefaultParameters: request.DefaultParameters,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"model": model})
}

func (a *API) handleGetAdminModel(c *gin.Context) {
	model, err := a.useCases.Models.GetAdminModel(c.Request.Context(), currentUser(c), c.Param("modelID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"model": model})
}

func (a *API) handleUpdateModel(c *gin.Context) {
	var request struct {
		CredentialID              *string         `json:"credential_id"`
		DisplayName               *string         `json:"display_name"`
		Description               *string         `json:"description"`
		InputModalities           []string        `json:"input_modalities"`
		OutputModalities          []string        `json:"output_modalities"`
		SupportsTools             *bool           `json:"supports_tools"`
		SupportsParallelTools     *bool           `json:"supports_parallel_tools"`
		SupportedReasoningEfforts []string        `json:"supported_reasoning_efforts"`
		ContextWindowTokens       *int            `json:"context_window_tokens"`
		MaxOutputTokens           *int            `json:"max_output_tokens"`
		DefaultParameters         json.RawMessage `json:"default_parameters"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	model, err := a.useCases.Models.UpdateModel(c.Request.Context(), currentUser(c), UpdateModelInput{
		ID: c.Param("modelID"), CredentialID: request.CredentialID, DisplayName: request.DisplayName,
		Description: request.Description, InputModalities: request.InputModalities, OutputModalities: request.OutputModalities,
		SupportsTools: request.SupportsTools, SupportsParallelTools: request.SupportsParallelTools,
		SupportedReasoningEfforts: request.SupportedReasoningEfforts, ContextWindowTokens: request.ContextWindowTokens,
		MaxOutputTokens: request.MaxOutputTokens, DefaultParameters: request.DefaultParameters,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"model": model})
}

func (a *API) handleSetModelEnabled(c *gin.Context, enabled bool) {
	status := domain.ModelStatusDisabled
	if enabled {
		status = domain.ModelStatusEnabled
	}
	model, err := a.useCases.Models.UpdateModel(c.Request.Context(), currentUser(c), UpdateModelInput{ID: c.Param("modelID"), Status: &status})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"model": model})
}

func (a *API) handleEnableModel(c *gin.Context)  { a.handleSetModelEnabled(c, true) }
func (a *API) handleDisableModel(c *gin.Context) { a.handleSetModelEnabled(c, false) }

func (a *API) handleListModelPrices(c *gin.Context) {
	items, err := a.useCases.Models.ListModelPrices(c.Request.Context(), currentUser(c), c.Param("modelID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"prices": items})
}

func (a *API) handleCreateModelPrice(c *gin.Context) {
	var request CreateModelPriceInput
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	request.ModelID = c.Param("modelID")
	price, err := a.useCases.Models.CreateModelPrice(c.Request.Context(), currentUser(c), request)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"price": price})
}

func (a *API) handleGetModelPrice(c *gin.Context) {
	price, err := a.useCases.Models.GetModelPrice(c.Request.Context(), currentUser(c), c.Param("modelID"), c.Param("priceID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"price": price})
}

func (a *API) handlePublishModelPrice(c *gin.Context) {
	var request struct {
		EffectiveFrom *time.Time `json:"effective_from"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	price, err := a.useCases.Models.PublishModelPrice(c.Request.Context(), currentUser(c), c.Param("modelID"), c.Param("priceID"), request.EffectiveFrom)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"price": price})
}

func (a *API) handleArchiveModelPrice(c *gin.Context) {
	price, err := a.useCases.Models.ArchiveModelPrice(c.Request.Context(), currentUser(c), c.Param("modelID"), c.Param("priceID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"price": price})
}

func (a *API) handleGetModelSettings(c *gin.Context) {
	settings, err := a.useCases.Models.GetModelSettings(c.Request.Context(), currentUser(c))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": settings})
}

func (a *API) handleUpdateModelSettings(c *gin.Context) {
	var request struct {
		DefaultChatModelID *string `json:"default_chat_model_id"`
		CompactionModelID  *string `json:"compaction_model_id"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	settings, err := a.useCases.Models.UpdateModelSettings(c.Request.Context(), currentUser(c), UpdateModelSettingsInput{DefaultChatModelID: request.DefaultChatModelID, CompactionModelID: request.CompactionModelID})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": settings})
}

func (a *API) handleListProviderCredentials(c *gin.Context) {
	result, err := a.useCases.Credentials.ListProviderCredentials(c.Request.Context(), currentUser(c), parseLimit(c, 50, 200), strings.TrimSpace(c.Query("cursor")))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, pagePayload(result.Items, result.NextCursor))
}

func (a *API) handleCreateProviderCredential(c *gin.Context) {
	var request struct {
		Provider string `json:"provider"`
		Name     string `json:"name"`
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	item, err := a.useCases.Credentials.CreateProviderCredential(c.Request.Context(), currentUser(c), CreateProviderCredentialInput(request))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"credential": item})
}

func (a *API) handleGetProviderCredential(c *gin.Context) {
	item, err := a.useCases.Credentials.GetProviderCredential(c.Request.Context(), currentUser(c), c.Param("credentialID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"credential": item})
}

func (a *API) handleUpdateProviderCredential(c *gin.Context) {
	var request struct {
		Name    *string `json:"name"`
		BaseURL *string `json:"base_url"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	item, err := a.useCases.Credentials.UpdateProviderCredential(c.Request.Context(), currentUser(c), UpdateProviderCredentialInput{ID: c.Param("credentialID"), Name: request.Name, BaseURL: request.BaseURL})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"credential": item})
}

func (a *API) handleRotateProviderCredential(c *gin.Context) {
	var request struct {
		APIKey string `json:"api_key"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	item, err := a.useCases.Credentials.RotateProviderCredential(c.Request.Context(), currentUser(c), c.Param("credentialID"), request.APIKey)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"credential": item})
}

func (a *API) handleValidateProviderCredential(c *gin.Context) {
	item, err := a.useCases.Credentials.ValidateProviderCredential(c.Request.Context(), currentUser(c), c.Param("credentialID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"credential": item})
}

func (a *API) handleSetProviderCredentialEnabled(c *gin.Context, enabled bool) {
	status := domain.CredentialStatusDisabled
	if enabled {
		status = domain.CredentialStatusEnabled
	}
	item, err := a.useCases.Credentials.UpdateProviderCredential(c.Request.Context(), currentUser(c), UpdateProviderCredentialInput{ID: c.Param("credentialID"), Status: &status})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"credential": item})
}

func (a *API) handleEnableProviderCredential(c *gin.Context) {
	a.handleSetProviderCredentialEnabled(c, true)
}
func (a *API) handleDisableProviderCredential(c *gin.Context) {
	a.handleSetProviderCredentialEnabled(c, false)
}

func (a *API) handleRevokeProviderCredential(c *gin.Context) {
	item, err := a.useCases.Credentials.RevokeProviderCredential(c.Request.Context(), currentUser(c), c.Param("credentialID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"credential": item})
}
