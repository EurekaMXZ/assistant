package server

import (
	"net/http"

	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	"github.com/gin-gonic/gin"
)

func (a *API) handleListManagedUsers(c *gin.Context) {
	users, err := a.useCases.Users.ListManagedUsers(c.Request.Context(), currentUser(c), parseLimit(c, 50, 200))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"users": users})
}

func (a *API) handleGetManagedUser(c *gin.Context) {
	user, err := a.useCases.Users.GetManagedUser(c.Request.Context(), currentUser(c), c.Param("userID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (a *API) handleCreateManagedUser(c *gin.Context) {
	var request struct {
		Email    string `json:"email"`
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
		Status   string `json:"status"`
	}

	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}

	user, err := a.useCases.Users.CreateManagedUser(c.Request.Context(), currentUser(c), assistantauth.CreateManagedUserInput{
		Email:    request.Email,
		Username: request.Username,
		Password: request.Password,
		Role:     request.Role,
		Status:   request.Status,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"user": user})
}

func (a *API) handleUpdateManagedUser(c *gin.Context) {
	var request struct {
		Email    *string `json:"email"`
		Username *string `json:"username"`
		Role     *string `json:"role"`
		Status   *string `json:"status"`
	}

	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}

	user, err := a.useCases.Users.UpdateManagedUser(c.Request.Context(), currentUser(c), assistantauth.UpdateManagedUserInput{
		UserID:   c.Param("userID"),
		Email:    request.Email,
		Username: request.Username,
		Role:     request.Role,
		Status:   request.Status,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (a *API) handleResetManagedUserPassword(c *gin.Context) {
	var request struct {
		NewPassword string `json:"new_password"`
	}

	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}

	user, err := a.useCases.Users.ResetManagedPassword(c.Request.Context(), currentUser(c), assistantauth.ResetManagedPasswordInput{
		UserID:      c.Param("userID"),
		NewPassword: request.NewPassword,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": user})
}
