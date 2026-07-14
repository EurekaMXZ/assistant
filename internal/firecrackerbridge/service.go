package firecrackerbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
)

const (
	providerName        = "firecracker"
	sandboxStateActive  = "active"
	sandboxStateStopped = "stopped"
	sandboxManifestName = "sandbox.json"
)

type Service struct {
	settings Settings
	logger   *log.Logger

	mu        sync.Mutex
	nextCID   uint32
	sandboxes map[string]*sandbox
}

type sandbox struct {
	opMu           sync.Mutex
	id             string
	conversationID string
	dir            string
	rootFSPath     string
	apiSocketPath  string
	vsockPath      string
	logPath        string
	metricsPath    string
	bridgeName     string
	tapName        string
	guestIP        string
	guestAddress   string
	guestGateway   string
	guestMAC       string
	guestCID       uint32
	agentPort      uint32
	cmd            *exec.Cmd
	done           chan error
	createdAt      time.Time
	state          string
	stoppedAt      *time.Time
}

type sandboxManifest struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id"`
	GuestCID       uint32     `json:"guest_cid"`
	CreatedAt      time.Time  `json:"created_at"`
	State          string     `json:"state"`
	StoppedAt      *time.Time `json:"stopped_at,omitempty"`
}

func New(settings Settings, logger *log.Logger) (*Service, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	service := &Service{
		settings:  settings,
		logger:    logger,
		nextCID:   3,
		sandboxes: make(map[string]*sandbox),
	}
	if err := service.loadSandboxManifests(); err != nil {
		return nil, err
	}
	return service, nil
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/sandboxes", s.handleSandboxes)
	mux.HandleFunc("/sandboxes/", s.handleSandbox)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" && !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Service) Shutdown() {
	s.mu.Lock()
	sandboxes := make([]*sandbox, 0, len(s.sandboxes))
	for runtimeID, sb := range s.sandboxes {
		sandboxes = append(sandboxes, sb)
		delete(s.sandboxes, runtimeID)
	}
	s.mu.Unlock()
	for _, sb := range sandboxes {
		sb.opMu.Lock()
		if err := s.pauseSandboxResources(sb); err != nil {
			s.logger.Printf("stop sandbox %s during shutdown: %v", sb.id, err)
		}
		sb.opMu.Unlock()
	}
}

func (s *Service) authorized(r *http.Request) bool {
	token := strings.TrimSpace(s.settings.Token)
	if token == "" {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+token
}

func (s *Service) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Service) handleSandboxes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	conversationID := strings.TrimSpace(request.ConversationID)
	if conversationID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id is required")
		return
	}
	handle, err := s.createSandbox(r.Context(), conversationID)
	if err != nil {
		s.logger.Printf("create sandbox failed: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, handle)
}

func (s *Service) handleSandbox(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/sandboxes/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeError(w, http.StatusNotFound, "sandbox not found")
		return
	}
	runtimeID := parts[0]

	if len(parts) == 1 && r.Method == http.MethodDelete {
		handle, err := s.destroySandbox(runtimeID)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errSandboxNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, handle)
		return
	}

	if len(parts) == 2 && parts[1] == "exec" && r.Method == http.MethodPost {
		var request domain.SandboxCommandRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := s.execCommand(r.Context(), runtimeID, request)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errSandboxNotFound) {
				status = http.StatusNotFound
			} else if errors.Is(err, errBadRequest) {
				status = http.StatusBadRequest
			}
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	if len(parts) == 2 && parts[1] == "stop" && r.Method == http.MethodPost {
		handle, err := s.stopSandboxRuntime(r.Context(), runtimeID)
		if err != nil {
			writeSandboxLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, handle)
		return
	}

	if len(parts) == 2 && parts[1] == "resume" && r.Method == http.MethodPost {
		handle, err := s.resumeSandboxRuntime(r.Context(), runtimeID)
		if err != nil {
			writeSandboxLifecycleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, handle)
		return
	}

	writeError(w, http.StatusNotFound, "sandbox not found")
}

