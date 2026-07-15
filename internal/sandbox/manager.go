package sandbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

const (
	ProviderFirecracker = "firecracker"
	ProviderCubeSandbox = "cubesandbox"
)

var _ tool.SandboxManager = (*Manager)(nil)

type RuntimeSettings struct {
	Provider string
	HTTP     HTTPRuntimeSettings
	Cube     CubeRuntimeSettings
}

type Manager struct {
	defaultProvider string
	providers       map[string]tool.SandboxManager
}

func NewManager(defaultProvider string, providers map[string]tool.SandboxManager) (*Manager, error) {
	defaultProvider = normalizeProvider(defaultProvider)
	if defaultProvider == "" {
		defaultProvider = ProviderFirecracker
	}

	configured := make(map[string]tool.SandboxManager, len(providers))
	for provider, runtime := range providers {
		provider = normalizeProvider(provider)
		if provider == "" || runtime == nil {
			continue
		}
		configured[provider] = runtime
	}
	if configured[defaultProvider] == nil {
		return nil, fmt.Errorf("sandbox provider %q is not configured", defaultProvider)
	}

	return &Manager{defaultProvider: defaultProvider, providers: configured}, nil
}

func NewRuntime(settings RuntimeSettings) (tool.SandboxManager, error) {
	provider := normalizeProvider(settings.Provider)
	if provider == "" {
		provider = ProviderFirecracker
	}
	if provider != ProviderFirecracker && provider != ProviderCubeSandbox {
		return nil, fmt.Errorf("unsupported sandbox provider %q", provider)
	}

	providers := make(map[string]tool.SandboxManager, 2)
	if strings.TrimSpace(settings.HTTP.BaseURL) != "" {
		runtime, err := NewHTTPRuntime(settings.HTTP)
		if err != nil {
			return nil, fmt.Errorf("configure firecracker sandbox runtime: %w", err)
		}
		providers[ProviderFirecracker] = runtime
	}
	if strings.TrimSpace(settings.Cube.APIURL) != "" {
		runtime, err := NewCubeRuntime(settings.Cube)
		if err != nil {
			return nil, fmt.Errorf("configure cube sandbox runtime: %w", err)
		}
		providers[ProviderCubeSandbox] = runtime
	}
	return NewManager(provider, providers)
}

func (m *Manager) CreateSandbox(ctx context.Context, conversationID string, requestKey string) (*domain.SandboxHandle, error) {
	runtime, err := m.runtime(m.defaultProvider)
	if err != nil {
		return nil, err
	}
	handle, err := runtime.CreateSandbox(ctx, conversationID, requestKey)
	if err != nil {
		return nil, err
	}
	if handle == nil {
		return nil, fmt.Errorf("sandbox provider %q returned an empty handle", m.defaultProvider)
	}
	if provider := normalizeProvider(handle.Provider); provider != m.defaultProvider {
		return nil, fmt.Errorf("sandbox provider %q returned handle for provider %q", m.defaultProvider, provider)
	}
	handle.Provider = m.defaultProvider
	return handle, nil
}

func (m *Manager) DestroySandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error) {
	runtime, err := m.runtime(handle.Provider)
	if err != nil {
		return nil, err
	}
	return runtime.DestroySandbox(ctx, handle, requestKey)
}

func (m *Manager) StopSandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error) {
	runtime, err := m.runtime(handle.Provider)
	if err != nil {
		return nil, err
	}
	return runtime.StopSandbox(ctx, handle, requestKey)
}

func (m *Manager) ResumeSandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error) {
	runtime, err := m.runtime(handle.Provider)
	if err != nil {
		return nil, err
	}
	return runtime.ResumeSandbox(ctx, handle, requestKey)
}

func (m *Manager) ExecSandboxCommand(ctx context.Context, handle domain.SandboxHandle, request domain.SandboxCommandRequest, requestKey string) (*domain.SandboxCommandResult, error) {
	runtime, err := m.runtime(handle.Provider)
	if err != nil {
		return nil, err
	}
	return runtime.ExecSandboxCommand(ctx, handle, request, requestKey)
}

func (m *Manager) runtime(provider string) (tool.SandboxManager, error) {
	if m == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	provider = normalizeProvider(provider)
	if provider == "" {
		return nil, fmt.Errorf("sandbox handle provider is required")
	}
	runtime := m.providers[provider]
	if runtime == nil {
		return nil, fmt.Errorf("sandbox provider %q is not configured", provider)
	}
	return runtime, nil
}

func (m *Manager) SupportsProvider(provider string) bool {
	if m == nil {
		return false
	}
	return m.providers[normalizeProvider(provider)] != nil
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
