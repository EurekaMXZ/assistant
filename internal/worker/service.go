package worker

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/segmentio/kafka-go"
)

const defaultWriterCloseTimeout = 2 * time.Second

type workflowEngine interface {
	FlushOutbox(ctx context.Context, publish workflow.WorkflowEventPublisher) error
	RequeueStaleTurns(ctx context.Context) (int, error)
	HandleWorkflowEvent(ctx context.Context, event workflow.WorkflowEvent) error
}

type WorkflowWriter interface {
	WriteMessages(ctx context.Context, messages ...kafka.Message) error
	Close() error
}

type WorkflowReader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, messages ...kafka.Message) error
	Close() error
}

type ReaderFactory func() WorkflowReader

type MaintenanceTask interface {
	Run(ctx context.Context) error
}

type Service struct {
	logger      *log.Logger
	settings    Settings
	engine      workflowEngine
	writer      WorkflowWriter
	newReader   ReaderFactory
	sleep       func(time.Duration)
	closeWait   time.Duration
	maintenance []MaintenanceTask
}

func New(logger *log.Logger, engine workflowEngine, settings Settings, writer WorkflowWriter, newReader ReaderFactory, maintenance ...MaintenanceTask) *Service {
	return &Service{
		logger:      logger,
		settings:    settings,
		engine:      engine,
		writer:      writer,
		newReader:   newReader,
		sleep:       time.Sleep,
		maintenance: append([]MaintenanceTask(nil), maintenance...),
	}
}

func (s *Service) Run(ctx context.Context) error {
	defer s.closeWriter()

	llmActors, executionActors := s.actorCounts()
	s.logger.Printf("worker service starting with llm_actors=%d execution_actors=%d", llmActors, executionActors)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.relayLoop(ctx)
	}()

	for _, task := range s.maintenance {
		if task == nil {
			continue
		}
		wg.Add(1)
		go func(task MaintenanceTask) {
			defer wg.Done()
			if err := task.Run(ctx); err != nil && ctx.Err() == nil && s.logger != nil {
				s.logger.Printf("maintenance task stopped: %v", err)
			}
		}(task)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.requeueLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.consume(ctx, llmActors, executionActors)
	}()

	<-ctx.Done()
	wg.Wait()
	return nil
}

func (s *Service) actorCounts() (int, int) {
	llmActors := s.settings.LLMClientActors
	executionActors := s.settings.ExecutionActors
	if llmActors > 0 && executionActors > 0 {
		return llmActors, executionActors
	}
	legacyBudget := s.settings.WorkerActorBudget
	if legacyBudget <= 0 {
		legacyBudget = s.settings.WorkerConcurrency
	}
	if legacyBudget <= 0 {
		legacyBudget = 2
	}
	legacyLLM, legacyExecution := splitActorCounts(legacyBudget)
	if llmActors <= 0 {
		llmActors = legacyLLM
	}
	if executionActors <= 0 {
		executionActors = legacyExecution
	}
	return llmActors, executionActors
}

func splitActorCounts(budget int) (int, int) {
	if budget < 2 {
		return 1, 1
	}
	llmActors := budget / 2
	if llmActors < 1 {
		llmActors = 1
	}
	executionActors := budget - llmActors
	if executionActors < 1 {
		executionActors = 1
	}
	return llmActors, executionActors
}

func (s *Service) workflowReader() WorkflowReader {
	if s.newReader != nil {
		return s.newReader()
	}
	return nil
}

func (s *Service) sleepFor(delay time.Duration) {
	if s.sleep != nil {
		s.sleep(delay)
		return
	}
	time.Sleep(delay)
}

func (s *Service) closeWriter() {
	if s.writer == nil {
		return
	}
	timeout := s.closeWait
	if timeout <= 0 {
		timeout = defaultWriterCloseTimeout
	}
	done := make(chan error, 1)
	go func() {
		done <- s.writer.Close()
	}()
	select {
	case err := <-done:
		if err != nil && s.logger != nil {
			s.logger.Printf("close workflow writer: %v", err)
		}
	case <-time.After(timeout):
		if s.logger != nil {
			s.logger.Printf("close workflow writer timed out after %s", timeout)
		}
	}
}
