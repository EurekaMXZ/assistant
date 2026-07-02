package domain

const (
	TurnErrorContextLoadFailed       = "context_load_failed"
	TurnErrorSandboxScopeFailed      = "sandbox_scope_failed"
	TurnErrorRequestPrepareFailed    = "request_prepare_failed"
	TurnErrorRequestBlobFailed       = "request_blob_failed"
	TurnErrorModelStreamFailed       = "model_stream_failed"
	TurnErrorUpstreamRequestFailed   = "upstream_request_failed"
	TurnErrorToolStepLimitExceeded   = "tool_step_limit_exceeded"
	TurnErrorBackendRequestFailed    = "backend_request_failed"
	TurnErrorResponseBlobFailed      = "response_blob_failed"
	TurnErrorGeneratedImageFailed    = "generated_image_failed"
	TurnErrorModelContextBlobFailed  = "model_context_blob_failed"
	TurnErrorTurnFinalizeFailed      = "turn_finalize_failed"
	TurnErrorBillingSettlementFailed = "billing_settlement_failed"
)

const (
	TurnPublicErrorUpstreamRequestFailed = "Upstream request failed"
	TurnPublicErrorToolStepLimitExceeded = "Tool call limit exceeded"
	TurnPublicErrorRequestProcessing     = "Request processing failed"
	TurnPublicErrorBillingRequired       = "Billing account is unavailable or has insufficient balance"
)
