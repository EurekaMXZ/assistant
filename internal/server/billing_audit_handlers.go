package server

import (
	"net/http"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/gin-gonic/gin"
)

func (a *API) handleGetOwnBillingAccount(c *gin.Context) {
	account, err := a.useCases.Billing.GetBillingAccount(c.Request.Context(), currentUser(c).ID)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"account": account})
}

func (a *API) handleListBillingAccounts(c *gin.Context) {
	result, err := a.useCases.Billing.ListBillingAccounts(c.Request.Context(), currentUser(c), parseLimit(c, 50, 200), strings.TrimSpace(c.Query("cursor")))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, pagePayload(result.Items, result.NextCursor))
}

func (a *API) handleGetAdminBillingAccount(c *gin.Context) {
	account, err := a.useCases.Billing.GetBillingAccount(c.Request.Context(), c.Param("userID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"account": account})
}

func (a *API) handleUpdateAdminBillingAccount(c *gin.Context) {
	var request struct {
		Status *string `json:"status"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	account, err := a.useCases.Billing.UpdateBillingAccount(c.Request.Context(), currentUser(c), UpdateBillingAccountInput{UserID: c.Param("userID"), Status: request.Status})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"account": account})
}

func (a *API) handleManualBilling(c *gin.Context, refund bool) {
	var request struct {
		Amount    string `json:"amount"`
		Currency  string `json:"currency"`
		Reason    string `json:"reason"`
		Reference string `json:"reference"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	input := ManualBillingInput{
		UserID: c.Param("userID"), Amount: request.Amount, Currency: request.Currency,
		Reason: request.Reason, Reference: request.Reference,
		IdempotencyKey: strings.TrimSpace(c.GetHeader("Idempotency-Key")), RequestID: requestID(c),
	}
	var transaction *domain.BillingTransaction
	var err error
	if refund {
		transaction, err = a.useCases.Billing.ApplyManualRefund(c.Request.Context(), currentUser(c), input)
	} else {
		transaction, err = a.useCases.Billing.ApplyManualTopup(c.Request.Context(), currentUser(c), input)
	}
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"transaction": transaction})
}

func (a *API) handleManualTopup(c *gin.Context)  { a.handleManualBilling(c, false) }
func (a *API) handleManualRefund(c *gin.Context) { a.handleManualBilling(c, true) }

func billingListInput(c *gin.Context, userID string) BillingListInput {
	return BillingListInput{
		UserID: userID, Kind: strings.TrimSpace(c.Query("kind")), Status: strings.TrimSpace(c.Query("status")),
		Limit: parseLimit(c, 50, 200), Cursor: strings.TrimSpace(c.Query("cursor")),
	}
}

func (a *API) handleListOwnBillingTransactions(c *gin.Context) {
	result, err := a.useCases.Billing.ListBillingTransactions(c.Request.Context(), billingListInput(c, currentUser(c).ID))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, pagePayload(result.Items, result.NextCursor))
}

func (a *API) handleGetOwnBillingTransaction(c *gin.Context) {
	item, err := a.useCases.Billing.GetBillingTransaction(c.Request.Context(), c.Param("transactionID"), currentUser(c).ID)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"transaction": item})
}

func (a *API) handleListAdminBillingTransactions(c *gin.Context) {
	result, err := a.useCases.Billing.ListBillingTransactions(c.Request.Context(), billingListInput(c, strings.TrimSpace(c.Query("user_id"))))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, pagePayload(result.Items, result.NextCursor))
}

func (a *API) handleGetAdminBillingTransaction(c *gin.Context) {
	item, err := a.useCases.Billing.GetBillingTransaction(c.Request.Context(), c.Param("transactionID"), "")
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"transaction": item})
}

func (a *API) handleListOwnBillingUsageEvents(c *gin.Context) {
	result, err := a.useCases.Billing.ListBillingUsageEvents(c.Request.Context(), billingListInput(c, currentUser(c).ID))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, pagePayload(result.Items, result.NextCursor))
}

func (a *API) handleGetOwnBillingUsageEvent(c *gin.Context) {
	item, err := a.useCases.Billing.GetBillingUsageEvent(c.Request.Context(), c.Param("usageEventID"), currentUser(c).ID)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"usage_event": item})
}

func (a *API) handleListAdminBillingUsageEvents(c *gin.Context) {
	result, err := a.useCases.Billing.ListBillingUsageEvents(c.Request.Context(), billingListInput(c, strings.TrimSpace(c.Query("user_id"))))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, pagePayload(result.Items, result.NextCursor))
}

func (a *API) handleGetAdminBillingUsageEvent(c *gin.Context) {
	item, err := a.useCases.Billing.GetBillingUsageEvent(c.Request.Context(), c.Param("usageEventID"), "")
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"usage_event": item})
}

func auditListInput(c *gin.Context, viewer string, viewerRole string) AuditListInput {
	return AuditListInput{
		ViewerUserID: viewer, ViewerRole: viewerRole, ActorUserID: strings.TrimSpace(c.Query("actor_user_id")),
		SubjectUserID: strings.TrimSpace(c.Query("subject_user_id")), Action: strings.TrimSpace(c.Query("action")),
		ResourceType: strings.TrimSpace(c.Query("resource_type")), Outcome: strings.TrimSpace(c.Query("outcome")),
		Limit: parseLimit(c, 50, 200), Cursor: strings.TrimSpace(c.Query("cursor")),
	}
}

func (a *API) handleListOwnAuditEvents(c *gin.Context) {
	user := currentUser(c)
	result, err := a.useCases.Audit.ListAuditEvents(c.Request.Context(), auditListInput(c, user.ID, user.Role))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, pagePayload(result.Items, result.NextCursor))
}

func (a *API) handleGetOwnAuditEvent(c *gin.Context) {
	user := currentUser(c)
	item, err := a.useCases.Audit.GetAuditEvent(c.Request.Context(), c.Param("auditEventID"), user.ID, user.Role)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"audit_event": item})
}

func (a *API) handleListAdminAuditEvents(c *gin.Context) {
	user := currentUser(c)
	result, err := a.useCases.Audit.ListAuditEvents(c.Request.Context(), auditListInput(c, user.ID, user.Role))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, pagePayload(result.Items, result.NextCursor))
}

func (a *API) handleGetAdminAuditEvent(c *gin.Context) {
	user := currentUser(c)
	item, err := a.useCases.Audit.GetAuditEvent(c.Request.Context(), c.Param("auditEventID"), user.ID, user.Role)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"audit_event": item})
}
