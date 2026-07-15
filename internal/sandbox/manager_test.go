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
