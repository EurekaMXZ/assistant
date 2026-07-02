package firecrackerbridge

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddress        = "127.0.0.1:8787"
	defaultFirecrackerBin = "firecracker"
	defaultKernelBootArgs = "console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw"
	defaultRuntimeDir     = "/tmp/assistant-firecracker"
	defaultVCPUCount      = 1
	defaultMemSizeMIB     = 512
	defaultAgentPort      = 52
	defaultBootTimeout    = 20 * time.Second
	defaultNetworkBridge  = "fcbr0"
	defaultNetworkCIDR    = "172.16.0.1/24"
	defaultNetworkSubnet  = "172.16.0.0/24"
	defaultNetworkGateway = "172.16.0.1"
	defaultNetworkIface   = "eth0"
	defaultGuestIPStart   = 100
)

type Settings struct {
	Address         string
	Token           string
	FirecrackerBin  string
	KernelImagePath string
	InitrdImagePath string
	RootFSImagePath string
	KernelBootArgs  string
	RuntimeDir      string
	VCPUCount       int
	MemSizeMIB      int
	AgentPort       uint32
	BootTimeout     time.Duration
	KeepFailed      bool
	NetworkEnabled  bool
	NetworkBridge   string
	NetworkCIDR     string
	NetworkSubnet   string
	NetworkGateway  string
	NetworkIface    string
	NetworkDNS      []string
	GuestIPStart    int
}

func LoadSettingsFromEnv() Settings {
	return Settings{
		Address:         getenv("FIRECRACKER_BRIDGE_ADDR", defaultAddress),
		Token:           os.Getenv("FIRECRACKER_BRIDGE_TOKEN"),
		FirecrackerBin:  getenv("FIRECRACKER_BIN", defaultFirecrackerBin),
		KernelImagePath: os.Getenv("FIRECRACKER_KERNEL_IMAGE"),
		InitrdImagePath: os.Getenv("FIRECRACKER_INITRD_IMAGE"),
		RootFSImagePath: os.Getenv("FIRECRACKER_ROOTFS_IMAGE"),
		KernelBootArgs:  getenv("FIRECRACKER_KERNEL_BOOT_ARGS", defaultKernelBootArgs),
		RuntimeDir:      getenv("FIRECRACKER_RUNTIME_DIR", defaultRuntimeDir),
		VCPUCount:       getenvInt("FIRECRACKER_VCPU_COUNT", defaultVCPUCount),
		MemSizeMIB:      getenvInt("FIRECRACKER_MEM_SIZE_MIB", defaultMemSizeMIB),
		AgentPort:       uint32(getenvInt("FIRECRACKER_AGENT_PORT", defaultAgentPort)),
		BootTimeout:     getenvDuration("FIRECRACKER_BOOT_TIMEOUT", defaultBootTimeout),
		KeepFailed:      getenvBool("FIRECRACKER_KEEP_FAILED_SANDBOXES", false),
		NetworkEnabled:  getenvBool("FIRECRACKER_NET_ENABLED", false),
		NetworkBridge:   getenv("FIRECRACKER_NET_BRIDGE", defaultNetworkBridge),
		NetworkCIDR:     getenv("FIRECRACKER_NET_CIDR", defaultNetworkCIDR),
		NetworkSubnet:   getenv("FIRECRACKER_NET_SUBNET", defaultNetworkSubnet),
		NetworkGateway:  getenv("FIRECRACKER_NET_GATEWAY", defaultNetworkGateway),
		NetworkIface:    getenv("FIRECRACKER_NET_GUEST_IFACE", defaultNetworkIface),
		NetworkDNS:      getenvList("FIRECRACKER_NET_DNS", []string{"223.5.5.5", "119.29.29.29"}),
		GuestIPStart:    getenvInt("FIRECRACKER_NET_GUEST_IP_START", defaultGuestIPStart),
	}
}

func (s Settings) Validate() error {
	if strings.TrimSpace(s.FirecrackerBin) == "" {
		return errors.New("FIRECRACKER_BIN is required")
	}
	if strings.TrimSpace(s.KernelImagePath) == "" {
		return errors.New("FIRECRACKER_KERNEL_IMAGE is required")
	}
	if strings.TrimSpace(s.RootFSImagePath) == "" {
		return errors.New("FIRECRACKER_ROOTFS_IMAGE is required")
	}
	if strings.TrimSpace(s.RuntimeDir) == "" {
		return errors.New("FIRECRACKER_RUNTIME_DIR is required")
	}
	if s.VCPUCount <= 0 {
		return errors.New("FIRECRACKER_VCPU_COUNT must be positive")
	}
	if s.MemSizeMIB <= 0 {
		return errors.New("FIRECRACKER_MEM_SIZE_MIB must be positive")
	}
	if s.AgentPort == 0 {
		return errors.New("FIRECRACKER_AGENT_PORT must be positive")
	}
	if s.NetworkEnabled {
		if strings.TrimSpace(s.NetworkBridge) == "" {
			return errors.New("FIRECRACKER_NET_BRIDGE is required when networking is enabled")
		}
		if strings.TrimSpace(s.NetworkCIDR) == "" {
			return errors.New("FIRECRACKER_NET_CIDR is required when networking is enabled")
		}
		if strings.TrimSpace(s.NetworkSubnet) == "" {
			return errors.New("FIRECRACKER_NET_SUBNET is required when networking is enabled")
		}
		if strings.TrimSpace(s.NetworkGateway) == "" {
			return errors.New("FIRECRACKER_NET_GATEWAY is required when networking is enabled")
		}
		if strings.TrimSpace(s.NetworkIface) == "" {
			return errors.New("FIRECRACKER_NET_GUEST_IFACE is required when networking is enabled")
		}
		if s.GuestIPStart < 2 || s.GuestIPStart > 250 {
			return errors.New("FIRECRACKER_NET_GUEST_IP_START must be between 2 and 250")
		}
	}
	return nil
}

func getenv(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getenvBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func getenvList(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return append([]string(nil), fallback...)
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			values = append(values, item)
		}
	}
	if len(values) == 0 {
		return append([]string(nil), fallback...)
	}
	return values
}
