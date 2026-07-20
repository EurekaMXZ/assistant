package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (a *API) handleGetStorage(c *gin.Context) {
	userID := currentUser(c).ID
	usage, err := a.useCases.Storage.GetStorageUsage(c.Request.Context(), userID)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	items, err := a.useCases.Storage.ListStorageAttachments(
		c.Request.Context(),
		userID,
		parseLimit(c, 50, 200),
		strings.TrimSpace(c.Query("cursor")),
	)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"storage": usage,
		"data":    nonNilSlice(items.Items),
		"page":    gin.H{"next_cursor": items.NextCursor, "has_more": items.NextCursor != ""},
	})
}

func (a *API) handleDeleteStorageAttachment(c *gin.Context) {
	if err := a.useCases.Storage.DeleteAttachment(
		c.Request.Context(),
		currentUser(c).ID,
		c.Param("attachmentID"),
	); err != nil {
		writeAPIError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
