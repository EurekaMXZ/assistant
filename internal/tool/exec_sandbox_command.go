package tool

import (
	"context"
	"errors"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type ExecSandboxCommandInput struct {
	ConversationID   string
	Command          string
	Args             []string
	WorkingDirectory string
	TimeoutSeconds   int
	RequestKey       string
}

type ExecSandboxCommand struct {
	Sandboxes ConversationSandboxReader
	Runtime   SandboxManager
}

func (uc ExecSandboxCommand) Execute(ctx context.Context, input ExecSandboxCommandInput) (*domain.SandboxCommandResult, error) {
	if uc.Sandboxes == nil {
		return nil, errors.New("exec sandbox command use case requires sandbox store")
	}
	if uc.Runtime == nil {
		return nil, errors.New("exec sandbox command use case requires sandbox runtime")
	}

	command := strings.TrimSpace(input.Command)
	if command == "" {
		return nil, errors.New("command is required")
	}

	sandbox, err := uc.Sandboxes.GetActiveConversationSandbox(ctx, input.ConversationID)
	if err != nil {
		return nil, err
	}

	return uc.Runtime.ExecSandboxCommand(ctx, domain.SandboxHandle{
		Provider:  sandbox.Provider,
		RuntimeID: sandbox.RuntimeID,
		Metadata:  sandbox.RuntimeMetadata,
	}, domain.SandboxCommandRequest{
		Command:          command,
		Args:             append([]string(nil), input.Args...),
		WorkingDirectory: strings.TrimSpace(input.WorkingDirectory),
		TimeoutSeconds:   input.TimeoutSeconds,
	}, input.RequestKey)
}
