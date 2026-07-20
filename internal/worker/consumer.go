package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/segmentio/kafka-go"
)

const maxQueuedWorkflowEvents = 1024

type workflowTask struct {
	message    kafka.Message
	event      workflow.WorkflowEvent
	offset     *trackedOffset
	decodeErr  error
	bypassLane bool
}

type workflowTaskResult struct {
	task *workflowTask
	err  error
}

type conversationLane struct {
	pending []*workflowTask
	running bool
	ready   bool
}

func (s *Service) consume(ctx context.Context, workerCount int) {
	reader := s.workflowReader()
	if reader == nil {
		s.logger.Print("worker missing workflow reader factory")
		return
	}
	var closeReaderOnce sync.Once
	closeReader := func() {
		closeReaderOnce.Do(func() {
			_ = reader.Close()
		})
	}
	defer closeReader()
	go func() {
		<-ctx.Done()
		closeReader()
	}()
	if workerCount <= 0 {
		workerCount = 1
	}

	fetched := make(chan kafka.Message, workerCount)
	jobs := make(chan *workflowTask)
	results := make(chan workflowTaskResult, workerCount)
	retries := make(chan string, maxQueuedWorkflowEvents)

	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		s.fetchWorkflowMessages(ctx, reader, fetched)
	}()
	for slot := 1; slot <= workerCount; slot++ {
		workers.Add(1)
		go func(slot int) {
			defer workers.Done()
			s.runWorkflowSlot(ctx, slot, jobs, results)
		}(slot)
	}
	defer workers.Wait()

	lanes := make(map[string]*conversationLane)
	ready := make([]string, 0)
	readyCancellations := make([]*workflowTask, 0)
	tracker := newOffsetTracker()
	queuedCount := 0
	commitTicker := time.NewTicker(time.Second)
	defer commitTicker.Stop()

	for {
		var nextJob *workflowTask
		var jobCh chan *workflowTask
		var fetchedCh <-chan kafka.Message
		if queuedCount < maxQueuedWorkflowEvents {
			fetchedCh = fetched
		}
		if len(readyCancellations) > 0 {
			nextJob = readyCancellations[0]
			jobCh = jobs
		} else if len(ready) > 0 {
			conversationID := ready[0]
			lane := lanes[conversationID]
			if lane == nil || lane.running || len(lane.pending) == 0 {
				ready = ready[1:]
				continue
			}
			nextJob = lane.pending[0]
			jobCh = jobs
		}
		if jobCh != nil {
			fetchedCh = nil
		}

		select {
		case <-ctx.Done():
			return
		case message := <-fetchedCh:
			offset, fresh := tracker.register(message)
			if !fresh {
				continue
			}
			task, err := decodeWorkflowTask(message, offset)
			if err != nil {
				s.logger.Printf("decode workflow event: %v", err)
				if task == nil {
					tracker.complete(offset)
					s.commitReadyOffsets(ctx, reader, tracker)
					continue
				}
				task.decodeErr = err
			}
			lane := lanes[task.event.ConversationID]
			if lane == nil {
				lane = &conversationLane{}
				lanes[task.event.ConversationID] = lane
			}
			lane.pending = append(lane.pending, task)
			queuedCount++
			if task.event.EventType == workflow.EventTurnCancellationRequested && lane.running {
				task.bypassLane = true
				lane.pending = lane.pending[:len(lane.pending)-1]
				readyCancellations = append(readyCancellations, task)
			} else {
				ready = enqueueReadyConversation(ready, task.event.ConversationID, lane)
			}
		case jobCh <- nextJob:
			if nextJob.bypassLane {
				readyCancellations = readyCancellations[1:]
				break
			}
			conversationID := nextJob.event.ConversationID
			lane := lanes[conversationID]
			lane.running = true
			lane.ready = false
			ready = ready[1:]
		case result := <-results:
			if result.task.bypassLane {
				conversationID := result.task.event.ConversationID
				if result.err != nil {
					s.logger.Printf("handle %s: %v", result.task.event.EventType, result.err)
					result.task.bypassLane = false
					lane := lanes[conversationID]
					if lane == nil {
						lane = &conversationLane{}
						lanes[conversationID] = lane
					}
					lane.pending = append([]*workflowTask{result.task}, lane.pending...)
					ready = enqueueReadyConversation(ready, conversationID, lane)
					continue
				}
				tracker.complete(result.task.offset)
				queuedCount--
				s.commitReadyOffsets(ctx, reader, tracker)
				continue
			}
			conversationID := result.task.event.ConversationID
			lane := lanes[conversationID]
			if lane == nil || len(lane.pending) == 0 || lane.pending[0] != result.task {
				continue
			}
			lane.running = false
			if result.err != nil {
				s.logger.Printf("handle %s: %v", result.task.event.EventType, result.err)
				go s.retryConversation(ctx, retries, conversationID)
				continue
			}

			tracker.complete(result.task.offset)
			lane.pending = lane.pending[1:]
			queuedCount--
			if len(lane.pending) == 0 {
				delete(lanes, conversationID)
			} else {
				ready = enqueueReadyConversation(ready, conversationID, lane)
			}
			s.commitReadyOffsets(ctx, reader, tracker)
		case conversationID := <-retries:
			lane := lanes[conversationID]
			ready = enqueueReadyConversation(ready, conversationID, lane)
		case <-commitTicker.C:
			s.commitReadyOffsets(ctx, reader, tracker)
		}
	}
}

