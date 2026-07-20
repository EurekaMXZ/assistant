package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	currentUserContextKey = "current_user"
	requestIDContextKey   = "request_id"
)

func withRequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 {
			requestID = uuid.NewString()
		}
		c.Set(requestIDContextKey, requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

func requestID(c *gin.Context) string {
	value, _ := c.Get(requestIDContextKey)
	requestID, _ := value.(string)
	return requestID
}

func (a *API) auditRequests() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodOptions {
			return
		}
		path := c.FullPath()
		if path == "" || strings.Contains(path, "/healthz") {
			return
		}
		semanticBillingMutation := strings.HasSuffix(path, "/topups") || strings.HasSuffix(path, "/refunds") || strings.HasSuffix(path, "/billing/redemptions") || strings.HasSuffix(path, "/billing/redemption-codes") || (strings.Contains(path, "/admin/billing/redemption-codes/") && strings.HasSuffix(path, "/disable"))
		if semanticBillingMutation && c.Writer.Status() < http.StatusBadRequest {
			return
		}
		actor := currentUser(c)
		input := RecordAuditInput{
			Action:       "http." + strings.ToLower(c.Request.Method),
			ResourceType: path,
			ResourceID:   firstPathID(c),
			Outcome:      "succeeded",
			RequestID:    requestID(c),
			ClientIP:     c.ClientIP(),
			UserAgent:    c.Request.UserAgent(),
			RequiredRole: auditRequiredRole(path),
			Metadata:     json.RawMessage(fmt.Sprintf(`{"status":%d,"duration_ms":%d}`, c.Writer.Status(), time.Since(started).Milliseconds())),
		}
		if c.Writer.Status() >= 400 {
			input.Outcome = "failed"
		}
		if actor != nil {
			input.ActorUserID = actor.ID
			input.ActorRole = actor.Role
			input.SubjectUserID = actor.ID
		}
		if target := strings.TrimSpace(c.Param("userID")); target != "" {
			input.SubjectUserID = target
			input.VisibleToSubject = actor == nil || target != actor.ID
		}
		_ = a.useCases.Audit.RecordAudit(c.Request.Context(), input)
	}
}

func auditRequiredRole(path string) string {
	for _, prefix := range []string{
		"/api/v1/admin/provider-credentials",
		"/api/v1/admin/models",
		"/api/v1/admin/model-settings",
		"/api/v1/admin/mail-settings",
	} {
		if strings.HasPrefix(path, prefix) {
			return domain.UserRoleSystem
		}
	}
	for _, prefix := range []string{
		"/api/v1/users",
		"/api/v1/admin/billing",
		"/api/v1/admin/audit-events",
	} {
		if strings.HasPrefix(path, prefix) {
			return domain.UserRoleAdmin
		}
	}
	return domain.UserRoleUser
}

func firstPathID(c *gin.Context) string {
	for _, name := range []string{"transactionID", "usageEventID", "auditEventID", "credentialID", "priceID", "modelID", "attachmentID", "turnID", "conversationID", "codeID", "userID"} {
		if value := strings.TrimSpace(c.Param(name)); value != "" {
			return value
		}
	}
	return ""
}

func withCORS(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if allowedOrigin != "" {
			c.Header("Access-Control-Allow-Origin", allowedOrigin)
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key, X-Request-ID")
			c.Header("Access-Control-Expose-Headers", "X-Request-ID")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}

		if c.Request.Method == http.MethodOptions {
			c.Status(http.StatusNoContent)
			c.Abort()
			return
		}

		c.Next()
	}
}

func (a *API) requireMinimumRole(minRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := bearerToken(c.GetHeader("Authorization"))
		if err != nil {
			writeAPIError(c, err)
			return
		}

		user, err := a.useCases.Auth.AuthenticateAccessToken(c.Request.Context(), token)
		if err != nil {
			writeAPIError(c, err)
			return
		}
		if !domain.UserRoleSatisfies(user.Role, minRole) {
			writeAPIError(c, domain.NewForbiddenError("insufficient privileges"))
			return
		}

		c.Set(currentUserContextKey, user)
		c.Next()
	}
}

func (a *API) requireExactRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := bearerToken(c.GetHeader("Authorization"))
		if err != nil {
			writeAPIError(c, err)
			return
		}
		user, err := a.useCases.Auth.AuthenticateAccessToken(c.Request.Context(), token)
		if err != nil {
			writeAPIError(c, err)
			return
		}
		if user.Role != role {
			writeAPIError(c, domain.NewForbiddenError("insufficient privileges"))
			return
		}
		c.Set(currentUserContextKey, user)
		c.Next()
	}
}

func currentUser(c *gin.Context) *domain.User {
	raw, ok := c.Get(currentUserContextKey)
	if !ok {
		return nil
	}
	user, _ := raw.(*domain.User)
	return user
}

func bearerToken(header string) (string, error) {
	value := strings.TrimSpace(header)
	if value == "" {
		return "", domain.NewUnauthorizedError("missing bearer token")
	}

	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", domain.NewUnauthorizedError("invalid authorization header")
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", domain.NewUnauthorizedError("missing bearer token")
	}
	return token, nil
}

func writeAPIError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		writeError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrAuthenticationFailed), errors.Is(err, domain.ErrUnauthorized):
		writeError(c, http.StatusUnauthorized, err.Error())
	case errors.Is(err, domain.ErrForbidden):
		writeError(c, http.StatusForbidden, err.Error())
	case errors.Is(err, domain.ErrNotFound):
		writeError(c, http.StatusNotFound, "resource not found")
	case errors.Is(err, domain.ErrConflict):
		message := err.Error()
		if message == "" || message == domain.ErrConflict.Error() {
			message = "conflict"
		}
		writeError(c, http.StatusConflict, message)
	case errors.Is(err, domain.ErrPaymentRequired):
		writeError(c, http.StatusPaymentRequired, err.Error())
	case errors.Is(err, domain.ErrStorageQuotaExceeded):
		writeError(c, http.StatusRequestEntityTooLarge, "storage quota exceeded")
	default:
		writeError(c, http.StatusInternalServerError, "internal server error")
	}
}

func writeError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, errorResponse{Error: message, RequestID: requestID(c)})
}

func bindJSON(c *gin.Context, target any) error {
	if c.Request.Body == nil {
		return nil
	}
	if err := c.ShouldBindJSON(target); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return domain.NewValidationError("invalid request body")
	}
	return nil
}

func parseLimit(c *gin.Context, fallback int, max int) int {
	value := strings.TrimSpace(c.Query("limit"))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	if parsed > max {
		return max
	}

	return parsed
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func cloneJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
