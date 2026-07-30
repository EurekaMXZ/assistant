package workflow

import "time"

type WorkflowSettings struct {
	AgentSystemPrompt       string
	AgentCompactPrompt      string
	CompactMaxOutputTokens  int
	CompactTriggerTokens    int
	TokenEstimateMultiplier int
	WorkerLeaseTimeout      time.Duration
	LLMClientLeaseTimeout   time.Duration
	ExecutionLeaseTimeout   time.Duration
	OutboxBatchSize         int
	ImageGenerationPartials int
	ImagePreviewTTL         time.Duration
}