func (s *Service) createSandbox(ctx context.Context, conversationID string) (*domain.SandboxHandle, error) {
	if err := os.MkdirAll(s.settings.RuntimeDir, 0o750); err != nil {
		return nil, fmt.Errorf("create runtime dir: %w", err)
	}
	if err := syncDirectory(filepath.Dir(s.settings.RuntimeDir)); err != nil {
		return nil, fmt.Errorf("sync runtime dir parent: %w", err)
	}

	runtimeID := "fc-" + uuid.NewString()
	s.mu.Lock()
	guestCID := s.nextCID
	s.nextCID++
	s.mu.Unlock()

	dir := filepath.Join(s.settings.RuntimeDir, runtimeID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create sandbox dir: %w", err)
	}
	if err := syncDirectory(s.settings.RuntimeDir); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("sync runtime dir: %w", err)
	}

	sb := &sandbox{
		id:             runtimeID,
		conversationID: conversationID,
		dir:            dir,
		rootFSPath:     filepath.Join(dir, "rootfs.ext4"),
		apiSocketPath:  filepath.Join(dir, "firecracker.sock"),
		vsockPath:      filepath.Join(dir, "vsock.sock"),
		logPath:        filepath.Join(dir, "firecracker.log"),
		metricsPath:    filepath.Join(dir, "metrics.json"),
		guestCID:       guestCID,
		agentPort:      s.settings.AgentPort,
		createdAt:      time.Now().UTC(),
		state:          sandboxStateActive,
	}
	if s.settings.NetworkEnabled {
		network, err := s.networkForSandbox(sb)
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
		sb.bridgeName = s.settings.NetworkBridge
		sb.tapName = network.tapName
		sb.guestIP = network.guestIP
		sb.guestAddress = network.guestAddress
		sb.guestGateway = s.settings.NetworkGateway
		sb.guestMAC = network.guestMAC
	}
	if err := copyRootFS(s.settings.RootFSImagePath, sb.rootFSPath); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("prepare sandbox rootfs: %w", err)
	}

	if err := s.startFirecracker(ctx, sb); err != nil {
		s.stopSandboxProcess(sb)
		if sb.tapName != "" {
			_ = deleteLink(ctx, sb.tapName)
		}
		if !s.settings.KeepFailed {
			_ = os.RemoveAll(dir)
		}
		return nil, err
	}
	if err := s.persistSandboxManifest(sb); err != nil {
		_ = s.destroySandboxResources(sb)
		return nil, err
	}

	s.mu.Lock()
	s.sandboxes[runtimeID] = sb
	s.mu.Unlock()

	metadata, err := sb.metadata(nil)
	if err != nil {
		return nil, err
	}
	return &domain.SandboxHandle{Provider: providerName, RuntimeID: runtimeID, Metadata: metadata}, nil
}

func (s *Service) startFirecracker(ctx context.Context, sb *sandbox) error {
	_ = os.Remove(sb.apiSocketPath)
	_ = os.Remove(sb.vsockPath)
	logFile, err := os.OpenFile(sb.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return fmt.Errorf("open firecracker log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(s.settings.FirecrackerBin, "--api-sock", sb.apiSocketPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start firecracker: %w", err)
	}
	sb.cmd = cmd
	sb.done = make(chan error, 1)
	go func() {
		sb.done <- cmd.Wait()
		close(sb.done)
	}()

	if err := waitForSocket(ctx, sb.apiSocketPath, sb.done, 5*time.Second); err != nil {
		return err
	}

	client := firecrackerAPI{socketPath: sb.apiSocketPath}
	if s.settings.NetworkEnabled {
		if err := s.prepareHostNetwork(ctx, sb); err != nil {
			return err
		}
	}
	if err := client.put(ctx, "/machine-config", map[string]any{
		"vcpu_count":   s.settings.VCPUCount,
		"mem_size_mib": s.settings.MemSizeMIB,
		"smt":          false,
	}); err != nil {
		return err
	}
	bootSource := map[string]any{
		"kernel_image_path": s.settings.KernelImagePath,
		"boot_args":         s.settings.KernelBootArgs,
	}
	if strings.TrimSpace(s.settings.InitrdImagePath) != "" {
		bootSource["initrd_path"] = s.settings.InitrdImagePath
	}
	if err := client.put(ctx, "/boot-source", bootSource); err != nil {
		return err
	}
	if err := client.put(ctx, "/drives/rootfs", map[string]any{
		"drive_id":       "rootfs",
		"path_on_host":   sb.rootFSPath,
		"is_root_device": true,
		"is_read_only":   false,
	}); err != nil {
		return err
	}
	if err := client.put(ctx, "/vsock", map[string]any{
		"guest_cid": sb.guestCID,
		"uds_path":  sb.vsockPath,
	}); err != nil {
		return err
	}
	if s.settings.NetworkEnabled {
		if err := client.put(ctx, "/network-interfaces/net1", map[string]any{
			"iface_id":      "net1",
			"guest_mac":     sb.guestMAC,
			"host_dev_name": sb.tapName,
		}); err != nil {
			return err
		}
	}
	if err := client.put(ctx, "/actions", map[string]string{"action_type": "InstanceStart"}); err != nil {
		return err
	}
	if s.settings.BootTimeout <= 0 {
		return nil
	}
	if err := s.waitForAgent(ctx, sb); err != nil {
		return err
	}
	if s.settings.NetworkEnabled {
		if err := s.configureGuestNetwork(ctx, sb); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) waitForAgent(ctx context.Context, sb *sandbox) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, s.settings.BootTimeout)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := s.agentHealth(deadlineCtx, sb); err == nil {
			return nil
		}
		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf("sandbox agent did not become ready before timeout: %w", deadlineCtx.Err())
		case err := <-sb.done:
			return processExitError("firecracker exited before sandbox agent was ready", err)
		case <-ticker.C:
		}
	}
}

