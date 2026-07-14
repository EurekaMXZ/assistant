package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/gin-gonic/gin"
)

func (a *API) handleCreateConversation(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "authentication required")
		return
	}

	var request struct {
		Title    string          `json:"title"`
		Metadata json.RawMessage `json:"metadata"`
	}

	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}

	conversation, err := a.useCases.Conversations.CreateConversation(c.Request.Context(), user.ID, request.Title, cloneJSON(request.Metadata))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"conversation": conversation})
}

func (a *API) handleInitialTurn(c *gin.Context) {
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" || len(idempotencyKey) > 128 {
		writeAPIError(c, domain.NewValidationError("Idempotency-Key is required and must be at most 128 characters"))
		return
	}

	var request struct {
		Action          string          `json:"action"`
		ConversationID  string          `json:"conversation_id"`
		Title           string          `json:"title"`
		Content         string          `json:"content"`
		AttachmentIDs   []string        `json:"attachment_ids"`
		ModelID         string          `json:"model_id"`
		ReasoningEffort string          `json:"reasoning_effort"`
		Metadata        json.RawMessage `json:"metadata"`
	}
	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	if request.Action != InitialTurnActionPrepare && request.Action != InitialTurnActionCommit {
		writeAPIError(c, domain.NewValidationError("action must be prepare or commit"))
		return
	}
	if request.Action == InitialTurnActionCommit {
		if strings.TrimSpace(request.ConversationID) == "" {
			writeAPIError(c, domain.NewValidationError("conversation_id is required for commit"))
			return
		}
		if strings.TrimSpace(request.Content) == "" && len(request.AttachmentIDs) == 0 {
			writeAPIError(c, domain.NewValidationError("content is required"))
			return
		}
	}

	result, err := a.useCases.Conversations.InitialTurn(c.Request.Context(), currentUser(c).ID, idempotencyKey, InitialTurnInput{
		Action: request.Action, ConversationID: request.ConversationID, Title: request.Title,
		Content: request.Content, AttachmentIDs: append([]string(nil), request.AttachmentIDs...),
		ModelID: request.ModelID, ReasoningEffort: request.ReasoningEffort, Metadata: cloneJSON(request.Metadata),
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	status := http.StatusCreated
	payload := gin.H{
		"state": result.State, "replayed": result.Replayed, "conversation": result.Conversation,
	}
	if result.Turn != nil && result.Message != nil {
		status = http.StatusAccepted
		payload["message"] = result.Message
		payload["turn"] = result.Turn
		payload["stream_path"] = "/api/v1/turns/" + result.Turn.ID + "/stream"
	}
	c.JSON(status, payload)
}

func (a *API) handleListConversations(c *gin.Context) {
	user := currentUser(c)
	conversations, err := a.useCases.Conversations.ListConversations(c.Request.Context(), user.ID, parseLimit(c, 50, 200))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"conversations": nonNilSlice(conversations)})
}

func (a *API) handleGetConversation(c *gin.Context) {
	user := currentUser(c)
	conversation, err := a.useCases.Conversations.GetConversation(c.Request.Context(), user.ID, c.Param("conversationID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"conversation": conversation})
}

func (a *API) handleUpdateConversation(c *gin.Context) {
	var request struct {
		Title    *string `json:"title"`
		Archived *bool   `json:"archived"`
	}

	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}

	conversation, err := a.useCases.Conversations.UpdateConversation(c.Request.Context(), currentUser(c).ID, UpdateConversationInput{
		ConversationID: c.Param("conversationID"),
		Title:          request.Title,
		Archived:       request.Archived,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"conversation": conversation})
}

func (a *API) handleListMessages(c *gin.Context) {
	messages, err := a.useCases.Conversations.ListMessages(
		c.Request.Context(),
		currentUser(c).ID,
		c.Param("conversationID"),
		parseLimit(c, 100, 1000),
	)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"messages": nonNilSlice(messages)})
}

func (a *API) handleCreateMessage(c *gin.Context) {
	var request struct {
		Content         string          `json:"content"`
		AttachmentIDs   []string        `json:"attachment_ids"`
		ModelID         string          `json:"model_id"`
		ReasoningEffort string          `json:"reasoning_effort"`
		Metadata        json.RawMessage `json:"metadata"`
	}

	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	if strings.TrimSpace(request.Content) == "" && len(request.AttachmentIDs) == 0 {
		writeAPIError(c, domain.NewValidationError("content is required"))
		return
	}

	result, err := a.useCases.Conversations.SendMessage(
		c.Request.Context(),
		currentUser(c).ID,
		c.Param("conversationID"),
		SendMessageInput{
			Content:         request.Content,
			AttachmentIDs:   append([]string(nil), request.AttachmentIDs...),
			ModelID:         request.ModelID,
			ReasoningEffort: request.ReasoningEffort,
			Metadata:        cloneJSON(request.Metadata),
		},
	)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"conversation_id": result.ConversationID,
		"message":         result.Message,
		"turn":            result.Turn,
		"stream_path":     "/api/v1/turns/" + result.Turn.ID + "/stream",
	})
}

func (a *API) handleGetConversationSandbox(c *gin.Context) {
	sandbox, err := a.useCases.Sandboxes.GetConversationSandbox(c.Request.Context(), currentUser(c).ID, c.Param("conversationID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"sandbox": sandbox})
}

func (a *API) handleCreateConversationSandbox(c *gin.Context) {
	sandbox, err := a.useCases.Sandboxes.CreateConversationSandbox(c.Request.Context(), currentUser(c).ID, c.Param("conversationID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"sandbox": sandbox})
}

func (a *API) handleDestroyConversationSandbox(c *gin.Context) {
	sandbox, err := a.useCases.Sandboxes.DestroyConversationSandbox(c.Request.Context(), currentUser(c).ID, c.Param("conversationID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"sandbox": sandbox})
}

func (a *API) handleExecConversationSandbox(c *gin.Context) {
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
	var request struct {
		Command          string   `json:"command"`
		Args             []string `json:"args"`
		WorkingDirectory string   `json:"working_directory"`
		TimeoutSeconds   int      `json:"timeout_seconds"`
	}

	if err := bindJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}

	result, err := a.useCases.Sandboxes.ExecConversationSandbox(c.Request.Context(), currentUser(c).ID, ExecConversationSandboxInput{
		ConversationID:   c.Param("conversationID"),
		Command:          request.Command,
		Args:             request.Args,
		WorkingDirectory: request.WorkingDirectory,
		TimeoutSeconds:   request.TimeoutSeconds,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"result": result})
}
