package bootstrap

import (
	"context"
	"fmt"
	"io"

	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	"github.com/EurekaMXZ/assistant/internal/credential"
	"github.com/EurekaMXZ/assistant/internal/objectstore"
	"github.com/EurekaMXZ/assistant/internal/postgres"
	"github.com/EurekaMXZ/assistant/internal/server"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/EurekaMXZ/assistant/internal/tool"
	"github.com/jackc/pgx/v5/pgxpool"
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

	artifactStore, err := objectstore.New(settings.ObjectStore)
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
	if err := ensureSandboxProvidersConfigured(ctx, pool, sandboxRuntime); err != nil {
		lifecycle.close()
		return nil, err
	}

	streamHub, err := buildStreamHub(ctx, settings.Stream)
	if err != nil {
		lifecycle.close()
		return nil, err
	}
	lifecycle.addClose(func() {
		_ = streamHub.Close()
	})
	serverUseCases, workflows := buildApplication(pool, artifactStore, artifactStore, streamHub, settings.BillingCurrency, authService, sandboxRuntime, settings.SandboxLifecycle, credentialCipher, settings.Server.WebOrigin)
	assembled := &baseAssembly{
		resources: lifecycle,
		server:    serverUseCases,
		streamHub: streamHub,
		workflows: workflows,
		address:   settings.Address,
	}

	return assembled, nil
}

type sandboxProviderSet interface {
	SupportsProvider(provider string) bool
}

func ensureSandboxProvidersConfigured(ctx context.Context, pool *pgxpool.Pool, runtime tool.SandboxManager) error {
	repository := postgres.NewConversationSandboxRepository(pool)
	providers, err := repository.ListNonDestroyedSandboxProviders(ctx)
	if err != nil {
		return err
	}
	configured, ok := runtime.(sandboxProviderSet)
	if !ok {
		return fmt.Errorf("sandbox runtime does not expose configured providers")
	}
	for _, provider := range providers {
		if !configured.SupportsProvider(provider) {
			return fmt.Errorf("sandbox provider %q has non-destroyed database records but is not configured", provider)
		}
	}
	return nil
}