func (s *Service) agentHealth(ctx context.Context, sb *sandbox) error {
	client := newVsockHTTPClient(sb.vsockPath, sb.agentPort, 5*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://sandbox/health", nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("sandbox agent health status=%d", res.StatusCode)
	}
	return nil
}

var (
	errSandboxNotFound = errors.New("sandbox not found")
	errBadRequest      = errors.New("bad request")
)

type sandboxNetwork struct {
	tapName      string
	guestIP      string
	guestAddress string
	guestMAC     string
}

type agentNetworkRequest struct {
	Interface string   `json:"interface"`
	Address   string   `json:"address"`
	Gateway   string   `json:"gateway"`
	DNS       []string `json:"dns,omitempty"`
}

func (s *Service) networkForSandbox(sb *sandbox) (sandboxNetwork, error) {
	_, subnet, err := net.ParseCIDR(s.settings.NetworkSubnet)
	if err != nil {
		return sandboxNetwork{}, fmt.Errorf("parse firecracker network subnet: %w", err)
	}
	ones, bits := subnet.Mask.Size()
	base := subnet.IP.To4()
	if base == nil || bits != 32 || ones != 24 {
		return sandboxNetwork{}, fmt.Errorf("FIRECRACKER_NET_SUBNET must be an IPv4 /24 CIDR")
	}
	host := s.settings.GuestIPStart + int(sb.guestCID-3)
	if host > 254 {
		return sandboxNetwork{}, fmt.Errorf("firecracker guest IP pool exhausted")
	}
	guestIP := net.IPv4(base[0], base[1], base[2], byte(host)).String()
	return sandboxNetwork{
		tapName:      tapNameForRuntimeID(sb.id),
		guestIP:      guestIP,
		guestAddress: fmt.Sprintf("%s/%d", guestIP, ones),
		guestMAC:     fmt.Sprintf("02:fc:%02x:%02x:%02x:%02x", base[0], base[1], base[2], byte(host)),
	}, nil
}

func tapNameForRuntimeID(runtimeID string) string {
	clean := strings.NewReplacer("-", "", "_", "").Replace(runtimeID)
	clean = strings.TrimPrefix(clean, "fc")
	if len(clean) > 11 {
		clean = clean[:11]
	}
	if clean == "" {
		clean = "sandbox"
	}
	return "tap" + clean
}

