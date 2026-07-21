package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/jackc/pgx/v5"
)

const toolCallColumns = `
	tc.id::text,
	tc.turn_id::text,
	tc.turn_run_id::text,
	tc.call_id,
	tc.tool_type,
	tc.namespace,
	tc.tool_name,
	tc.status,
	tc.execution_attempt,
	tc.arguments_blob_key,
	COALESCE(tc.output_blob_key, ''),
	COALESCE(tc.error_message, ''),
	COALESCE(tc.answer_idempotency_key, ''),
	COALESCE(tc.answer_fingerprint, ''),
	COALESCE(tc.answer_option_id, ''),
	tc.answer_output_pending,
	tc.started_at,
	tc.completed_at,
	tc.failed_at,
	tc.cancelled_at,
	tc.created_at,
	tc.updated_at`

func (r *ToolCallRepository) AcquireToolCall(ctx context.Context, turnID string, turnRunID string, executionAttempt int, call tool.ToolCall, argumentsBlobKey string) (*domain.ToolCallRecord, bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, false, fmt.Errorf("begin tool call acquisition: %w", err)
	}
	defer tx.Rollback(ctx)

	prior, err := scanToolCall(tx.QueryRow(ctx, `
		SELECT `+toolCallColumns+`
		FROM tool_calls tc
		JOIN turn_runs prior_run ON prior_run.id = tc.turn_run_id
		JOIN turn_runs current_run ON current_run.id = $2::uuid
		WHERE tc.turn_id = $1::uuid
		  AND tc.call_id = $3
		  AND tc.turn_run_id <> $2::uuid
		  AND prior_run.step_index = current_run.step_index
		ORDER BY prior_run.attempt DESC
		LIMIT 1
		FOR UPDATE OF tc
	`, turnID, turnRunID, call.CallID))
	recoveredAcquired := false
	if err == nil {
		switch prior.Status {
		case domain.ToolCallStatusRunning:
			prior, err = scanToolCall(tx.QueryRow(ctx, `
				UPDATE tool_calls tc
				SET status = $2, error_message = $3, failed_at = now(), completed_at = NULL, cancelled_at = NULL
				WHERE id = $1::uuid AND status = $4
				RETURNING `+toolCallColumns+`
			`, prior.ID, domain.ToolCallStatusAmbiguous,
				"previous run attempt ended without a durable tool result", domain.ToolCallStatusRunning))
			if err != nil {
				return nil, false, fmt.Errorf("mark prior tool call ambiguous: %w", err)
			}
		case domain.ToolCallStatusAwaitingInput:
			prior, err = scanToolCall(tx.QueryRow(ctx, `
				UPDATE tool_calls tc
				SET turn_run_id = $2::uuid, execution_attempt = $3, status = $4,
					error_message = NULL, completed_at = NULL, failed_at = NULL, cancelled_at = NULL
				WHERE tc.id = $1::uuid AND tc.status = $5
					AND EXISTS (SELECT 1 FROM turn_runs tr WHERE tr.id = tc.turn_run_id AND tr.status = $6)
				RETURNING `+toolCallColumns,
				prior.ID, turnRunID, executionAttempt, domain.ToolCallStatusRunning,
				domain.ToolCallStatusAwaitingInput, domain.TurnRunStatusFailed))
			if err != nil {
				return nil, false, fmt.Errorf("recover prior awaiting tool call: %w", err)
			}
			recoveredAcquired = true
		}
		if prior != nil {
			if err := tx.Commit(ctx); err != nil {
				return nil, false, fmt.Errorf("commit prior tool call recovery: %w", err)
			}
			return prior, recoveredAcquired, nil
		}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("load prior tool call attempt: %w", err)
	}

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
		SELECT `+toolCallColumns+`
		FROM tool_calls tc
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
			UPDATE tool_calls tc
			SET status = $2, error_message = $3, failed_at = now(), completed_at = NULL, cancelled_at = NULL
			WHERE id = $1::uuid AND status = $4 AND execution_attempt < $5
			RETURNING `+toolCallColumns+`
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
		UPDATE tool_calls tc
		SET
			status = $2,
			output_blob_key = $3,
			error_message = NULL,
			completed_at = now(),
			failed_at = NULL,
			cancelled_at = NULL
		WHERE id = $1::uuid AND status = $4
		RETURNING `+toolCallColumns+`
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
		UPDATE tool_calls tc
		SET
			status = $2,
			output_blob_key = $3,
			error_message = $4,
			completed_at = NULL,
			failed_at = now(),
			cancelled_at = NULL
		WHERE id = $1::uuid AND status = $5
		RETURNING `+toolCallColumns+`
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

