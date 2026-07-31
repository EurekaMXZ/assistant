package workflow

import "context"

type WorkflowEventBatchPublisher func(ctx context.Context, events []WorkflowEvent) error
