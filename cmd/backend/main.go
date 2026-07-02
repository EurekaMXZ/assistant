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
	if err := cfg.ValidateBackend(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	openCtx, cancel := context.WithTimeout(ctx, cfg.ShutdownTimeout)
	defer cancel()

	rt, err := assistantbootstrap.NewBackend(openCtx, log.Default(), cfg)
	if err != nil {
		log.Fatalf("bootstrap runtime: %v", err)
	}
	defer rt.Close()

	log.Printf("backend listening on %s", rt.Address())
	if err := rt.Run(ctx, cfg.ShutdownTimeout); err != nil {
		log.Fatalf("backend stopped: %v", err)
	}
}
