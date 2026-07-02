package firecrackerbridge

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHandlerAuth(t *testing.T) {
	service, err := New(Settings{
		Token:           "bridge-token",
		FirecrackerBin:  "firecracker",
		KernelImagePath: "/tmp/vmlinux",
		RootFSImagePath: "/tmp/rootfs.ext4",
		RuntimeDir:      t.TempDir(),
		VCPUCount:       1,
		MemSizeMIB:      128,
		AgentPort:       52,
	}, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	health := httptest.NewRecorder()
	service.Handler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}

	protected := httptest.NewRecorder()
	service.Handler().ServeHTTP(protected, httptest.NewRequest(http.MethodPost, "/sandboxes", nil))
	if protected.Code != http.StatusUnauthorized {
		t.Fatalf("protected status = %d, want 401", protected.Code)
	}
}

func TestNetworkForSandboxAllocatesGuestAddress(t *testing.T) {
	service, err := New(Settings{
		FirecrackerBin:  "firecracker",
		KernelImagePath: "/tmp/vmlinux",
		RootFSImagePath: "/tmp/rootfs.ext4",
		RuntimeDir:      t.TempDir(),
		VCPUCount:       1,
		MemSizeMIB:      128,
		AgentPort:       52,
		BootTimeout:     time.Second,
		NetworkEnabled:  true,
		NetworkBridge:   "fcbr0",
		NetworkCIDR:     "172.16.0.1/24",
		NetworkSubnet:   "172.16.0.0/24",
		NetworkGateway:  "172.16.0.1",
		NetworkIface:    "eth0",
		GuestIPStart:    100,
	}, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	network, err := service.networkForSandbox(&sandbox{id: "fc-1234567890abcdef", guestCID: 5})
	if err != nil {
		t.Fatalf("network for sandbox: %v", err)
	}
	if network.guestIP != "172.16.0.102" || network.guestAddress != "172.16.0.102/24" {
		t.Fatalf("unexpected guest address: %+v", network)
	}
	if network.tapName != "tap1234567890a" {
		t.Fatalf("tapName = %q, want tap1234567890a", network.tapName)
	}
	if network.guestMAC != "02:fc:ac:10:00:66" {
		t.Fatalf("guestMAC = %q, want 02:fc:ac:10:00:66", network.guestMAC)
	}
}

func TestCopyRootFSCreatesIndependentSandboxImage(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "rootfs-template.ext4")
	if err := os.WriteFile(srcPath, []byte("original-rootfs"), 0o640); err != nil {
		t.Fatalf("write source rootfs: %v", err)
	}

	dstPath := filepath.Join(t.TempDir(), "sandbox", "rootfs.ext4")
	if err := copyRootFS(srcPath, dstPath); err != nil {
		t.Fatalf("copy rootfs: %v", err)
	}

	gotDst, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read copied rootfs: %v", err)
	}
	if string(gotDst) != "original-rootfs" {
		t.Fatalf("copied rootfs = %q, want original-rootfs", string(gotDst))
	}

	if err := os.WriteFile(dstPath, []byte("sandbox-mutated"), 0o640); err != nil {
		t.Fatalf("mutate copied rootfs: %v", err)
	}
	gotSrc, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read source rootfs: %v", err)
	}
	if string(gotSrc) != "original-rootfs" {
		t.Fatalf("source rootfs = %q, want original-rootfs", string(gotSrc))
	}
}