func (s *Service) prepareHostNetwork(ctx context.Context, sb *sandbox) error {
	if err := runCommand(ctx, "ip", "link", "show", s.settings.NetworkBridge); err != nil {
		if err := runCommand(ctx, "ip", "link", "add", s.settings.NetworkBridge, "type", "bridge"); err != nil {
			return fmt.Errorf("create firecracker bridge %s: %w", s.settings.NetworkBridge, err)
		}
	}
	if err := runCommand(ctx, "ip", "addr", "replace", s.settings.NetworkCIDR, "dev", s.settings.NetworkBridge); err != nil {
		return fmt.Errorf("configure firecracker bridge address: %w", err)
	}
	if err := runCommand(ctx, "ip", "link", "set", s.settings.NetworkBridge, "up"); err != nil {
		return fmt.Errorf("bring up firecracker bridge: %w", err)
	}
	if err := runCommand(ctx, "sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return fmt.Errorf("enable ipv4 forwarding: %w", err)
	}
	if err := ensureIPTablesRule(ctx, "nat", []string{"POSTROUTING", "-s", s.settings.NetworkSubnet, "-j", "MASQUERADE"}); err != nil {
		return err
	}
	if err := ensureIPTablesRule(ctx, "filter", []string{"FORWARD", "-i", s.settings.NetworkBridge, "-j", "ACCEPT"}); err != nil {
		return err
	}
	if err := ensureIPTablesRule(ctx, "filter", []string{"FORWARD", "-o", s.settings.NetworkBridge, "-j", "ACCEPT"}); err != nil {
		return err
	}

	_ = deleteLink(ctx, sb.tapName)
	if err := runCommand(ctx, "ip", "tuntap", "add", "dev", sb.tapName, "mode", "tap"); err != nil {
		return fmt.Errorf("create tap %s: %w", sb.tapName, err)
	}
	if err := runCommand(ctx, "ip", "link", "set", sb.tapName, "master", s.settings.NetworkBridge); err != nil {
		_ = deleteLink(ctx, sb.tapName)
		return fmt.Errorf("attach tap %s to bridge %s: %w", sb.tapName, s.settings.NetworkBridge, err)
	}
	if err := runCommand(ctx, "ip", "link", "set", sb.tapName, "up"); err != nil {
		_ = deleteLink(ctx, sb.tapName)
		return fmt.Errorf("bring up tap %s: %w", sb.tapName, err)
	}
	return nil
}

func (s *Service) configureGuestNetwork(ctx context.Context, sb *sandbox) error {
	request := agentNetworkRequest{
		Interface: s.settings.NetworkIface,
		Address:   sb.guestAddress,
		Gateway:   sb.guestGateway,
		DNS:       append([]string(nil), s.settings.NetworkDNS...),
	}
	var response map[string]string
	if err := s.doAgentJSON(ctx, sb, http.MethodPost, "/network/configure", request, &response, 10*time.Second); err != nil {
		return fmt.Errorf("configure guest network: %w", err)
	}
	return nil
}

func (s *Service) doAgentJSON(ctx context.Context, sb *sandbox, method string, path string, input any, output any, timeout time.Duration) error {
	client := newVsockHTTPClient(sb.vsockPath, sb.agentPort, timeout)
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://sandbox"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= http.StatusBadRequest {
		message := readErrorMessage(res.Body)
		if message == "" {
			message = fmt.Sprintf("sandbox agent request failed: status=%d", res.StatusCode)
		}
		return errors.New(message)
	}
	if output != nil {
		if err := json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(output); err != nil {
			return err
		}
	}
	return nil
}

func ensureIPTablesRule(ctx context.Context, table string, rule []string) error {
	checkArgs := append([]string{"-t", table, "-C"}, rule...)
	if err := runCommand(ctx, "iptables", checkArgs...); err == nil {
		return nil
	}
	addArgs := append([]string{"-t", table, "-A"}, rule...)
	if err := runCommand(ctx, "iptables", addArgs...); err != nil {
		return fmt.Errorf("configure iptables %s rule %s: %w", table, strings.Join(rule, " "), err)
	}
	return nil
}

func deleteLink(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	err := runCommand(ctx, "ip", "link", "delete", name)
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "cannot find device") || strings.Contains(message, "does not exist") || strings.Contains(message, "not found") {
		return nil
	}
	return err
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), message)
	}
	return nil
}

