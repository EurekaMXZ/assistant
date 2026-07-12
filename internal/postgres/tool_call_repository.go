package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
	"github.com/jackc/pgx/v5"
)

func (r *ToolCallRepository) AcquireToolCall(ctx context.Context, turnID string, turnRunID string, executionAttempt int, call tool.ToolCall, argumentsBlobKey string) (*domain.ToolCallRecord, bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, false, fmt.Errorf("begin tool call acquisition: %w", err)
	}
	defer tx.Rollback(ctx)

	var insertedID string
	err = tx.QueryRow(ctx, `
		INSERT INTO tool_calls (
			turn_id,
			turn_run_id,
			call_id,
			tool_type,
			namespace,
			tool_name,
			status,
			execution_attempt,
			arguments_blob_key
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (turn_run_id, call_id) DO NOTHING
		RETURNING id::text
	`, turnID, turnRunID, call.CallID, normalizedToolCallType(call.Type), nullableText(call.Namespace), call.Name,
		domain.ToolCallStatusRunning, executionAttempt, argumentsBlobKey).Scan(&insertedID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("insert tool call: %w", err)
	}

	row := tx.QueryRow(ctx, `
		SELECT
			id::text,
			turn_id::text,
			turn_run_id::text,
			call_id,
			tool_type,
			namespace,
			tool_name,
			status,
			execution_attempt,
			arguments_blob_key,
			COALESCE(output_blob_key, ''),
			COALESCE(error_message, ''),
			started_at,
			completed_at,
			failed_at,
			created_at,
			updated_at
		FROM tool_calls
		WHERE turn_run_id = $1::uuid AND call_id = $2
		FOR UPDATE
	`, turnRunID, call.CallID)

	record, err := scanToolCall(row)
	if err != nil {
		return nil, false, fmt.Errorf("load acquired tool call: %w", err)
	}
	acquired := insertedID != ""
	if !acquired && record.Status == domain.ToolCallStatusRunning && record.ExecutionAttempt < executionAttempt {
		row = tx.QueryRow(ctx, `
			UPDATE tool_calls
			SET status = $2, error_message = $3, failed_at = now(), completed_at = NULL
			WHERE id = $1::uuid AND status = $4 AND execution_attempt < $5
			RETURNING
				id::text, turn_id::text, turn_run_id::text, call_id, tool_type, namespace,
				tool_name, status, execution_attempt, arguments_blob_key,
				COALESCE(output_blob_key, ''), COALESCE(error_message, ''), started_at,
				completed_at, failed_at, created_at, updated_at
		`, record.ID, domain.ToolCallStatusAmbiguous,
			"previous execution ended without a durable tool result", domain.ToolCallStatusRunning, executionAttempt)
		record, err = scanToolCall(row)
		if err != nil {
			return nil, false, fmt.Errorf("mark tool call outcome ambiguous: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit tool call acquisition: %w", err)
	}
	return record, acquired, nil
}

func normalizedToolCallType(value string) string {
	switch value {
	case llm.ModelItemFunctionCall:
		return llm.ModelToolTypeFunction
	case llm.ModelItemMCPCall, llm.ModelItemMCPApprovalRequest:
		return llm.ModelToolTypeMCP
	default:
		return value
	}
}

func (r *ToolCallRepository) CompleteToolCall(ctx context.Context, recordID string, outputBlobKey string) (*domain.ToolCallRecord, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE tool_calls
		SET
			status = $2,
			output_blob_key = $3,
			error_message = NULL,
			completed_at = now(),
			failed_at = NULL
		WHERE id = $1::uuid AND status = $4
		RETURNING
			id::text,
			turn_id::text,
			turn_run_id::text,
			call_id,
			tool_type,
			namespace,
			tool_name,
			status,
			execution_attempt,
			arguments_blob_key,
			COALESCE(output_blob_key, ''),
			COALESCE(error_message, ''),
			started_at,
			completed_at,
			failed_at,
			created_at,
			updated_at
	`, recordID, domain.ToolCallStatusCompleted, outputBlobKey, domain.ToolCallStatusRunning)

	record, err := scanToolCall(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("complete tool call: %w", err)
	}

	return record, nil
}

func (r *ToolCallRepository) FailToolCall(ctx context.Context, recordID string, outputBlobKey string, message string) (*domain.ToolCallRecord, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE tool_calls
		SET
			status = $2,
			output_blob_key = $3,
			error_message = $4,
			completed_at = NULL,
			failed_at = now()
		WHERE id = $1::uuid AND status = $5
		RETURNING
			id::text,
			turn_id::text,
			turn_run_id::text,
			call_id,
			tool_type,
			namespace,
			tool_name,
			status,
			execution_attempt,
			arguments_blob_key,
			COALESCE(output_blob_key, ''),
			COALESCE(error_message, ''),
			started_at,
			completed_at,
			failed_at,
			created_at,
			updated_at
	`, recordID, domain.ToolCallStatusFailed, outputBlobKey, message, domain.ToolCallStatusRunning)

	record, err := scanToolCall(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("fail tool call: %w", err)
	}

	return record, nil
}

func (r *ToolCallRepository) ListToolCallsByTurn(ctx context.Context, turnID string) ([]domain.ToolCallRecord, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			turn_id::text,
			turn_run_id::text,
			call_id,
			tool_type,
			namespace,
			tool_name,
			status,
			execution_attempt,
			arguments_blob_key,
			COALESCE(output_blob_key, ''),
			COALESCE(error_message, ''),
			started_at,
			completed_at,
			failed_at,
			created_at,
			updated_at
		FROM tool_calls
		WHERE turn_id = $1::uuid
		ORDER BY created_at ASC, id ASC
	`, turnID)
	if err != nil {
		return nil, fmt.Errorf("list tool calls: %w", err)
	}
	defer rows.Close()

	records := make([]domain.ToolCallRecord, 0)
	for rows.Next() {
		record, scanErr := scanToolCall(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan tool call: %w", scanErr)
		}
		records = append(records, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tool calls: %w", err)
	}

	return records, nil
}
