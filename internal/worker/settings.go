package worker

import "time"

type Settings struct {
	WorkerConcurrency  int
	WorkerPollInterval time.Duration
	WorkerLeaseTimeout time.Duration
}