func (s *Service) destroySandbox(runtimeID string) (*domain.SandboxHandle, error) {
	s.mu.Lock()
	sb := s.sandboxes[runtimeID]
	s.mu.Unlock()
	if sb == nil {
		return &domain.SandboxHandle{Provider: providerName, RuntimeID: runtimeID}, nil
	}
	sb.opMu.Lock()
	defer sb.opMu.Unlock()
	if err := s.destroySandboxResources(sb); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.sandboxes[runtimeID] == sb {
		delete(s.sandboxes, runtimeID)
	}
	s.mu.Unlock()
	metadata, err := sb.metadata(map[string]any{
		"destroyed_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, err
	}
	return &domain.SandboxHandle{Provider: providerName, RuntimeID: runtimeID, Metadata: metadata}, nil
}

func (s *Service) stopSandboxRuntime(_ context.Context, runtimeID string) (*domain.SandboxHandle, error) {
	s.mu.Lock()
	sb := s.sandboxes[runtimeID]
	s.mu.Unlock()
	if sb == nil {
		return nil, errSandboxNotFound
	}
	sb.opMu.Lock()
	defer sb.opMu.Unlock()
	if err := s.pauseSandboxResources(sb); err != nil {
		return nil, err
	}
	metadata, err := sb.metadata(nil)
	if err != nil {
		return nil, err
	}
	return &domain.SandboxHandle{Provider: providerName, RuntimeID: runtimeID, Metadata: metadata}, nil
}

func (s *Service) resumeSandboxRuntime(ctx context.Context, runtimeID string) (*domain.SandboxHandle, error) {
	s.mu.Lock()
	sb := s.sandboxes[runtimeID]
	s.mu.Unlock()
	if sb == nil {
		return nil, errSandboxNotFound
	}
	sb.opMu.Lock()
	defer sb.opMu.Unlock()
	if err := s.resumeSandboxResources(ctx, sb); err != nil {
		return nil, err
	}
	metadata, err := sb.metadata(nil)
	if err != nil {
		return nil, err
	}
	return &domain.SandboxHandle{Provider: providerName, RuntimeID: runtimeID, Metadata: metadata}, nil
}

func (s *Service) resumeSandboxResources(ctx context.Context, sb *sandbox) error {
	if sb == nil {
		return nil
	}
	if sb.state == sandboxStateActive && sandboxProcessRunning(sb) {
		return nil
	}
	if sb.state == sandboxStateActive {
		s.stopSandboxProcess(sb)
		now := time.Now().UTC()
		sb.state = sandboxStateStopped
		sb.stoppedAt = &now
	}
	if sb.tapName != "" {
		if err := deleteLink(ctx, sb.tapName); err != nil {
			return fmt.Errorf("delete stale sandbox network link: %w", err)
		}
	}
	if err := s.startFirecracker(ctx, sb); err != nil {
		s.stopSandboxProcess(sb)
		return err
	}
	sb.state = sandboxStateActive
	sb.stoppedAt = nil
	if err := s.persistSandboxManifest(sb); err != nil {
		_ = s.pauseSandboxResources(sb)
		return err
	}
	return nil
}

func (s *Service) pauseSandboxResources(sb *sandbox) error {
	if sb == nil {
		return nil
	}
	if sb.state == sandboxStateStopped {
		return nil
	}
	if !sandboxProcessRunning(sb) {
		if sb.tapName != "" {
			deleteCtx, cancelDelete := context.WithTimeout(context.Background(), 5*time.Second)
			err := deleteLink(deleteCtx, sb.tapName)
			cancelDelete()
			if err != nil {
				return fmt.Errorf("delete sandbox network link after process exit: %w", err)
			}
		}
		now := time.Now().UTC()
		sb.state = sandboxStateStopped
		sb.stoppedAt = &now
		return s.persistSandboxManifest(sb)
	}
	syncCtx, cancelSync := context.WithTimeout(context.Background(), 5*time.Second)
	var syncResult domain.SandboxCommandResult
	syncErr := s.doAgentJSON(syncCtx, sb, http.MethodPost, "/exec", domain.SandboxCommandRequest{Command: "sync", TimeoutSeconds: 5}, &syncResult, 5*time.Second)
	cancelSync()
	if syncErr != nil {
		return fmt.Errorf("sync sandbox %s before stop: %w", sb.id, syncErr)
	}
	if syncResult.ExitCode != 0 || syncResult.TimedOut {
		return fmt.Errorf("sync sandbox %s before stop failed: exit_code=%d timed_out=%t", sb.id, syncResult.ExitCode, syncResult.TimedOut)
	}
	s.stopSandboxProcess(sb)
	if sb.tapName != "" {
		deleteCtx, cancelDelete := context.WithTimeout(context.Background(), 5*time.Second)
		err := deleteLink(deleteCtx, sb.tapName)
		cancelDelete()
		if err != nil {
			return fmt.Errorf("delete sandbox network link: %w", err)
		}
	}
	now := time.Now().UTC()
	sb.state = sandboxStateStopped
	sb.stoppedAt = &now
	return s.persistSandboxManifest(sb)
}

func (s *Service) destroySandboxResources(sb *sandbox) error {
	if sb == nil {
		return nil
	}
	s.stopSandboxProcess(sb)
	if sb.tapName != "" {
		deleteCtx, cancelDelete := context.WithTimeout(context.Background(), 5*time.Second)
		err := deleteLink(deleteCtx, sb.tapName)
		cancelDelete()
		if err != nil {
			return fmt.Errorf("delete sandbox network link: %w", err)
		}
	}
	if err := os.RemoveAll(sb.dir); err != nil {
		return fmt.Errorf("remove sandbox directory %s: %w", sb.dir, err)
	}
	if err := syncDirectory(s.settings.RuntimeDir); err != nil {
		return fmt.Errorf("sync runtime dir after removing sandbox: %w", err)
	}
	return nil
}

func (s *Service) stopSandboxProcess(sb *sandbox) {
	if sb == nil || sb.cmd == nil || sb.cmd.Process == nil {
		return
	}
	defer func() {
		sb.cmd = nil
		sb.done = nil
	}()
	select {
	case <-sb.done:
		return
	default:
	}
	_ = sb.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-sb.done:
	case <-time.After(3 * time.Second):
		_ = sb.cmd.Process.Kill()
		<-sb.done
	}
}

