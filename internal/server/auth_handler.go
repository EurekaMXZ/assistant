package server

import (
	"net/http"

	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	"github.com/gin-gonic/gin"
)

func (a *API) handleRegister(c *gin.Context) {
	var request struct {
		Email    string `json:"email"`
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}

	result, err := a.useCases.Auth.Register(c.Request.Context(), assistantauth.RegisterInput{
		Email:    request.Email,
		Username: request.Username,
		Password: request.Password,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusCreated, result)
}

func (a *API) handleVerifyEmail(c *gin.Context) {
	var request struct {
		Token string `json:"token"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	if _, err := a.useCases.Auth.VerifyEmail(c.Request.Context(), assistantauth.VerifyEmailInput{Token: request.Token}); err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"verified": true})
}

func (a *API) handleResendVerification(c *gin.Context) {
	var request struct {
		Email string `json:"email"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	if err := a.useCases.Auth.ResendVerification(c.Request.Context(), assistantauth.ResendVerificationInput{Email: request.Email}); err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "if the account exists, a verification email will be sent"})
}

func (a *API) handleForgotPassword(c *gin.Context) {
	var request struct {
		Email string `json:"email"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	if err := a.useCases.Auth.ForgotPassword(c.Request.Context(), assistantauth.ForgotPasswordInput{Email: request.Email}); err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "if the account exists, a password reset email will be sent"})
}

func (a *API) handleResetPassword(c *gin.Context) {
	var request struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	if _, err := a.useCases.Auth.ResetPassword(c.Request.Context(), assistantauth.ResetPasswordInput{Token: request.Token, NewPassword: request.NewPassword}); err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"password_reset": true})
}

func (a *API) handleLogin(c *gin.Context) {
	var request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}

	session, err := a.useCases.Auth.Login(c.Request.Context(), assistantauth.LoginInput{
		Email:    request.Email,
		Password: request.Password,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"session": session})
}

func (a *API) handleGetCurrentUser(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"user": currentUser(c)})
}

func (a *API) handleChangeOwnPassword(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "authentication required")
		return
	}

	var request struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}

	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}

	updatedUser, err := a.useCases.Auth.ChangeOwnPassword(c.Request.Context(), assistantauth.ChangePasswordInput{
		UserID:          user.ID,
		CurrentPassword: request.CurrentPassword,
		NewPassword:     request.NewPassword,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": updatedUser})
}