func (s *Service) fetchWorkflowMessages(ctx context.Context, reader WorkflowReader, fetched chan<- kafka.Message) {
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.Printf("fetch workflow message: %v", err)
			s.sleepFor(time.Second)
			continue
		}

		select {
		case <-ctx.Done():
			return
		case fetched <- message:
		}
	}
}

func (s *Service) runWorkflowSlot(ctx context.Context, _ int, jobs <-chan *workflowTask, results chan<- workflowTaskResult) {
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-jobs:
			err := task.decodeErr
			if err == nil {
				err = s.engine.HandleWorkflowEvent(ctx, task.event)
			}
			select {
			case <-ctx.Done():
				return
			case results <- workflowTaskResult{task: task, err: err}:
			}
		}
	}
}

func (s *Service) retryConversation(ctx context.Context, retries chan<- string, conversationID string) {
	s.sleepFor(time.Second)
	select {
	case <-ctx.Done():
	case retries <- conversationID:
	}
}

func (s *Service) commitReadyOffsets(ctx context.Context, reader WorkflowReader, tracker *offsetTracker) {
	for _, message := range tracker.ready() {
		if err := reader.CommitMessages(ctx, message); err != nil {
			if ctx.Err() == nil {
				s.logger.Printf("commit workflow partition %s[%d] offset %d: %v", message.Topic, message.Partition, message.Offset, err)
			}
			tracker.commitFailed(message)
			continue
		}
		tracker.committed(message)
	}
}

func decodeWorkflowTask(message kafka.Message, offset *trackedOffset) (*workflowTask, error) {
	var event workflow.WorkflowEvent
	if err := json.Unmarshal(message.Value, &event); err != nil {
		return nil, err
	}
	if event.ConversationID == "" {
		return nil, fmt.Errorf("workflow event %s has no conversation_id", event.ID)
	}
	if len(message.Key) > 0 && string(message.Key) != event.ConversationID {
		return &workflowTask{message: message, event: event, offset: offset}, fmt.Errorf("workflow event %s key does not match conversation_id", event.ID)
	}
	return &workflowTask{message: message, event: event, offset: offset}, nil
}

func enqueueReadyConversation(ready []string, conversationID string, lane *conversationLane) []string {
	if lane == nil || lane.running || lane.ready || len(lane.pending) == 0 {
		return ready
	}
	lane.ready = true
	return append(ready, conversationID)
}