func sandboxProcessRunning(sb *sandbox) bool {
	if sb == nil || sb.cmd == nil || sb.cmd.Process == nil || sb.done == nil {
		return false
	}
	select {
	case <-sb.done:
		sb.cmd = nil
		sb.done = nil
		return false
	default:
		return true
	}
}

func (s *Service) execCommand(ctx context.Context, runtimeID string, request domain.SandboxCommandRequest) (*domain.SandboxCommandResult, error) {
	if strings.TrimSpace(request.Command) == "" {
		return nil, fmt.Errorf("%w: command is required", errBadRequest)
	}
	s.mu.Lock()
	sb := s.sandboxes[runtimeID]
	s.mu.Unlock()
	if sb == nil {
		return nil, errSandboxNotFound
	}
	sb.opMu.Lock()
	defer sb.opMu.Unlock()
	if err := s.resumeSandboxResources(ctx, sb); err != nil {
		return nil, fmt.Errorf("resume sandbox before exec: %w", err)
	}

	timeout := 35 * time.Second
	if request.TimeoutSeconds > 0 {
		timeout = time.Duration(request.TimeoutSeconds+5) * time.Second
	}
	client := newVsockHTTPClient(sb.vsockPath, sb.agentPort, timeout)
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://sandbox/exec", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send sandbox agent exec request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= http.StatusBadRequest {
		message := readErrorMessage(res.Body)
		if message == "" {
			message = fmt.Sprintf("sandbox agent exec failed: status=%d", res.StatusCode)
		}
		return nil, fmt.Errorf("%s", message)
	}
	var result domain.SandboxCommandResult
	if err := json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode sandbox agent exec response: %w", err)
	}
	result.RuntimeID = runtimeID
	return &result, nil
}

