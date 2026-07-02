package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/gin-gonic/gin"
)

const maxConversationAttachmentBytes int64 = 128 << 20

func (a *API) handleUploadConversationAttachment(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxConversationAttachmentBytes+(1<<20))
	fileHeader, err := c.FormFile("file")
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil {
		if isMissingUploadFile(err) {
			writeAPIError(c, domain.NewValidationError("file is required"))
			return
		}
		if isRequestBodyTooLarge(err) {
			writeAPIError(c, domain.NewValidationError(fmt.Sprintf("file must be %d bytes or smaller", maxConversationAttachmentBytes)))
			return
		}
		writeAPIError(c, domain.NewValidationError("file is required"))
		return
	}
	if fileHeader.Size <= 0 {
		writeAPIError(c, domain.NewValidationError("file is empty"))
		return
	}
	if fileHeader.Size > maxConversationAttachmentBytes {
		writeAPIError(c, domain.NewValidationError(fmt.Sprintf("file must be %d bytes or smaller", maxConversationAttachmentBytes)))
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "open uploaded file")
		return
	}
	defer file.Close()

	attachment, err := a.useCases.Attachments.UploadConversationAttachment(
		c.Request.Context(),
		currentUser(c).ID,
		c.Param("conversationID"),
		UploadConversationAttachmentInput{
			IdempotencyKey: strings.TrimSpace(c.GetHeader("Idempotency-Key")),
			Filename:       fileHeader.Filename,
			ContentType:    fileHeader.Header.Get("Content-Type"),
			SizeBytes:      fileHeader.Size,
			File:           file,
		},
	)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"attachment": attachment})
}

func (a *API) handleGetConversationAttachment(c *gin.Context) {
	content, err := a.useCases.Attachments.GetConversationAttachment(
		c.Request.Context(),
		currentUser(c).ID,
		c.Param("conversationID"),
		c.Param("attachmentID"),
	)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if content == nil {
		writeAPIError(c, domain.ErrNotFound)
		return
	}

	filename := strings.TrimSpace(content.Attachment.Filename)
	if filename != "" {
		c.Header("Content-Disposition", fmt.Sprintf("inline; filename=%q", filename))
	}
	contentType := strings.TrimSpace(content.Attachment.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.Data(http.StatusOK, contentType, content.Data)
}

func isRequestBodyTooLarge(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "request body too large")
}

func isMissingUploadFile(err error) bool {
	return err != nil && errors.Is(err, http.ErrMissingFile)
}
