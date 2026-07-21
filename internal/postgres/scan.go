package postgres

import (
	"database/sql"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/workflow"
)

type scanRow interface {
	Scan(dest ...any) error
}

func scanConversation(row scanRow) (*domain.Conversation, error) {
	var (
		conversation domain.Conversation
		ownerUserID  sql.NullString
		title        sql.NullString
		archivedAt   sql.NullTime
		deletedAt    sql.NullTime
		metadata     []byte
	)

	if err := row.Scan(
		&conversation.ID,
		&ownerUserID,
		&title,
		&conversation.Status,
		&metadata,
		&conversation.CreatedAt,
		&conversation.UpdatedAt,
		&archivedAt,
		&deletedAt,
	); err != nil {
		return nil, err
	}

	if ownerUserID.Valid {
		conversation.OwnerUserID = ownerUserID.String
	}
	if title.Valid {
		conversation.Title = title.String
	}
	if archivedAt.Valid {
		conversation.ArchivedAt = &archivedAt.Time
	}
	if deletedAt.Valid {
		conversation.DeletedAt = &deletedAt.Time
	}
	conversation.Metadata = cloneJSON(metadata)

	return &conversation, nil
}

func scanUser(row scanRow) (*domain.User, error) {
	var (
		user            domain.User
		lastLoginAt     sql.NullTime
		emailVerifiedAt sql.NullTime
		deletedAt       sql.NullTime
	)

	if err := row.Scan(
		&user.ID,
		&user.Email,
		&user.Username,
		&user.PasswordHash,
		&user.Role,
		&user.Status,
		&lastLoginAt,
		&emailVerifiedAt,
		&user.AuthVersion,
		&user.StorageQuotaBytes,
		&user.StorageUsedBytes,
		&deletedAt,
		&user.CreatedAt,
		&user.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if lastLoginAt.Valid {
		user.LastLoginAt = &lastLoginAt.Time
	}
	if emailVerifiedAt.Valid {
		user.EmailVerifiedAt = &emailVerifiedAt.Time
	}
	if deletedAt.Valid {
		user.DeletedAt = &deletedAt.Time
	}

	return &user, nil
}

func scanContextHead(row scanRow) (*domain.ContextHead, error) {
	var head domain.ContextHead
	if err := row.Scan(
		&head.ConversationID,
		&head.Version,
		&head.AnchorGeneration,
		&head.AnchorKey,
		&head.CoveredUntilSeq,
		&head.RawTailStartSeq,
		&head.LastSeq,
		&head.ActiveContextTokens,
		&head.LatestRequestRunID,
		&head.LatestSuccessfulRunID,
		&head.LatestCheckpointKey,
		&head.LatestCheckpointChecksum,
		&head.CheckpointCoveredEventSeq,
		&head.LastContextEventSeq,
		&head.ContextSchemaVersion,
		&head.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &head, nil
}

func scanTurn(row scanRow) (*domain.Turn, error) {
	var (
		turn        domain.Turn
		metadata    []byte
		startedAt   sql.NullTime
		completedAt sql.NullTime
		failedAt    sql.NullTime
	)

	if err := row.Scan(
		&turn.ID,
		&turn.ConversationID,
		&turn.Seq,
		&turn.RetryOfTurnID,
		&turn.VariantIndex,
		&turn.Status,
		&turn.RequestBlobKey,
		&turn.ResponseBlobKey,
		&turn.StreamBlobKey,
		&turn.OpenAIResponseID,
		&turn.ErrorCode,
		&turn.ErrorMessage,
		&metadata,
		&startedAt,
		&completedAt,
		&failedAt,
		&turn.CreatedAt,
		&turn.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if startedAt.Valid {
		turn.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		turn.CompletedAt = &completedAt.Time
	}
	if failedAt.Valid {
		turn.FailedAt = &failedAt.Time
	}
	turn.Metadata = cloneJSON(metadata)

	return &turn, nil
}

func scanMessage(row scanRow) (*domain.Message, error) {
	var (
		message    domain.Message
		tokenCount sql.NullInt64
		metadata   []byte
	)

	if err := row.Scan(
		&message.ID,
		&message.ConversationID,
		&message.TurnID,
		&message.Seq,
		&message.Role,
		&message.ContentText,
		&tokenCount,
		&metadata,
		&message.ContextExcluded,
		&message.CreatedAt,
	); err != nil {
		return nil, err
	}

	if tokenCount.Valid {
		value := int(tokenCount.Int64)
		message.TokenCount = &value
	}
	message.Metadata = cloneJSON(metadata)

	return &message, nil
}

func scanOutboxEvent(row scanRow) (*workflow.OutboxEvent, error) {
	var (
		event       workflow.OutboxEvent
		publishedAt sql.NullTime
		claimedAt   sql.NullTime
	)

	if err := row.Scan(
		&event.ID,
		&event.EventType,
		&event.ConversationID,
		&event.TurnID,
		&event.TurnRunID,
		&publishedAt,
		&event.ClaimToken,
		&claimedAt,
		&event.ErrorMessage,
		&event.CreatedAt,
	); err != nil {
		return nil, err
	}

	if publishedAt.Valid {
		event.PublishedAt = &publishedAt.Time
	}
	if claimedAt.Valid {
		event.ClaimedAt = &claimedAt.Time
	}

	return &event, nil
}

func scanTurnRun(row scanRow) (*domain.TurnRun, error) {
	var (
		run           domain.TurnRun
		completedAt   sql.NullTime
		failedAt      sql.NullTime
		heartbeatAt   sql.NullTime
		cancelledAt   sql.NullTime
		billingAmount sql.NullInt64
		requestSize   sql.NullInt64
		responseSize  sql.NullInt64
	)

	if err := row.Scan(
		&run.ID,
		&run.TurnID,
		&run.StepIndex,
		&run.Provider,
		&run.Model,
		&run.Status,
		&run.RequestBlobKey,
		&run.ResponseBlobKey,
		&run.OutputItemsBlobKey,
		&run.ToolResultsBlobKey,
		&run.PresentationEventsBlobKey,
		&run.CheckpointBlobKey,
		&run.FailureBlobKey,
		&run.ArtifactMetadata,
		&run.RequestChecksum,
		&run.ResponseChecksum,
		&requestSize,
		&responseSize,
		&run.RequestSchemaVersion,
		&run.ResponseSchemaVersion,
		&run.ResponseID,
		&run.InputTokens,
		&run.CacheReadInputTokens,
		&run.CacheCreationInputTokens,
		&run.OutputTokens,
		&run.ReasoningOutputTokens,
		&run.TotalTokens,
		&run.BillingCurrency,
		&billingAmount,
		&run.ErrorMessage,
		&run.StartedAt,
		&completedAt,
		&failedAt,
		&run.CreatedAt,
		&run.UpdatedAt,
		&run.Attempt,
		&run.StateBlobKey,
		&run.ResultBlobKey,
		&heartbeatAt,
		&cancelledAt,
	); err != nil {
		return nil, err
	}

	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	if failedAt.Valid {
		run.FailedAt = &failedAt.Time
	}
	if heartbeatAt.Valid {
		run.HeartbeatAt = &heartbeatAt.Time
	}
	if cancelledAt.Valid {
		run.CancelledAt = &cancelledAt.Time
	}
	if requestSize.Valid {
		run.RequestSizeBytes = requestSize.Int64
	}
	if responseSize.Valid {
		run.ResponseSizeBytes = responseSize.Int64
	}
	if billingAmount.Valid {
		value := billingAmount.Int64
		run.BillingAmountNanos = &value
	}

	return &run, nil
}

func scanToolCall(row scanRow) (*domain.ToolCallRecord, error) {
	var (
		record      domain.ToolCallRecord
		namespace   sql.NullString
		completedAt sql.NullTime
		failedAt    sql.NullTime
	)

	if err := row.Scan(
		&record.ID,
		&record.TurnID,
		&record.TurnRunID,
		&record.CallID,
		&record.ToolType,
		&namespace,
		&record.ToolName,
		&record.Status,
		&record.ExecutionAttempt,
		&record.ArgumentsBlobKey,
		&record.OutputBlobKey,
		&record.ErrorMessage,
		&record.StartedAt,
		&completedAt,
		&failedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if namespace.Valid {
		record.Namespace = namespace.String
	}
	if completedAt.Valid {
		record.CompletedAt = &completedAt.Time
	}
	if failedAt.Valid {
		record.FailedAt = &failedAt.Time
	}

	return &record, nil
}

func scanTurnStreamEvent(row scanRow) (*domain.TurnStreamEvent, error) {
	var (
		event   domain.TurnStreamEvent
		payload []byte
	)

	if err := row.Scan(
		&event.ID,
		&event.TurnID,
		&event.ConversationID,
		&event.EventIndex,
		&event.EventType,
		&payload,
		&event.CreatedAt,
	); err != nil {
		return nil, err
	}

	event.Payload = cloneJSON(payload)
	return &event, nil
}

func scanConversationSandbox(row scanRow) (*domain.ConversationSandbox, error) {
	var (
		sandbox               domain.ConversationSandbox
		metadata              []byte
		stoppedAt             sql.NullTime
		destroyedAt           sql.NullTime
		executionToken        sql.NullString
		executionLeaseUntil   sql.NullTime
		releasePreviousStatus sql.NullString
		releaseToken          sql.NullString
		releaseLeaseUntil     sql.NullTime
	)

	if err := row.Scan(
		&sandbox.ID,
		&sandbox.ConversationID,
		&sandbox.Provider,
		&sandbox.RuntimeID,
		&sandbox.Status,
		&metadata,
		&sandbox.LastActivityAt,
		&sandbox.CreatedAt,
		&sandbox.UpdatedAt,
		&stoppedAt,
		&destroyedAt,
		&executionToken,
		&executionLeaseUntil,
		&releasePreviousStatus,
		&releaseToken,
		&releaseLeaseUntil,
	); err != nil {
		return nil, err
	}

	sandbox.RuntimeMetadata = cloneJSON(metadata)
	if stoppedAt.Valid {
		sandbox.StoppedAt = &stoppedAt.Time
	}
	if destroyedAt.Valid {
		sandbox.DestroyedAt = &destroyedAt.Time
	}
	if executionToken.Valid {
		sandbox.ExecutionToken = executionToken.String
	}
	if executionLeaseUntil.Valid {
		sandbox.ExecutionLeaseUntil = &executionLeaseUntil.Time
	}
	if releasePreviousStatus.Valid {
		sandbox.ReleasePreviousStatus = releasePreviousStatus.String
	}
	if releaseToken.Valid {
		sandbox.ReleaseToken = releaseToken.String
	}
	if releaseLeaseUntil.Valid {
		sandbox.ReleaseLeaseUntil = &releaseLeaseUntil.Time
	}

	return &sandbox, nil
}

func scanAttachment(row scanRow) (*domain.Attachment, error) {
	var (
		attachment domain.Attachment
		metadata   []byte
	)

	if err := row.Scan(
		&attachment.ID,
		&attachment.ConversationID,
		&attachment.UploadedByUserID,
		&attachment.Filename,
		&attachment.ContentType,
		&attachment.Category,
		&attachment.SizeBytes,
		&attachment.SHA256,
		&attachment.ContentMD5,
		&attachment.Status,
		&attachment.ObjectKey,
		&metadata,
		&attachment.UploadCompletedAt,
		&attachment.CreatedAt,
		&attachment.UpdatedAt,
	); err != nil {
		return nil, err
	}

	attachment.Metadata = cloneJSON(metadata)
	return &attachment, nil
}

func scanStorageAttachment(row scanRow) (*domain.StorageAttachment, error) {
	var (
		item              domain.StorageAttachment
		metadata          []byte
		conversationTitle sql.NullString
	)
	if err := row.Scan(
		&item.ID,
		&item.ConversationID,
		&item.UploadedByUserID,
		&item.Filename,
		&item.ContentType,
		&item.Category,
		&item.SizeBytes,
		&item.SHA256,
		&item.ContentMD5,
		&item.Status,
		&item.ObjectKey,
		&metadata,
		&item.UploadCompletedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
		&conversationTitle,
	); err != nil {
		return nil, err
	}
	item.Metadata = cloneJSON(metadata)
	if conversationTitle.Valid {
		item.ConversationTitle = conversationTitle.String
	}
	return &item, nil
}
