package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

type fakeServer struct {
	listenErr      error
	listenCalled   int
	shutdownCalled int
	closeCalled    int
	shutdownCh     chan struct{}
	blockShutdown  bool
}

func (s *fakeServer) ListenAndServe() error {
	s.listenCalled++
	if s.shutdownCh != nil {
		<-s.shutdownCh
	}
	if s.listenErr != nil {
		return s.listenErr
	}
	return http.ErrServerClosed
}

func (s *fakeServer) Shutdown(ctx context.Context) error {
	s.shutdownCalled++
	if s.blockShutdown {
		<-ctx.Done()
		return ctx.Err()
	}
	if s.shutdownCh != nil {
		select {
		case <-s.shutdownCh:
		default:
			close(s.shutdownCh)
		}
	}
	return nil
}

func (s *fakeServer) Close() error {
	s.closeCalled++
	if s.shutdownCh != nil {
		select {
		case <-s.shutdownCh:
		default:
			close(s.shutdownCh)
		}
	}
	return nil
}

type fakeWorker struct {
	runCalled int
	runErr    error
	runCh     chan struct{}
	ignoreCtx bool
}

func (w *fakeWorker) Run(ctx context.Context) error {
	w.runCalled++
	if w.runCh != nil {
		if w.ignoreCtx {
			<-w.runCh
			return w.runErr
		}
		select {
		case <-w.runCh:
		case <-ctx.Done():
			if w.runErr != nil {
				return w.runErr
			}
			return nil
		}
	}
	return w.runErr
}

func TestAPIRuntimeRunReturnsServerError(t *testing.T) {
	rt := &APIRuntime{
		server:  &fakeServer{listenErr: errors.New("boom")},
		address: ":8080",
	}

	err := rt.Run(context.Background(), time.Second)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("error = %v, want boom", err)
	}
}

func TestAPIRuntimeRunShutsDownOnContextCancel(t *testing.T) {
	server := &fakeServer{shutdownCh: make(chan struct{})}
	stopCalled := 0
	rt := &APIRuntime{
		server: server,
		stopServer: func() {
			stopCalled++
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- rt.Run(ctx, time.Second)
	}()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime to stop")
	}

	if server.shutdownCalled != 1 {
		t.Fatalf("shutdown called = %d, want 1", server.shutdownCalled)
	}
	if stopCalled != 1 {
		t.Fatalf("stopServer called = %d, want 1", stopCalled)
	}
}

func TestBackendRuntimeRunDelegatesWorkerError(t *testing.T) {
	worker := &fakeWorker{runErr: errors.New("worker failed")}
	rt := &BackendRuntime{
		worker: worker,
	}

	err := rt.Run(context.Background(), time.Second)
	if err == nil || err.Error() != "worker failed" {
		t.Fatalf("error = %v, want worker failed", err)
	}
	if worker.runCalled != 1 {
		t.Fatalf("run called = %d, want 1", worker.runCalled)
	}
}

func TestBackendRuntimeRunCancelsRequestsAndShutsDownOnContextCancel(t *testing.T) {
	server := &fakeServer{shutdownCh: make(chan struct{})}
	worker := &fakeWorker{runCh: make(chan struct{})}
	stopCalled := 0
	rt := &BackendRuntime{
		server: server,
		worker: worker,
		stopServer: func() {
			stopCalled++
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- rt.Run(ctx, time.Second)
	}()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for backend runtime to stop")
	}

	if server.shutdownCalled != 1 {
		t.Fatalf("shutdown called = %d, want 1", server.shutdownCalled)
	}
	if worker.runCalled != 1 {
		t.Fatalf("worker run called = %d, want 1", worker.runCalled)
	}
	if stopCalled != 1 {
		t.Fatalf("stopServer called = %d, want 1", stopCalled)
	}
}

func TestBackendRuntimeUsesOneShutdownDeadline(t *testing.T) {
	server := &fakeServer{shutdownCh: make(chan struct{}), blockShutdown: true}
	worker := &fakeWorker{runCh: make(chan struct{}), ignoreCtx: true}
	rt := &BackendRuntime{server: server, worker: worker}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	err := rt.Run(ctx, 40*time.Millisecond)
	elapsed := time.Since(started)
	close(worker.runCh)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want shutdown deadline exceeded", err)
	}
	if elapsed >= 100*time.Millisecond {
		t.Fatalf("shutdown elapsed = %v, want one shared deadline", elapsed)
	}
	if server.closeCalled != 1 {
		t.Fatalf("server close called = %d, want 1", server.closeCalled)
	}
}
