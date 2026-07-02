package postgres

import "github.com/jackc/pgx/v5/pgxpool"

type WorkflowOutboxRepository struct {
	pool *pgxpool.Pool
}

func NewWorkflowOutboxRepository(pool *pgxpool.Pool) *WorkflowOutboxRepository {
	return &WorkflowOutboxRepository{pool: pool}
}

type WorkflowTurnRepository struct {
	pool *pgxpool.Pool
}

func NewWorkflowTurnRepository(pool *pgxpool.Pool) *WorkflowTurnRepository {
	return &WorkflowTurnRepository{pool: pool}
}

type WorkflowContextRepository struct {
	pool *pgxpool.Pool
}

func NewWorkflowContextRepository(pool *pgxpool.Pool) *WorkflowContextRepository {
	return &WorkflowContextRepository{pool: pool}
}

type TurnRunRepository struct {
	pool *pgxpool.Pool
}

func NewTurnRunRepository(pool *pgxpool.Pool) *TurnRunRepository {
	return &TurnRunRepository{pool: pool}
}

type ToolCallRepository struct {
	pool *pgxpool.Pool
}

func NewToolCallRepository(pool *pgxpool.Pool) *ToolCallRepository {
	return &ToolCallRepository{pool: pool}
}

type StaleTurnRepository struct {
	pool *pgxpool.Pool
}

func NewStaleTurnRepository(pool *pgxpool.Pool) *StaleTurnRepository {
	return &StaleTurnRepository{pool: pool}
}

type TurnStreamEventRepository struct {
	pool *pgxpool.Pool
}

func NewTurnStreamEventRepository(pool *pgxpool.Pool) *TurnStreamEventRepository {
	return &TurnStreamEventRepository{pool: pool}
}
