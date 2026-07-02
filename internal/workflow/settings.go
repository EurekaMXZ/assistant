package workflow

import "time"

type WorkflowSettings struct {
	AgentSystemPrompt        string
	AgentCompactPrompt       string
	RemoteToolReplayMaxBytes int
	CompactMaxOutputTokens   int
	CompactTriggerTokens     int
	WorkerLeaseTimeout       time.Duration
	OutboxBatchSize          int
}
