package workflow

import "context"

type WorkflowEventPublisher func(ctx context.Context, event WorkflowEvent) error
