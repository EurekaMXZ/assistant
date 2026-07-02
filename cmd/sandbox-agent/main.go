package main

import (
	"log"
	"net/http"
	"os"

	"github.com/EurekaMXZ/assistant/internal/sandboxagent"
)

func main() {
	logger := log.New(os.Stdout, "sandbox-agent ", log.LstdFlags|log.LUTC)
	settings := sandboxagent.LoadSettingsFromEnv()
	listener, err := sandboxagent.ListenVsock(settings.Port)
	if err != nil {
		logger.Fatalf("listen: %v", err)
	}
	logger.Printf("listening on vsock port %d", settings.Port)
	if err := http.Serve(listener, sandboxagent.NewHandler(settings)); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("serve: %v", err)
	}
}
