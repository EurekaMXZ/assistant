package bootstrap

import (
	"context"
	"fmt"
	"log"

	"github.com/EurekaMXZ/assistant/internal/config"
	"github.com/EurekaMXZ/assistant/internal/server"
)

func NewAPI(ctx context.Context, logger *log.Logger, cfg config.Config) (*APIRuntime, error) {
	base := newBaseSettings(cfg, true)
	assembled, err := newBaseAssembly(ctx, base)
	if err != nil {
		return nil, err
	}
	serverBaseCtx, stopServer := context.WithCancel(context.Background())
	return &APIRuntime{
		resources:  assembled.resources,
		server:     buildServer(base.Server, assembled.server, assembled.streamHub, serverBaseCtx),
		stopServer: stopServer,
		address:    assembled.address,
	}, nil
}

func NewWorker(ctx context.Context, logger *log.Logger, cfg config.Config) (*WorkerRuntime, error) {
	loaded, err := cfg.LoadPrompts()
	if err != nil {
		return nil, fmt.Errorf("load workflow prompts: %w", err)
	}
	cfg = loaded
	base := newBaseSettings(cfg, false)
	assembled, err := newBaseAssembly(ctx, base)
	if err != nil {
		return nil, err
	}
	workerService, err := buildWorker(ctx, logger, newWorkerSettings(cfg), assembled.workflows, assembled.streamHub)
	if err != nil {
		assembled.resources.close()
		return nil, err
	}
	return &WorkerRuntime{
		resources: assembled.resources,
		worker:    workerService,
	}, nil
}

func NewBackend(ctx context.Context, logger *log.Logger, cfg config.Config) (*BackendRuntime, error) {
	loaded, err := cfg.LoadPrompts()
	if err != nil {
		return nil, fmt.Errorf("load workflow prompts: %w", err)
	}
	cfg = loaded
	base := newBaseSettings(cfg, true)
	assembled, err := newBaseAssembly(ctx, base)
	if err != nil {
		return nil, err
	}
	workerService, err := buildWorker(ctx, logger, newWorkerSettings(cfg), assembled.workflows, assembled.streamHub)
	if err != nil {
		assembled.resources.close()
		return nil, err
	}
	serverBaseCtx, stopServer := context.WithCancel(context.Background())
	return &BackendRuntime{
		resources:  assembled.resources,
		server:     buildServer(base.Server, assembled.server, assembled.streamHub, serverBaseCtx),
		worker:     workerService,
		stopServer: stopServer,
		address:    assembled.address,
	}, nil
}

func buildServer(settings server.Settings, useCases server.UseCases, streamHub turnStreamSubscriber, baseCtx context.Context) serverRunner {
	return server.New(settings, useCases, streamHub, baseCtx)
}
