package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	assistantbootstrap "github.com/EurekaMXZ/assistant/internal/bootstrap"
	"github.com/EurekaMXZ/assistant/internal/config"
)

func main() {
	cfg := config.Load()
	if err := cfg.ValidateWorker(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	openCtx, cancel := context.WithTimeout(ctx, cfg.ShutdownTimeout)
	defer cancel()

	rt, err := assistantbootstrap.NewWorker(openCtx, log.Default(), cfg)
	if err != nil {
		log.Fatalf("bootstrap runtime: %v", err)
	}
	defer rt.Close()

	if err := rt.Run(ctx); err != nil {
		log.Fatalf("worker stopped: %v", err)
	}
}
