package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *ToolCallRepository) ScheduleToolExecutionPlan(ctx context.Context, input workflow.ToolExecutionScheduleInput) ([]domain.ToolCallRecord, error) {
	if input.Run == nil || input.Plan == nil || len(input.Plan.Groups) == 0 {
		return nil, domain.NewValidationError("tool execution plan is empty")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tool execution plan: %w", err)
	}
	defer tx.Rollback(ctx)

	var conversationID, turnStatus, runStatus string
	if err := tx.QueryRow(ctx, `
		SELECT t.conversation_id::text, t.status, tr.status
		FROM turns t
		JOIN turn_runs tr ON tr.id = $2::uuid AND tr.turn_id = t.id
		WHERE t.id = $1::uuid
		FOR UPDATE OF t, tr
	`, input.Run.TurnID, input.Run.ID).Scan(&conversationID, &turnStatus, &runStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("lock run for tool execution plan: %w", err)
	}
	if turnStatus != domain.TurnStatusProcessing {
		return nil, domain.ErrConflict
	}
	if runStatus == domain.TurnRunStatusWaitingTools {
		records, err := listToolCallsByRun(ctx, tx, input.Run.ID)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit existing tool execution plan: %w", err)
		}
		return records, nil
	}
	if runStatus != domain.TurnRunStatusRunning {
		return nil, domain.ErrConflict
	}

	for _, group := range input.Plan.Groups {
		for _, planned := range group.Calls {
			call := planned.Call
			if _, err := tx.Exec(ctx, `
				INSERT INTO tool_calls (
					turn_id, turn_run_id, call_id, tool_type, namespace, tool_name, status,
					execution_attempt, execution_group, execution_ordinal, stable_operation_id,
					arguments_blob_key
				)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
				ON CONFLICT (turn_run_id, call_id) DO NOTHING
			`, input.Run.TurnID, input.Run.ID, call.CallID, normalizedToolCallType(call.Type), nullableText(call.Namespace), call.Name,
				domain.ToolCallStatusQueued, maxInt(input.Run.Attempt, 1), group.Index, planned.Ordinal,
				planned.StableOperationID, planned.ArgumentsBlobKey); err != nil {
				return nil, fmt.Errorf("insert queued tool call: %w", err)
			}
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE turn_runs
		SET status = $2, lease_token = NULL, heartbeat_at = NULL, error_message = NULL
		WHERE id = $1::uuid AND status = $3
	`, input.Run.ID, domain.TurnRunStatusWaitingTools, domain.TurnRunStatusRunning); err != nil {
		return nil, fmt.Errorf("mark run waiting for tools: %w", err)
	}

	firstGroup := input.Plan.Groups[0].Index
	rows, err := tx.Query(ctx, `
		SELECT id::text
		FROM tool_calls
		WHERE turn_run_id = $1::uuid AND execution_group = $2 AND status = $3
		ORDER BY execution_ordinal ASC, id ASC
	`, input.Run.ID, firstGroup, domain.ToolCallStatusQueued)
	if err != nil {
		return nil, fmt.Errorf("load first tool execution group: %w", err)
	}
	var firstCallIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan first tool call: %w", err)
		}
		firstCallIDs = append(firstCallIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate first tool group: %w", err)
	}
	rows.Close()
	for _, toolCallID := range firstCallIDs {
		if err := insertOutboxEvent(ctx, tx, outboxInsert{
			EventType: workflow.EventToolCallRequested, ConversationID: conversationID,
			TurnID: input.Run.TurnID, TurnRunID: input.Run.ID, ToolCallID: toolCallID,
			ExecutionGroup: firstGroup,
		}); err != nil {
			return nil, err
		}
	}

	records, err := listToolCallsByRun(ctx, tx, input.Run.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tool execution plan: %w", err)
	}
	return records, nil
}

func (r *ToolCallRepository) ClaimQueuedToolCall(ctx context.Context, toolCallID string, leaseTimeout time.Duration) (*domain.ToolCallRecord, workflow.ToolCallLease, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, workflow.ToolCallLease{}, fmt.Errorf("begin tool call claim: %w", err)
	}
	defer tx.Rollback(ctx)
	token := uuid.NewString()
	record, err := scanToolCall(tx.QueryRow(ctx, `
		UPDATE tool_calls tc
		SET status = $2,
			execution_attempt = CASE WHEN tc.status = $4 THEN tc.execution_attempt + 1 ELSE tc.execution_attempt END,
			lease_token = $3::uuid, leased_at = now(), started_at = COALESCE(tc.started_at, now())
		FROM turn_runs tr
		WHERE tc.id = $1::uuid AND tr.id = tc.turn_run_id
		  AND tr.status = $5
		  AND (tc.status = $6 OR (tc.status = $4 AND tc.leased_at < now() - $7::interval))
		RETURNING `+toolCallColumns,
		toolCallID, domain.ToolCallStatusRunning, token, domain.ToolCallStatusRunning, domain.TurnRunStatusWaitingTools,
		domain.ToolCallStatusQueued, leaseInterval(leaseTimeout)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, workflow.ToolCallLease{}, domain.ErrConflict
		}
		return nil, workflow.ToolCallLease{}, fmt.Errorf("claim queued tool call: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, workflow.ToolCallLease{}, fmt.Errorf("commit tool call claim: %w", err)
	}
	return record, workflow.ToolCallLease{ToolCallID: record.ID, TurnRunID: record.TurnRunID, Token: token}, nil
}

func (r *ToolCallRepository) RenewQueuedToolCallLease(ctx context.Context, lease workflow.ToolCallLease) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE tool_calls tc
		SET leased_at = now()
		WHERE tc.id = $1::uuid AND tc.turn_run_id = $2::uuid
		  AND tc.status = $3 AND tc.lease_token = $4::uuid
		  AND EXISTS (SELECT 1 FROM turn_runs tr WHERE tr.id = tc.turn_run_id AND tr.status = $5)
	`, lease.ToolCallID, lease.TurnRunID, domain.ToolCallStatusRunning, lease.Token, domain.TurnRunStatusWaitingTools)
	if err != nil {
		return fmt.Errorf("renew tool call lease: %w", err)
	}
	if result.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}

