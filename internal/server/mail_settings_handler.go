package server

import (
	"net/http"

	assistantmail "github.com/EurekaMXZ/assistant/internal/mail"
	"github.com/gin-gonic/gin"
)

func (a *API) handleGetMailSettings(c *gin.Context) {
	settings, err := a.useCases.Mail.GetMailSettings(c.Request.Context(), currentUser(c))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": settings})
}

func (a *API) handleUpdateMailSettings(c *gin.Context) {
	var request struct {
		Enabled   *bool   `json:"enabled"`
		Host      *string `json:"host"`
		Port      *int    `json:"port"`
		Security  *string `json:"security"`
		Username  *string `json:"username"`
		Password  *string `json:"password"`
		FromEmail *string `json:"from_email"`
		FromName  *string `json:"from_name"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	settings, err := a.useCases.Mail.UpdateMailSettings(c.Request.Context(), currentUser(c), assistantmail.UpdateSettingsInput{
		Enabled: request.Enabled, Host: request.Host, Port: request.Port, Security: request.Security,
		Username: request.Username, Password: request.Password, FromEmail: request.FromEmail, FromName: request.FromName,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": settings})
}

func (a *API) handleTestMailSettings(c *gin.Context) {
	var request struct {
		Recipient string `json:"recipient"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	if err := a.useCases.Mail.TestMailSettings(c.Request.Context(), currentUser(c), request.Recipient); err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sent": true})
}
