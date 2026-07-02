package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type serverRunner interface {
	ListenAndServe() error
	Shutdown(ctx context.Context) error
	Close() error
}

type workerRunner interface {
	Run(ctx context.Context) error
}

func (r *APIRuntime) Address() string {
	if r == nil {
		return ""
	}
	return r.address
}

func (r *APIRuntime) Run(ctx context.Context, shutdownTimeout time.Duration) error {
	if r == nil {
		return nil
	}
	return runHTTPServer(ctx, r.server, r.stopServer, shutdownTimeout)
}

func (r *WorkerRuntime) Run(ctx context.Context) error {
	if r == nil || r.worker == nil {
		return nil
	}
	return r.worker.Run(ctx)
}

func (r *BackendRuntime) Address() string {
	if r == nil {
		return ""
	}
	return r.address
}

func (r *BackendRuntime) Run(ctx context.Context, shutdownTimeout time.Duration) error {
	if r == nil {
		return nil
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	serverErr := make(chan error, 1)
	if r.server != nil {
		go func() {
			serverErr <- r.server.ListenAndServe()
		}()
	}

	workerErr := make(chan error, 1)
	if r.worker != nil {
		go func() {
			workerErr <- r.worker.Run(runCtx)
		}()
	}

	select {
	case <-ctx.Done():
		return shutdownBackend(r.server, r.stopServer, workerErr, shutdownTimeout)
	case err := <-serverErr:
		cancelRun()
		shutdownErr := shutdownBackend(r.server, r.stopServer, workerErr, shutdownTimeout)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return errors.Join(err, shutdownErr)
		}
		return shutdownErr
	case err := <-workerErr:
		cancelRun()
		return errors.Join(err, shutdownBackend(r.server, r.stopServer, nil, shutdownTimeout))
	}
}

func runHTTPServer(ctx context.Context, server serverRunner, stopServer func(), shutdownTimeout time.Duration) error {
	if server == nil {
		return nil
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		return shutdownHTTPServer(server, stopServer, shutdownTimeout)
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func shutdownHTTPServer(server serverRunner, stopServer func(), shutdownTimeout time.Duration) error {
	if stopServer != nil {
		stopServer()
	}
	if server == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	err := server.Shutdown(shutdownCtx)
	if err != nil && shutdownCtx.Err() != nil {
		_ = server.Close()
	}
	return err
}

func shutdownBackend(server serverRunner, stopServer func(), workerErr <-chan error, shutdownTimeout time.Duration) error {
	if stopServer != nil {
		stopServer()
	}
	if shutdownTimeout <= 0 {
		shutdownTimeout = time.Second
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	httpErr := make(chan error, 1)
	if server == nil {
		httpErr <- nil
	} else {
		go func() {
			httpErr <- server.Shutdown(shutdownCtx)
		}()
	}
	if workerErr == nil {
		closed := make(chan error, 1)
		closed <- nil
		workerErr = closed
	}

	var (
		serverDone bool
		workerDone bool
		result     error
	)
	for !serverDone || !workerDone {
		select {
		case err := <-httpErr:
			serverDone = true
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				result = errors.Join(result, err)
			}
		case err := <-workerErr:
			workerDone = true
			if err != nil {
				result = errors.Join(result, err)
			}
		case <-shutdownCtx.Done():
			if server != nil {
				_ = server.Close()
			}
			return errors.Join(result, shutdownCtx.Err())
		}
	}
	return result
}