func (r *ToolCallRepository) CompleteQueuedToolCall(ctx context.Context, lease workflow.ToolCallLease, outputBlobKey string) (*workflow.ToolCallSettlement, error) {
	return r.settleQueuedToolCall(ctx, lease, domain.ToolCallStatusCompleted, outputBlobKey, "")
}

func (r *ToolCallRepository) FailQueuedToolCall(ctx context.Context, lease workflow.ToolCallLease, outputBlobKey string, message string) (*workflow.ToolCallSettlement, error) {
	return r.settleQueuedToolCall(ctx, lease, domain.ToolCallStatusFailed, outputBlobKey, message)
}

func (r *ToolCallRepository) MarkQueuedToolCallUncertain(ctx context.Context, lease workflow.ToolCallLease, outputBlobKey string, message string) (*workflow.ToolCallSettlement, error) {
	return r.settleQueuedToolCall(ctx, lease, domain.ToolCallStatusUncertain, outputBlobKey, message)
}

func (r *ToolCallRepository) settleQueuedToolCall(ctx context.Context, lease workflow.ToolCallLease, status string, outputBlobKey string, message string) (*workflow.ToolCallSettlement, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tool call settlement: %w", err)
	}
	defer tx.Rollback(ctx)
	var runStatus string
	if err := tx.QueryRow(ctx, `
		SELECT status FROM turn_runs WHERE id = $1::uuid FOR UPDATE
	`, lease.TurnRunID).Scan(&runStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("lock tool call run: %w", err)
	}
	if runStatus != domain.TurnRunStatusWaitingTools {
		return nil, domain.ErrConflict
	}
	query := `
		UPDATE tool_calls tc
		SET status = $2, output_blob_key = NULLIF($3, ''), error_message = NULLIF($4, ''),
			completed_at = CASE WHEN $2 = $5 THEN now() ELSE NULL END,
			failed_at = CASE WHEN $2 = $6 OR $2 = $7 THEN now() ELSE NULL END,
			cancelled_at = NULL, lease_token = NULL, leased_at = NULL
		WHERE tc.id = $1::uuid AND tc.turn_run_id = $8::uuid
		  AND tc.status = $9 AND tc.lease_token = $10::uuid
		RETURNING ` + toolCallColumns
	record, err := scanToolCall(tx.QueryRow(ctx, query, lease.ToolCallID, status, outputBlobKey, message,
		domain.ToolCallStatusCompleted, domain.ToolCallStatusFailed, domain.ToolCallStatusUncertain,
		lease.TurnRunID, domain.ToolCallStatusRunning, lease.Token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("settle tool call: %w", err)
	}
	var pending int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM tool_calls
		WHERE turn_run_id = $1::uuid AND execution_group = $2
		  AND status IN ($3, $4, $5)
	`, record.TurnRunID, record.ExecutionGroup, domain.ToolCallStatusQueued, domain.ToolCallStatusRunning, domain.ToolCallStatusAwaitingInput).Scan(&pending); err != nil {
		return nil, fmt.Errorf("count pending tool group: %w", err)
	}
	if pending == 0 {
		var conversationID string
		if err := tx.QueryRow(ctx, `
			SELECT t.conversation_id::text FROM turns t WHERE t.id = $1::uuid
		`, record.TurnID).Scan(&conversationID); err != nil {
			return nil, fmt.Errorf("load tool group conversation: %w", err)
		}
		if err := insertOutboxEvent(ctx, tx, outboxInsert{
			EventType: workflow.EventToolGroupCompleted, ConversationID: conversationID,
			TurnID: record.TurnID, TurnRunID: record.TurnRunID, ExecutionGroup: record.ExecutionGroup,
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tool call settlement: %w", err)
	}
	return &workflow.ToolCallSettlement{Record: record, GroupComplete: pending == 0}, nil
}

func (r *ToolCallRepository) AdvanceToolExecutionGroup(ctx context.Context, turnRunID string, executionGroup int) (*workflow.ToolGroupAdvance, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tool group advancement: %w", err)
	}
	defer tx.Rollback(ctx)
	var turnID, conversationID, runStatus string
	if err := tx.QueryRow(ctx, `
		SELECT tr.turn_id::text, t.conversation_id::text, tr.status
		FROM turn_runs tr JOIN turns t ON t.id = tr.turn_id
		WHERE tr.id = $1::uuid
		FOR UPDATE OF tr
	`, turnRunID).Scan(&turnID, &conversationID, &runStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("lock tool group run: %w", err)
	}
	if runStatus != domain.TurnRunStatusWaitingTools {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit handled tool group: %w", err)
		}
		return &workflow.ToolGroupAdvance{}, nil
	}
	var pending int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM tool_calls
		WHERE turn_run_id = $1::uuid AND execution_group = $2
		  AND status IN ($3, $4, $5)
	`, turnRunID, executionGroup, domain.ToolCallStatusQueued, domain.ToolCallStatusRunning, domain.ToolCallStatusAwaitingInput).Scan(&pending); err != nil {
		return nil, fmt.Errorf("count current tool group: %w", err)
	}
	if pending != 0 {
		return nil, domain.ErrConflict
	}
	var nextGroup int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(min(execution_group), $2)
		FROM tool_calls WHERE turn_run_id = $1::uuid AND execution_group > $2
	`, turnRunID, executionGroup).Scan(&nextGroup); err != nil {
		return nil, fmt.Errorf("find next tool group: %w", err)
	}
	var hasNext bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tool_calls WHERE turn_run_id = $1::uuid AND execution_group > $2)
	`, turnRunID, executionGroup).Scan(&hasNext); err != nil {
		return nil, fmt.Errorf("check next tool group: %w", err)
	}
	if !hasNext {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit final tool group: %w", err)
		}
		return &workflow.ToolGroupAdvance{LastGroup: true}, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text FROM tool_calls
		WHERE turn_run_id = $1::uuid AND execution_group = $2 AND status = $3
		ORDER BY execution_ordinal ASC, id ASC
	`, turnRunID, nextGroup, domain.ToolCallStatusQueued)
	if err != nil {
		return nil, fmt.Errorf("load next tool group: %w", err)
	}
	for rows.Next() {
		var toolCallID string
		if err := rows.Scan(&toolCallID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan next tool call: %w", err)
		}
		if err := insertOutboxEvent(ctx, tx, outboxInsert{
			EventType: workflow.EventToolCallRequested, ConversationID: conversationID,
			TurnID: turnID, TurnRunID: turnRunID, ToolCallID: toolCallID, ExecutionGroup: nextGroup,
		}); err != nil {
			rows.Close()
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate next tool group: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit next tool group: %w", err)
	}
	return &workflow.ToolGroupAdvance{}, nil
}

func (r *ToolCallRepository) ListToolCallsByRun(ctx context.Context, turnRunID string) ([]domain.ToolCallRecord, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+toolCallColumns+`
		FROM tool_calls tc
		WHERE tc.turn_run_id = $1::uuid
		ORDER BY execution_group ASC, execution_ordinal ASC, created_at ASC, id ASC
	`, turnRunID)
	if err != nil {
		return nil, fmt.Errorf("list tool calls by run: %w", err)
	}
	defer rows.Close()
	var records []domain.ToolCallRecord
	for rows.Next() {
		record, err := scanToolCall(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tool call by run: %w", err)
		}
		records = append(records, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tool calls by run: %w", err)
	}
	return records, nil
}

func listToolCallsByRun(ctx context.Context, tx pgx.Tx, turnRunID string) ([]domain.ToolCallRecord, error) {
	rows, err := tx.Query(ctx, `
		SELECT `+toolCallColumns+`
		FROM tool_calls tc
		WHERE tc.turn_run_id = $1::uuid
		ORDER BY execution_group ASC, execution_ordinal ASC, created_at ASC, id ASC
	`, turnRunID)
	if err != nil {
		return nil, fmt.Errorf("list tool calls in transaction: %w", err)
	}
	defer rows.Close()
	var records []domain.ToolCallRecord
	for rows.Next() {
		record, err := scanToolCall(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tool call in transaction: %w", err)
		}
		records = append(records, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tool calls in transaction: %w", err)
	}
	return records, nil
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func leaseInterval(timeout time.Duration) string {
	if timeout <= 0 {
		timeout = time.Minute
	}
	return timeout.String()
}

var _ workflow.ToolExecutionWorkflowStore = (*ToolCallRepository)(nil)