func (sb *sandbox) metadata(extra map[string]any) (json.RawMessage, error) {
	metadata := map[string]any{
		"runtime_id":      sb.id,
		"conversation_id": sb.conversationID,
		"rootfs_path":     sb.rootFSPath,
		"api_socket":      sb.apiSocketPath,
		"vsock_socket":    sb.vsockPath,
		"log_path":        sb.logPath,
		"metrics_path":    sb.metricsPath,
		"guest_cid":       sb.guestCID,
		"agent_port":      sb.agentPort,
		"created_at":      sb.createdAt.Format(time.RFC3339Nano),
		"lifecycle_state": sb.state,
	}
	if sb.stoppedAt != nil {
		metadata["stopped_at"] = sb.stoppedAt.Format(time.RFC3339Nano)
	}
	if sb.tapName != "" {
		metadata["network"] = map[string]any{
			"tap_name":      sb.tapName,
			"guest_ip":      sb.guestIP,
			"guest_address": sb.guestAddress,
			"guest_gateway": sb.guestGateway,
			"guest_mac":     sb.guestMAC,
			"bridge":        sb.bridgeName,
		}
	}
	for key, value := range extra {
		metadata[key] = value
	}
	return json.Marshal(metadata)
}

func copyRootFS(srcPath string, dstPath string) error {
	srcPath = strings.TrimSpace(srcPath)
	dstPath = strings.TrimSpace(dstPath)
	if srcPath == "" {
		return errors.New("rootfs image path is required")
	}
	if dstPath == "" {
		return errors.New("sandbox rootfs path is required")
	}

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("stat source rootfs: %w", err)
	}
	if !srcInfo.Mode().IsRegular() {
		return errors.New("source rootfs is not a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o750); err != nil {
		return fmt.Errorf("create sandbox rootfs dir: %w", err)
	}

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source rootfs: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create sandbox rootfs: %w", err)
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		_ = os.Remove(dstPath)
		return fmt.Errorf("copy sandbox rootfs: %w", err)
	}
	if err := dstFile.Sync(); err != nil {
		_ = dstFile.Close()
		_ = os.Remove(dstPath)
		return fmt.Errorf("sync sandbox rootfs: %w", err)
	}
	if err := dstFile.Close(); err != nil {
		_ = os.Remove(dstPath)
		return fmt.Errorf("close sandbox rootfs: %w", err)
	}
	return nil
}

type firecrackerAPI struct {
	socketPath string
}

func (c firecrackerAPI) put(ctx context.Context, path string, input any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", c.socketPath)
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://firecracker"+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("firecracker api %s: %w", path, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return fmt.Errorf("firecracker api %s failed: status=%d body=%s", path, res.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func newVsockHTTPClient(vsockPath string, port uint32, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			var dialer net.Dialer
			conn, err := dialer.DialContext(ctx, "unix", vsockPath)
			if err != nil {
				return nil, err
			}
			if deadline, ok := ctx.Deadline(); ok {
				_ = conn.SetDeadline(deadline)
			} else {
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
			}
			if _, err := fmt.Fprintf(conn, "CONNECT %d\n", port); err != nil {
				_ = conn.Close()
				return nil, err
			}
			line, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("read firecracker vsock connect response: %w", err)
			}
			if !strings.HasPrefix(line, "OK ") {
				_ = conn.Close()
				return nil, fmt.Errorf("firecracker vsock connect failed: %s", strings.TrimSpace(line))
			}
			_ = conn.SetDeadline(time.Time{})
			return conn, nil
		},
		DisableKeepAlives: true,
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

func waitForSocket(ctx context.Context, path string, done <-chan error, timeout time.Duration) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf("wait for firecracker api socket: %w", deadlineCtx.Err())
		case err := <-done:
			return processExitError("firecracker exited before api socket was ready", err)
		case <-ticker.C:
		}
	}
}

func processExitError(message string, err error) error {
	if err == nil {
		return errors.New(message)
	}
	return fmt.Errorf("%s: %w", message, err)
}

