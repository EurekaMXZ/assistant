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

func (r *trackingSandboxRuntime) ExecSandboxCommand(_ context.Context, handle domain.SandboxHandle, _ domain.SandboxCommandRequest, _ string) (*domain.SandboxCommandResult, error) {
	r.execCalls++
	return &domain.SandboxCommandResult{RuntimeID: handle.RuntimeID}, nil
}

func TestManagerCreatesWithDefaultProviderAndRoutesPersistedHandles(t *testing.T) {
	firecracker := &trackingSandboxRuntime{provider: ProviderFirecracker}
	agentBay := &trackingSandboxRuntime{provider: ProviderAgentBay}
	manager, err := NewManager(ProviderAgentBay, map[string]tool.SandboxManager{
		ProviderFirecracker: firecracker,
		ProviderAgentBay:    agentBay,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	handle, err := manager.CreateSandbox(context.Background(), "conv-1", "create-key")
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	if handle.Provider != ProviderAgentBay || agentBay.createCalls != 1 || firecracker.createCalls != 0 {
		t.Fatalf("unexpected create routing: handle=%#v firecracker=%d agentbay=%d", handle, firecracker.createCalls, agentBay.createCalls)
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
	manager, err := NewManager(ProviderAgentBay, map[string]tool.SandboxManager{
		ProviderAgentBay: &trackingSandboxRuntime{provider: ProviderAgentBay},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	_, err = manager.DestroySandbox(context.Background(), domain.SandboxHandle{Provider: ProviderFirecracker, RuntimeID: "fc-1"}, "")
	if err == nil || !strings.Contains(err.Error(), `sandbox provider "firecracker" is not configured`) {
		t.Fatalf("error = %v, want unconfigured provider", err)
	}
}
