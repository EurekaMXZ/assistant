package sandbox

import (
	"context"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

type trackingSandboxRuntime struct {
	provider     string
	createCalls  int
	destroyCalls int
	execCalls    int
}

func (r *trackingSandboxRuntime) CreateSandbox(context.Context, string, string) (*domain.SandboxHandle, error) {
	r.createCalls++
	return &domain.SandboxHandle{Provider: r.provider, RuntimeID: r.provider + "-runtime"}, nil
}

func (r *trackingSandboxRuntime) DestroySandbox(_ context.Context, handle domain.SandboxHandle, _ string) (*domain.SandboxHandle, error) {
	r.destroyCalls++
	return &handle, nil
}

func (r *trackingSandboxRuntime) StopSandbox(_ context.Context, handle domain.SandboxHandle, _ string) (*domain.SandboxHandle, error) {
	return &handle, nil
}

func (r *trackingSandboxRuntime) ResumeSandbox(_ context.Context, handle domain.SandboxHandle, _ string) (*domain.SandboxHandle, error) {
	return &handle, nil
}

func (r *trackingSandboxRuntime) ExecSandboxCommand(_ context.Context, handle domain.SandboxHandle, _ domain.SandboxCommandRequest, _ string) (*domain.SandboxCommandResult, error) {
	r.execCalls++
	return &domain.SandboxCommandResult{RuntimeID: handle.RuntimeID}, nil
}

func TestManagerCreatesAndRoutesFirecrackerHandles(t *testing.T) {
	firecracker := &trackingSandboxRuntime{provider: ProviderFirecracker}
	manager, err := NewManager(ProviderFirecracker, map[string]tool.SandboxManager{
		ProviderFirecracker: firecracker,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	handle, err := manager.CreateSandbox(context.Background(), "conv-1", "create-key")
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	if handle.Provider != ProviderFirecracker || firecracker.createCalls != 1 {
		t.Fatalf("unexpected create routing: handle=%#v firecracker=%d", handle, firecracker.createCalls)
	}

	firecrackerHandle := domain.SandboxHandle{Provider: ProviderFirecracker, RuntimeID: "fc-1"}
	if _, err := manager.ExecSandboxCommand(context.Background(), firecrackerHandle, domain.SandboxCommandRequest{Command: "pwd"}, "exec-key"); err != nil {
		t.Fatalf("exec firecracker sandbox: %v", err)
	}
	if _, err := manager.DestroySandbox(context.Background(), firecrackerHandle, "destroy-key"); err != nil {
		t.Fatalf("destroy firecracker sandbox: %v", err)
	}
	if firecracker.execCalls != 1 || firecracker.destroyCalls != 1 {
		t.Fatalf("unexpected persisted handle routing: %#v", firecracker)
	}
}

func TestManagerCreatesCubeSandboxAndRoutesPersistedFirecrackerHandle(t *testing.T) {
	firecracker := &trackingSandboxRuntime{provider: ProviderFirecracker}
	cube := &trackingSandboxRuntime{provider: ProviderCubeSandbox}
	manager, err := NewManager(ProviderCubeSandbox, map[string]tool.SandboxManager{
		ProviderFirecracker: firecracker,
		ProviderCubeSandbox: cube,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	handle, err := manager.CreateSandbox(t.Context(), "conv-1", "create-key")
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	if handle.Provider != ProviderCubeSandbox || cube.createCalls != 1 {
		t.Fatalf("unexpected create routing: handle=%#v cube=%d", handle, cube.createCalls)
	}
	if _, err := manager.DestroySandbox(t.Context(), domain.SandboxHandle{Provider: ProviderFirecracker, RuntimeID: "fc-1"}, "destroy-key"); err != nil {
		t.Fatalf("destroy persisted firecracker sandbox: %v", err)
	}
	if firecracker.destroyCalls != 1 {
		t.Fatalf("firecracker destroy calls = %d, want 1", firecracker.destroyCalls)
	}
	if !manager.SupportsProvider(ProviderFirecracker) || !manager.SupportsProvider(ProviderCubeSandbox) || manager.SupportsProvider("agentbay") {
		t.Fatal("unexpected configured provider set")
	}
}

func TestManagerCanonicalizesCreatedProvider(t *testing.T) {
	cube := &trackingSandboxRuntime{provider: " CubeSandbox "}
	manager, err := NewManager(ProviderCubeSandbox, map[string]tool.SandboxManager{ProviderCubeSandbox: cube})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	handle, err := manager.CreateSandbox(t.Context(), "conv-1", "")
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	if handle.Provider != ProviderCubeSandbox {
		t.Fatalf("provider = %q, want canonical provider", handle.Provider)
	}
}

func TestManagerRejectsUnconfiguredProvider(t *testing.T) {
	manager, err := NewManager(ProviderFirecracker, map[string]tool.SandboxManager{
		ProviderFirecracker: &trackingSandboxRuntime{provider: ProviderFirecracker},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	_, err = manager.DestroySandbox(context.Background(), domain.SandboxHandle{Provider: "agentbay", RuntimeID: "session-1"}, "")
	if err == nil || !strings.Contains(err.Error(), `sandbox provider "agentbay" is not configured`) {
		t.Fatalf("error = %v, want unconfigured provider", err)
	}
}

func TestNewRuntimeRejectsRemovedAgentBayProvider(t *testing.T) {
	_, err := NewRuntime(RuntimeSettings{
		Provider: "agentbay",
		HTTP:     HTTPRuntimeSettings{BaseURL: "http://127.0.0.1:8787"},
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported sandbox provider "agentbay"`) {
		t.Fatalf("error = %v, want removed AgentBay provider rejection", err)
	}
}
