package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/EurekaMXZ/assistant/internal/firecrackerbridge"
)

func main() {
	logger := log.New(os.Stdout, "firecracker-bridge ", log.LstdFlags|log.LUTC)
	settings := firecrackerbridge.LoadSettingsFromEnv()
	service, err := firecrackerbridge.New(settings, logger)
	if err != nil {
		logger.Fatalf("configure bridge: %v", err)
	}

	server := &http.Server{
		Addr:              settings.Address,
		Handler:           service.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Printf("listening on %s", settings.Address)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Printf("shutdown: %v", err)
	}
	service.Shutdown()
}
