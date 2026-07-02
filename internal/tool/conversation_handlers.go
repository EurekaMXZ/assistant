package tool

import (
	"context"

	"github.com/EurekaMXZ/assistant/internal/stream"
)

type RenameConversationTitleHandler struct {
	UseCase RenameConversationTitle
}

func (h RenameConversationTitleHandler) ToolName() string {
	return ConversationRenameTitle
}

func (h RenameConversationTitleHandler) Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	var input struct {
		Title string `json:"title"`
	}
	if err := decodeToolArguments(call, ConversationRenameTitle, &input); err != nil {
		return nil, err
	}

	conversation, err := h.UseCase.Execute(ctx, RenameConversationTitleInput{
		ConversationID: scope.ConversationID,
		Title:          input.Title,
	})
	if err != nil {
		return nil, err
	}

	payload, err := marshalToolOutput(ConversationRenameTitle, map[string]any{
		"conversation_id": conversation.ID,
		"title":           conversation.Title,
	})
	if err != nil {
		return nil, err
	}

	return eventedExecutionResult(scope, call.CallID, payload, stream.Event{
		Type:     stream.EventConversationUpdated,
		ToolName: ConversationRenameTitle,
	}), nil
}
