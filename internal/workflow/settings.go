package workflow

import "time"

type WorkflowSettings struct {
	AgentSystemPrompt       string
	AgentCompactPrompt      string
	CompactMaxOutputTokens  int
	CompactTriggerTokens    int
	WorkerLeaseTimeout      time.Duration
	OutboxBatchSize         int
	ImageGenerationPartials int
	ImagePreviewTTL         time.Duration
}