func (r *ToolCallRepository) MarkToolCallAmbiguous(ctx context.Context, recordID string, message string) (*domain.ToolCallRecord, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE tool_calls tc
		SET
			status = $2,
			output_blob_key = NULL,
			error_message = $3,
			completed_at = NULL,
			failed_at = now(),
			cancelled_at = NULL
		WHERE id = $1::uuid AND status = $4
		RETURNING `+toolCallColumns+`
	`, recordID, domain.ToolCallStatusAmbiguous, message, domain.ToolCallStatusRunning)

	record, err := scanToolCall(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("mark tool call ambiguous: %w", err)
	}
	return record, nil
}

func (r *ToolCallRepository) ListToolCallsByTurn(ctx context.Context, turnID string) ([]domain.ToolCallRecord, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+toolCallColumns+`
		FROM tool_calls tc
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

func (r *ToolCallRepository) GetToolCallForAnswer(ctx context.Context, ownerUserID string, turnID string, toolCallID string) (*domain.ToolCallRecord, error) {
	record, err := scanToolCall(r.pool.QueryRow(ctx, `
		SELECT `+toolCallColumns+`
		FROM tool_calls tc
		JOIN turns t ON t.id = tc.turn_id
		JOIN conversations c ON c.id = t.conversation_id
		WHERE tc.id = $1::uuid AND tc.turn_id = $2::uuid AND c.owner_user_id = $3::uuid
	`, toolCallID, turnID, ownerUserID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get tool call for answer: %w", err)
	}
	return record, nil
}

type lockedAskUserAnswer struct {
	conversationID string
	runID          string
	turnStatus     string
	runStatus      string
	record         *domain.ToolCallRecord
}

func lockAskUserAnswer(ctx context.Context, tx pgx.Tx, ownerUserID string, turnID string, toolCallID string) (*lockedAskUserAnswer, error) {
	locked := &lockedAskUserAnswer{}
	if err := tx.QueryRow(ctx, `
		SELECT t.conversation_id::text, t.status
		FROM turns t
		JOIN conversations c ON c.id = t.conversation_id
		WHERE t.id = $1::uuid AND c.owner_user_id = $2::uuid
		FOR UPDATE OF t
	`, turnID, ownerUserID).Scan(&locked.conversationID, &locked.turnStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("lock turn for tool answer: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT tr.id::text, tr.status
		FROM turn_runs tr
		WHERE tr.turn_id = $1::uuid
		  AND EXISTS (
			SELECT 1 FROM tool_calls tc
			WHERE tc.id = $2::uuid AND tc.turn_id = $1::uuid AND tc.turn_run_id = tr.id
		  )
		FOR UPDATE OF tr
	`, turnID, toolCallID).Scan(&locked.runID, &locked.runStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("lock turn run for tool answer: %w", err)
	}
	record, err := scanToolCall(tx.QueryRow(ctx, `
		SELECT `+toolCallColumns+`
		FROM tool_calls tc
		WHERE tc.id = $1::uuid AND tc.turn_id = $2::uuid AND tc.turn_run_id = $3::uuid
		FOR UPDATE OF tc
	`, toolCallID, turnID, locked.runID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("lock tool call answer: %w", err)
	}
	if record.ToolName != tool.AskUser || record.Namespace != "" {
		return nil, domain.ErrConflict
	}
	locked.record = record
	return locked, nil
}

func answerDeclarationMatches(record *domain.ToolCallRecord, answerKey string, answerFingerprint string, answerOptionID string, outputBlobKey string) bool {
	return record != nil &&
		record.AnswerKey == answerKey &&
		record.AnswerFingerprint == answerFingerprint &&
		record.AnswerOptionID == answerOptionID &&
		record.OutputBlobKey == outputBlobKey
}

func answerConflict() error {
	return domain.NewConflictError("Idempotency-Key was already used with a different answer")
}

func (r *ToolCallRepository) ClaimAwaitingInputAnswer(ctx context.Context, ownerUserID string, turnID string, toolCallID string, answerKey string, answerFingerprint string, answerOptionID string, outputBlobKey string) (*workflow.AskUserAnswerClaim, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin claim tool answer: %w", err)
	}
	defer tx.Rollback(ctx)

	locked, err := lockAskUserAnswer(ctx, tx, ownerUserID, turnID, toolCallID)
	if err != nil {
		return nil, err
	}
	if locked.record.Status == domain.ToolCallStatusCompleted {
		if !answerDeclarationMatches(locked.record, answerKey, answerFingerprint, answerOptionID, outputBlobKey) {
			return nil, answerConflict()
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit finalized tool answer claim: %w", err)
		}
		return &workflow.AskUserAnswerClaim{ToolCall: locked.record, ConversationID: locked.conversationID, Finalized: true}, nil
	}
	if locked.record.Status != domain.ToolCallStatusAwaitingInput ||
		locked.runStatus != domain.TurnRunStatusAwaitingInput ||
		locked.turnStatus != domain.TurnStatusAwaitingInput {
		return nil, domain.ErrConflict
	}
	if locked.record.AnswerKey != "" {
		if !answerDeclarationMatches(locked.record, answerKey, answerFingerprint, answerOptionID, outputBlobKey) {
			return nil, answerConflict()
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit existing tool answer claim: %w", err)
		}
		return &workflow.AskUserAnswerClaim{ToolCall: locked.record, ConversationID: locked.conversationID}, nil
	}

	record, err := scanToolCall(tx.QueryRow(ctx, `
		UPDATE tool_calls tc
		SET answer_idempotency_key = $2, answer_fingerprint = $3, answer_option_id = $4,
			output_blob_key = $5, answer_output_pending = true
		WHERE tc.id = $1::uuid AND tc.status = $6 AND tc.answer_idempotency_key IS NULL
		RETURNING `+toolCallColumns,
		toolCallID, answerKey, answerFingerprint, answerOptionID, outputBlobKey, domain.ToolCallStatusAwaitingInput))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("declare tool answer: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tool answer claim: %w", err)
	}
	return &workflow.AskUserAnswerClaim{ToolCall: record, ConversationID: locked.conversationID}, nil
}

func (r *ToolCallRepository) FinalizeAwaitingInputAnswer(ctx context.Context, ownerUserID string, turnID string, toolCallID string, answerKey string, answerFingerprint string, answerOptionID string, outputBlobKey string, interaction json.RawMessage) (*domain.ToolCallRecord, bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, false, fmt.Errorf("begin finalize tool answer: %w", err)
	}
	defer tx.Rollback(ctx)

	locked, err := lockAskUserAnswer(ctx, tx, ownerUserID, turnID, toolCallID)
	if err != nil {
		return nil, false, err
	}
	if locked.record.Status == domain.ToolCallStatusCompleted {
		if !answerDeclarationMatches(locked.record, answerKey, answerFingerprint, answerOptionID, outputBlobKey) {
			return nil, false, answerConflict()
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, false, fmt.Errorf("commit replayed tool answer: %w", err)
		}
		return locked.record, true, nil
	}
	if locked.record.Status != domain.ToolCallStatusAwaitingInput ||
		locked.runStatus != domain.TurnRunStatusAwaitingInput ||
		locked.turnStatus != domain.TurnStatusAwaitingInput ||
		!locked.record.AnswerOutputPending ||
		!answerDeclarationMatches(locked.record, answerKey, answerFingerprint, answerOptionID, outputBlobKey) {
		return nil, false, domain.ErrConflict
	}

	head, err := queryContextHeadForUpdate(ctx, tx, locked.conversationID)
	if err != nil {
		return nil, false, err
	}
	if err := insertCompleteEvent(ctx, tx, head, domain.ConversationEventInput{
		ConversationID:  locked.conversationID,
		TurnID:          turnID,
		TurnRunID:       locked.runID,
		EventKey:        "run:" + locked.runID + ":" + domain.ConversationEventInteractionCompleted + ":" + toolCallID + ":completed",
		SchemaVersion:   1,
		EventType:       domain.ConversationEventInteractionCompleted,
		Payload:         interaction,
		ContextIncluded: false,
	}); err != nil {
		return nil, false, err
	}

	record, err := scanToolCall(tx.QueryRow(ctx, `
		UPDATE tool_calls tc
		SET status = $2, answer_output_pending = false,
			error_message = NULL, completed_at = now(), failed_at = NULL, cancelled_at = NULL
		WHERE tc.id = $1::uuid AND tc.status = $3 AND tc.answer_output_pending = true
		RETURNING `+toolCallColumns,
		toolCallID, domain.ToolCallStatusCompleted, domain.ToolCallStatusAwaitingInput))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, domain.ErrConflict
		}
		return nil, false, fmt.Errorf("complete answered tool call: %w", err)
	}
	result, err := tx.Exec(ctx, `
		UPDATE turn_runs
		SET status = $2, lease_token = NULL, heartbeat_at = NULL, error_message = NULL
		WHERE id = $1::uuid AND status = $3
	`, locked.runID, domain.TurnRunStatusQueued, domain.TurnRunStatusAwaitingInput)
	if err != nil {
		return nil, false, fmt.Errorf("queue answered turn run: %w", err)
	}
	if result.RowsAffected() != 1 {
		return nil, false, domain.ErrConflict
	}
	result, err = tx.Exec(ctx, `
		UPDATE turns SET status = $2
		WHERE id = $1::uuid AND status = $3
	`, turnID, domain.TurnStatusProcessing, domain.TurnStatusAwaitingInput)
	if err != nil {
		return nil, false, fmt.Errorf("resume answered turn: %w", err)
	}
	if result.RowsAffected() != 1 {
		return nil, false, domain.ErrConflict
	}
	if err := insertTurnRunRequestedEvent(ctx, tx, locked.conversationID, turnID, locked.runID, 0); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit tool call answer: %w", err)
	}
	return record, false, nil
}