func (s *Service) loadSandboxManifests() error {
	if err := os.MkdirAll(s.settings.RuntimeDir, 0o750); err != nil {
		return fmt.Errorf("create firecracker runtime dir: %w", err)
	}
	entries, err := os.ReadDir(s.settings.RuntimeDir)
	if err != nil {
		return fmt.Errorf("read firecracker runtime dir: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(s.settings.RuntimeDir, entry.Name())
		payload, readErr := os.ReadFile(filepath.Join(dir, sandboxManifestName))
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return fmt.Errorf("read sandbox manifest %s: %w", entry.Name(), readErr)
		}
		var manifest sandboxManifest
		if err := json.Unmarshal(payload, &manifest); err != nil {
			return fmt.Errorf("decode sandbox manifest %s: %w", entry.Name(), err)
		}
		if strings.TrimSpace(manifest.ID) == "" || manifest.ID != entry.Name() || manifest.GuestCID < 3 {
			return fmt.Errorf("sandbox manifest %s is invalid", entry.Name())
		}
		if _, err := os.Stat(filepath.Join(dir, "rootfs.ext4")); err != nil {
			return fmt.Errorf("sandbox %s rootfs is unavailable: %w", manifest.ID, err)
		}
		now := time.Now().UTC()
		sb := &sandbox{
			id:             manifest.ID,
			conversationID: manifest.ConversationID,
			dir:            dir,
			rootFSPath:     filepath.Join(dir, "rootfs.ext4"),
			apiSocketPath:  filepath.Join(dir, "firecracker.sock"),
			vsockPath:      filepath.Join(dir, "vsock.sock"),
			logPath:        filepath.Join(dir, "firecracker.log"),
			metricsPath:    filepath.Join(dir, "metrics.json"),
			guestCID:       manifest.GuestCID,
			agentPort:      s.settings.AgentPort,
			createdAt:      manifest.CreatedAt,
			state:          sandboxStateStopped,
			stoppedAt:      manifest.StoppedAt,
		}
		if sb.createdAt.IsZero() {
			sb.createdAt = now
		}
		if sb.stoppedAt == nil {
			sb.stoppedAt = &now
		}
		if s.settings.NetworkEnabled {
			network, networkErr := s.networkForSandbox(sb)
			if networkErr != nil {
				return networkErr
			}
			sb.bridgeName = s.settings.NetworkBridge
			sb.tapName = network.tapName
			sb.guestIP = network.guestIP
			sb.guestAddress = network.guestAddress
			sb.guestGateway = s.settings.NetworkGateway
			sb.guestMAC = network.guestMAC
		}
		s.sandboxes[sb.id] = sb
		if s.nextCID <= sb.guestCID {
			s.nextCID = sb.guestCID + 1
		}
		if err := s.persistSandboxManifest(sb); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) persistSandboxManifest(sb *sandbox) error {
	if sb == nil {
		return errors.New("persist sandbox manifest: sandbox is required")
	}
	payload, err := json.Marshal(sandboxManifest{
		ID:             sb.id,
		ConversationID: sb.conversationID,
		GuestCID:       sb.guestCID,
		CreatedAt:      sb.createdAt,
		State:          sb.state,
		StoppedAt:      sb.stoppedAt,
	})
	if err != nil {
		return fmt.Errorf("marshal sandbox manifest: %w", err)
	}
	temporary := filepath.Join(sb.dir, sandboxManifestName+".tmp")
	manifestFile, err := os.OpenFile(temporary, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return fmt.Errorf("write sandbox manifest: %w", err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := manifestFile.Write(payload); err != nil {
		_ = manifestFile.Close()
		return fmt.Errorf("write sandbox manifest: %w", err)
	}
	if err := manifestFile.Sync(); err != nil {
		_ = manifestFile.Close()
		return fmt.Errorf("sync sandbox manifest: %w", err)
	}
	if err := manifestFile.Close(); err != nil {
		return fmt.Errorf("close sandbox manifest: %w", err)
	}
	if err := os.Rename(temporary, filepath.Join(sb.dir, sandboxManifestName)); err != nil {
		return fmt.Errorf("replace sandbox manifest: %w", err)
	}
	removeTemporary = false
	if err := syncDirectory(sb.dir); err != nil {
		return fmt.Errorf("sync sandbox directory: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	return directory.Close()
}

func writeSandboxLifecycleError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, errSandboxNotFound) {
		status = http.StatusNotFound
	} else if errors.Is(err, errBadRequest) {
		status = http.StatusBadRequest
	}
	writeError(w, status, err.Error())
}

func decodeJSON(w http.ResponseWriter, r *http.Request, output any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func readErrorMessage(body io.Reader) string {
	var payload struct {
		Error string `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&payload) == nil {
		return strings.TrimSpace(payload.Error)
	}
	return ""
}
