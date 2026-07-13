package bootstrap

import (
	"context"
	"io"

	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	"github.com/EurekaMXZ/assistant/internal/credential"
	"github.com/EurekaMXZ/assistant/internal/minio"
	"github.com/EurekaMXZ/assistant/internal/postgres"
	"github.com/EurekaMXZ/assistant/internal/server"
	"github.com/EurekaMXZ/assistant/internal/stream"
)

type turnStreamSubscriber interface {
	SubscribeEvents(ctx context.Context, turnID string) (io.Closer, <-chan stream.Event, error)
}

type streamRuntime interface {
	stream.Publisher
	turnStreamSubscriber
	io.Closer
}

type baseAssembly struct {
	resources *resources
	server    server.UseCases
	streamHub streamRuntime
	workflows workflowAdapters
	address   string
}

func newBaseAssembly(ctx context.Context, settings baseSettings) (*baseAssembly, error) {
	lifecycle := &resources{}

	pool, err := postgres.OpenPool(ctx, settings.DatabaseURL)
	if err != nil {
		return nil, err
	}
	lifecycle.addClose(pool.Close)

	artifactStore, err := minio.New(settings.MinIO)
	if err != nil {
		lifecycle.close()
		return nil, err
	}

	credentialCipher, err := credential.NewCipher(settings.ProviderCredentialMasterKey)
	if err != nil {
		lifecycle.close()
		return nil, err
	}

	var authService *assistantauth.Service
	if settings.EnableAuth {
		tokenService, err := assistantauth.NewTokenService(settings.Auth)
		if err != nil {
			lifecycle.close()
			return nil, err
		}
		authService = &assistantauth.Service{
			Users:      postgres.NewUserRepository(pool),
			Tokens:     tokenService,
			SystemUser: settings.SystemUser,
		}
		if _, err := authService.BootstrapSystemUser(ctx); err != nil {
			lifecycle.close()
			return nil, err
		}
	}

	sandboxRuntime, err := buildSandboxRuntime(settings.Sandbox)
	if err != nil {
		lifecycle.close()
		return nil, err
	}

	serverUseCases, workflows := buildApplication(pool, artifactStore, artifactStore, settings.BillingCurrency, authService, sandboxRuntime, credentialCipher, settings.Server.WebOrigin)
	assembled := &baseAssembly{
		resources: lifecycle,
		server:    serverUseCases,
		workflows: workflows,
		address:   settings.Address,
	}

	stream, err := buildStreamHub(ctx, settings.Stream)
	if err != nil {
		assembled.resources.close()
		return nil, err
	}
	assembled.resources.addClose(func() {
		_ = stream.Close()
	})
	assembled.streamHub = stream

	return assembled, nil
}
