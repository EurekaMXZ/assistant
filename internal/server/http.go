package server

import (
	"context"
	"io"
	"net"
	"net/http"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/gin-gonic/gin"
)

type turnStreamSubscriber interface {
	SubscribeEvents(ctx context.Context, turnID string) (io.Closer, <-chan stream.Event, error)
}

type API struct {
	useCases  UseCases
	streamHub turnStreamSubscriber
}

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

type errorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

func New(settings Settings, useCases UseCases, streamHub turnStreamSubscriber, baseCtx context.Context) *http.Server {
	if err := useCases.validate(); err != nil {
		panic(err)
	}
	api := &API{
		useCases:  useCases,
		streamHub: streamHub,
	}
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	engine := gin.New()
	engine.Use(withRequestID())
	engine.Use(api.auditRequests())
	engine.Use(gin.Recovery())
	engine.Use(withCORS(settings.WebOrigin))
	api.registerRoutes(engine)

	return &http.Server{
		Addr:    settings.Address,
		Handler: engine,
		BaseContext: func(net.Listener) context.Context {
			return baseCtx
		},
		ReadTimeout:  settings.ReadTimeout,
		WriteTimeout: settings.WriteTimeout,
		IdleTimeout:  settings.IdleTimeout,
	}
}

func (a *API) registerRoutes(router *gin.Engine) {
	router.GET("/", rootHandler)
	router.GET("/healthz", healthHandler)

	v1 := router.Group("/api/v1")
	v1.GET("/healthz", healthHandler)
	v1.POST("/auth/register", a.handleRegister)
	v1.POST("/auth/login", a.handleLogin)
	v1.POST("/auth/verify-email", a.handleVerifyEmail)
	v1.POST("/auth/resend-verification", a.handleResendVerification)
	v1.POST("/auth/forgot-password", a.handleForgotPassword)
	v1.POST("/auth/reset-password", a.handleResetPassword)

	userRoutes := v1.Group("")
	userRoutes.Use(a.requireMinimumRole(domain.UserRoleUser))
	userRoutes.GET("/auth/me", a.handleGetCurrentUser)
	userRoutes.POST("/auth/change-password", a.handleChangeOwnPassword)
	userRoutes.GET("/models", a.handleListModels)
	userRoutes.GET("/models/:modelID", a.handleGetModel)
	userRoutes.GET("/billing/account", a.handleGetOwnBillingAccount)
	userRoutes.GET("/billing/transactions", a.handleListOwnBillingTransactions)
	userRoutes.GET("/billing/transactions/:transactionID", a.handleGetOwnBillingTransaction)
	userRoutes.GET("/billing/usage-events", a.handleListOwnBillingUsageEvents)
	userRoutes.GET("/billing/usage-events/:usageEventID", a.handleGetOwnBillingUsageEvent)
	userRoutes.GET("/audit-events", a.handleListOwnAuditEvents)
	userRoutes.GET("/audit-events/:auditEventID", a.handleGetOwnAuditEvent)
	userRoutes.GET("/conversations", a.handleListConversations)
	userRoutes.POST("/conversations", a.handleCreateConversation)
	userRoutes.POST("/conversations/initial-turns", a.handleInitialTurn)
	userRoutes.GET("/conversations/:conversationID", a.handleGetConversation)
	userRoutes.PATCH("/conversations/:conversationID", a.handleUpdateConversation)
	userRoutes.POST("/conversations/:conversationID/attachments", a.handleUploadConversationAttachment)
	userRoutes.GET("/conversations/:conversationID/attachments/:attachmentID", a.handleGetConversationAttachment)
	userRoutes.GET("/conversations/:conversationID/messages", a.handleListMessages)
	userRoutes.POST("/conversations/:conversationID/messages", a.handleCreateMessage)
	userRoutes.GET("/conversations/:conversationID/sandbox", a.handleGetConversationSandbox)
	userRoutes.POST("/conversations/:conversationID/sandbox", a.handleCreateConversationSandbox)
	userRoutes.POST("/conversations/:conversationID/sandbox/exec", a.handleExecConversationSandbox)
	userRoutes.DELETE("/conversations/:conversationID/sandbox", a.handleDestroyConversationSandbox)
	userRoutes.GET("/turns/:turnID", a.handleGetTurn)
	userRoutes.GET("/turns/:turnID/execution-trace", a.handleGetTurnExecutionTrace)
	userRoutes.GET("/turns/:turnID/stream", a.handleStreamTurn)

	adminRoutes := v1.Group("")
	adminRoutes.Use(a.requireMinimumRole(domain.UserRoleAdmin))
	adminRoutes.GET("/users", a.handleListManagedUsers)
	adminRoutes.POST("/users", a.handleCreateManagedUser)
	adminRoutes.GET("/users/:userID", a.handleGetManagedUser)
	adminRoutes.PATCH("/users/:userID", a.handleUpdateManagedUser)
	adminRoutes.POST("/users/:userID/reset-password", a.handleResetManagedUserPassword)
	adminRoutes.GET("/admin/billing/accounts", a.handleListBillingAccounts)
	adminRoutes.GET("/admin/billing/accounts/:userID", a.handleGetAdminBillingAccount)
	adminRoutes.PATCH("/admin/billing/accounts/:userID", a.handleUpdateAdminBillingAccount)
	adminRoutes.POST("/admin/billing/accounts/:userID/topups", a.handleManualTopup)
	adminRoutes.POST("/admin/billing/accounts/:userID/refunds", a.handleManualRefund)
	adminRoutes.GET("/admin/billing/transactions", a.handleListAdminBillingTransactions)
	adminRoutes.GET("/admin/billing/transactions/:transactionID", a.handleGetAdminBillingTransaction)
	adminRoutes.GET("/admin/billing/usage-events", a.handleListAdminBillingUsageEvents)
	adminRoutes.GET("/admin/billing/usage-events/:usageEventID", a.handleGetAdminBillingUsageEvent)
	adminRoutes.GET("/admin/audit-events", a.handleListAdminAuditEvents)
	adminRoutes.GET("/admin/audit-events/:auditEventID", a.handleGetAdminAuditEvent)

	systemRoutes := v1.Group("")
	systemRoutes.Use(a.requireExactRole(domain.UserRoleSystem))
	systemRoutes.GET("/admin/provider-credentials", a.handleListProviderCredentials)
	systemRoutes.POST("/admin/provider-credentials", a.handleCreateProviderCredential)
	systemRoutes.GET("/admin/provider-credentials/:credentialID", a.handleGetProviderCredential)
	systemRoutes.PATCH("/admin/provider-credentials/:credentialID", a.handleUpdateProviderCredential)
	systemRoutes.DELETE("/admin/provider-credentials/:credentialID", a.handleRevokeProviderCredential)
	systemRoutes.POST("/admin/provider-credentials/:credentialID/rotate", a.handleRotateProviderCredential)
	systemRoutes.POST("/admin/provider-credentials/:credentialID/validate", a.handleValidateProviderCredential)
	systemRoutes.POST("/admin/provider-credentials/:credentialID/enable", a.handleEnableProviderCredential)
	systemRoutes.POST("/admin/provider-credentials/:credentialID/disable", a.handleDisableProviderCredential)
	systemRoutes.GET("/admin/models", a.handleListAdminModels)
	systemRoutes.POST("/admin/models", a.handleCreateModel)
	systemRoutes.GET("/admin/models/:modelID", a.handleGetAdminModel)
	systemRoutes.PATCH("/admin/models/:modelID", a.handleUpdateModel)
	systemRoutes.POST("/admin/models/:modelID/enable", a.handleEnableModel)
	systemRoutes.POST("/admin/models/:modelID/disable", a.handleDisableModel)
	systemRoutes.GET("/admin/models/:modelID/prices", a.handleListModelPrices)
	systemRoutes.POST("/admin/models/:modelID/prices", a.handleCreateModelPrice)
	systemRoutes.GET("/admin/models/:modelID/prices/:priceID", a.handleGetModelPrice)
	systemRoutes.POST("/admin/models/:modelID/prices/:priceID/publish", a.handlePublishModelPrice)
	systemRoutes.POST("/admin/models/:modelID/prices/:priceID/archive", a.handleArchiveModelPrice)
	systemRoutes.GET("/admin/model-settings", a.handleGetModelSettings)
	systemRoutes.PATCH("/admin/model-settings", a.handleUpdateModelSettings)
	systemRoutes.GET("/admin/mail-settings", a.handleGetMailSettings)
	systemRoutes.PATCH("/admin/mail-settings", a.handleUpdateMailSettings)
	systemRoutes.POST("/admin/mail-settings/test", a.handleTestMailSettings)
}

func rootHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name":    "assistant-backend",
		"message": "assistant backend is ready",
	})
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, healthResponse{
		Status:  "ok",
		Service: "assistant-backend",
		Version: "dev",
	})
}
