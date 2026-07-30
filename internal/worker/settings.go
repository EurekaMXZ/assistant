package worker

import "time"

type Settings struct {
	WorkerActorBudget     int
	LLMClientActors       int
	ExecutionActors       int
	WorkerConcurrency     int
	WorkerPollInterval    time.Duration
	WorkerLeaseTimeout    time.Duration
	LLMClientPollInterval time.Duration
	ExecutionPollInterval time.Duration
	LLMClientLeaseTimeout time.Duration
	ExecutionLeaseTimeout time.Duration
}
