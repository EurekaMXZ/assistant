package postgres

import "github.com/EurekaMXZ/assistant/internal/workflow"

var (
	_ workflow.WorkflowOutboxRepository  = (*WorkflowOutboxRepository)(nil)
	_ workflow.TurnWorkflowRepository    = (*WorkflowTurnRepository)(nil)
	_ workflow.WorkflowContextRepository = (*WorkflowContextRepository)(nil)
	_ workflow.TurnRunWorkflowStore      = (*TurnRunRepository)(nil)
	_ workflow.ToolCallStore             = (*ToolCallRepository)(nil)
	_ workflow.StaleTurnRepository       = (*StaleTurnRepository)(nil)
	_ workflow.TurnStreamEventStore      = (*TurnStreamEventRepository)(nil)
)
