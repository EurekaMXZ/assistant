package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (a *API) handleUploadConversationAttachment(c *gin.Context) {
	var body struct {
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
		SizeBytes   int64  `json:"size_bytes"`
		SHA256      string `json:"sha256"`
		ContentMD5  string `json:"content_md5"`
	}
	if err := bindJSON(c, &body); err != nil {
		writeAPIError(c, err)
		return
	}

	result, err := a.useCases.Attachments.CreateConversationAttachmentUpload(
		c.Request.Context(),
		currentUser(c).ID,
		c.Param("conversationID"),
		CreateConversationAttachmentUploadInput{
			IdempotencyKey: strings.TrimSpace(c.GetHeader("Idempotency-Key")),
			Filename:       body.Filename,
			ContentType:    body.ContentType,
			SizeBytes:      body.SizeBytes,
			SHA256:         body.SHA256,
			ContentMD5:     body.ContentMD5,
		},
	)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (a *API) handleCompleteConversationAttachmentUpload(c *gin.Context) {
	attachment, err := a.useCases.Attachments.CompleteConversationAttachmentUpload(
		c.Request.Context(),
		currentUser(c).ID,
		c.Param("conversationID"),
		c.Param("attachmentID"),
		CompleteConversationAttachmentUploadInput{},
	)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"attachment": attachment})
}

func (a *API) handleGetConversationAttachment(c *gin.Context) {
	result, err := a.useCases.Attachments.GetConversationAttachmentDownload(
		c.Request.Context(),
		currentUser(c).ID,
		c.Param("conversationID"),
		c.Param("attachmentID"),
		strings.EqualFold(strings.TrimSpace(c.Query("disposition")), "attachment"),
	)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, result)
}
