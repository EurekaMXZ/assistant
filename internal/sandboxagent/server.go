package sandboxagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func NewHandler(settings Settings) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var request domain.SandboxCommandRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := Exec(r.Context(), settings, request)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("/network/configure", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var request NetworkConfigRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := ConfigureNetwork(r.Context(), request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

type NetworkConfigRequest struct {
	Interface string   `json:"interface"`
	Address   string   `json:"address"`
	Gateway   string   `json:"gateway"`
	DNS       []string `json:"dns,omitempty"`
}

func ConfigureNetwork(ctx context.Context, request NetworkConfigRequest) error {
	iface := strings.TrimSpace(request.Interface)
	if iface == "" {
		return errors.New("interface is required")
	}
	address := strings.TrimSpace(request.Address)
	if _, _, err := net.ParseCIDR(address); err != nil {
		return fmt.Errorf("address must be CIDR: %w", err)
	}
	gateway := strings.TrimSpace(request.Gateway)
	if net.ParseIP(gateway) == nil {
		return errors.New("gateway must be an IP address")
	}

	if err := runCommand(ctx, "ip", "link", "set", iface, "up"); err != nil {
		return err
	}
	if err := runCommand(ctx, "ip", "addr", "flush", "dev", iface); err != nil {
		return err
	}
	if err := runCommand(ctx, "ip", "addr", "add", address, "dev", iface); err != nil {
		return err
	}
	if err := runCommand(ctx, "ip", "route", "replace", "default", "via", gateway, "dev", iface); err != nil {
		return err
	}
	return configureDNS(ctx, iface, request.DNS)
}

func configureDNS(ctx context.Context, iface string, dns []string) error {
	servers := make([]string, 0, len(dns))
	for _, server := range dns {
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		if net.ParseIP(server) == nil {
			return fmt.Errorf("dns server %q must be an IP address", server)
		}
		servers = append(servers, server)
	}
	if len(servers) == 0 {
		return nil
	}

	args := append([]string{"dns", iface}, servers...)
	if err := runCommand(ctx, "resolvectl", args...); err == nil {
		_ = runCommand(ctx, "resolvectl", "default-route", iface, "true")
		return nil
	}

	var builder strings.Builder
	for _, server := range servers {
		builder.WriteString("nameserver ")
		builder.WriteString(server)
		builder.WriteByte('\n')
	}
	_ = os.Remove("/etc/resolv.conf")
	if err := os.WriteFile("/etc/resolv.conf", []byte(builder.String()), 0o644); err != nil {
		return fmt.Errorf("write /etc/resolv.conf: %w", err)
	}
	return nil
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

func Exec(ctx context.Context, settings Settings, request domain.SandboxCommandRequest) (*domain.SandboxCommandResult, error) {
	command := strings.TrimSpace(request.Command)
	if command == "" {
		return nil, errors.New("command is required")
	}
	workdir, err := resolveWorkdir(settings.Workdir, request.WorkingDirectory)
	if err != nil {
		return nil, err
	}

	timeout := 30 * time.Second
	if request.TimeoutSeconds > 0 {
		timeout = time.Duration(request.TimeoutSeconds) * time.Second
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	output := &limitedBuffer{limit: settings.MaxOutputBytes}
	cmd := exec.CommandContext(commandCtx, command, request.Args...)
	cmd.Dir = workdir
	// A shared writer gives both child descriptors one OS pipe, preserving write order.
	cmd.Stdout = output
	cmd.Stderr = output

	exitCode := 0
	runErr := cmd.Run()
	timedOut := commandCtx.Err() == context.DeadlineExceeded
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if timedOut {
			exitCode = -1
			if output.Len() == 0 {
				_, _ = output.Write([]byte(runErr.Error()))
			}
		} else {
			return nil, fmt.Errorf("exec command: %w", runErr)
		}
	}

	return &domain.SandboxCommandResult{
		Command:          command,
		Args:             append([]string(nil), request.Args...),
		WorkingDirectory: workdir,
		Output:           output.String(),
		ExitCode:         exitCode,
		TimedOut:         timedOut,
	}, nil
}

func resolveWorkdir(root string, requested string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = defaultWorkdir
	}
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve sandbox workdir: %w", err)
	}
	if err := os.MkdirAll(resolvedRoot, 0o755); err != nil {
		return "", fmt.Errorf("create sandbox workdir: %w", err)
	}

	target := resolvedRoot
	if strings.TrimSpace(requested) != "" {
		clean := filepath.Clean(requested)
		if filepath.IsAbs(clean) {
			target = clean
		} else {
			target = filepath.Join(resolvedRoot, clean)
		}
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("create command workdir: %w", err)
	}
	return target, nil
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.buffer.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	_, _ = b.buffer.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) Len() int {
	return b.buffer.Len()
}

func (b *limitedBuffer) String() string {
	value := b.buffer.String()
	if b.truncated {
		value += "\n[output truncated]\n"
	}
	return value
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
