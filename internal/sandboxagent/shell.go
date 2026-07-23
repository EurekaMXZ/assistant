package sandboxagent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

const maxShellSessions = 8

type ShellManager struct {
	settings Settings
	mu       sync.Mutex
	sessions map[string]*shellSession
}

type shellSession struct {
	id      string
	workdir string
	cmd     *exec.Cmd
	stdin   ioWriteCloser
	output  *shellOutputBuffer
	done    chan struct{}
	opMu    sync.Mutex
}

type ioWriteCloser interface {
	Write([]byte) (int, error)
	Close() error
}

type shellOutputBuffer struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	truncated bool
}

func NewShellManager(settings Settings) *ShellManager {
	return &ShellManager{settings: settings, sessions: make(map[string]*shellSession)}
}

func (m *ShellManager) RegisterHandlers(mux *http.ServeMux) {
	if m == nil || mux == nil {
		return
	}
	mux.HandleFunc("/shells", m.handleShells)
	mux.HandleFunc("/shells/", m.handleShell)
}

func (m *ShellManager) handleShells(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request domain.SandboxShellCreateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	session, err := m.Create(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (m *ShellManager) handleShell(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/shells/"), "/"), "/")
	if len(parts) == 1 && r.Method == http.MethodDelete {
		session, err := m.Destroy(r.Context(), parts[0])
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, session)
		return
	}
	if len(parts) == 2 && parts[1] == "connect" && r.Method == http.MethodPost {
		var request domain.SandboxShellCommandRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		request.SessionID = parts[0]
		result, err := m.Execute(r.Context(), request)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	writeError(w, http.StatusNotFound, "shell session not found")
}

func (m *ShellManager) Create(_ context.Context, request domain.SandboxShellCreateRequest) (*domain.SandboxShellSession, error) {
	if m == nil {
		return nil, errors.New("shell manager is not configured")
	}
	sessionID := strings.TrimSpace(request.SessionID)
	if !validShellSessionID(sessionID) {
		return nil, errors.New("session_id must contain 1-128 letters, numbers, hyphens, or underscores")
	}
	workdir, err := resolveWorkdir(m.settings.Workdir, request.WorkingDirectory)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.sessions[sessionID]; existing != nil {
		select {
		case <-existing.done:
			delete(m.sessions, sessionID)
		default:
			return shellSessionResult(existing, domain.SandboxShellStatusActive), nil
		}
	}
	if len(m.sessions) >= maxShellSessions {
		return nil, fmt.Errorf("shell session limit of %d reached", maxShellSessions)
	}

	outputLimit := m.settings.MaxOutputBytes
	if outputLimit <= 0 {
		outputLimit = defaultMaxOutputBytes
	}
	output := &shellOutputBuffer{limit: outputLimit + 512}
	cmd := exec.Command("/bin/bash", "--noprofile", "--norc")
	cmd.Dir = workdir
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open shell input: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start shell session: %w", err)
	}
	session := &shellSession{id: sessionID, workdir: workdir, cmd: cmd, stdin: stdin, output: output, done: make(chan struct{})}
	m.sessions[sessionID] = session
	go func() {
		_ = cmd.Wait()
		close(session.done)
		m.mu.Lock()
		if m.sessions[session.id] == session {
			delete(m.sessions, session.id)
		}
		m.mu.Unlock()
	}()
	return shellSessionResult(session, domain.SandboxShellStatusActive), nil
}

func (m *ShellManager) Execute(ctx context.Context, request domain.SandboxShellCommandRequest) (*domain.SandboxShellCommandResult, error) {
	session, err := m.session(request.SessionID)
	if err != nil {
		return nil, err
	}
	command := strings.TrimSpace(request.Command)
	if command == "" {
		return nil, errors.New("command is required")
	}
	timeout := 30 * time.Second
	if request.TimeoutSeconds > 0 {
		timeout = time.Duration(request.TimeoutSeconds) * time.Second
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	session.opMu.Lock()
	defer session.opMu.Unlock()
	select {
	case <-session.done:
		return nil, errors.New("shell session is closed")
	default:
	}
	token, err := randomShellToken()
	if err != nil {
		return nil, err
	}
	prefix := []byte("\x1eassistant-shell-" + token + ":")
	suffix := byte(0x1f)
	session.output.reset()
	payload := command + "\n" + "builtin printf '\\036assistant-shell-" + token + ":%s\\037' \"$?\"\n"
	if _, err := session.stdin.Write([]byte(payload)); err != nil {
		return nil, fmt.Errorf("send shell command: %w", err)
	}

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, truncated := session.output.snapshot()
		if markerStart := bytes.Index(data, prefix); markerStart >= 0 {
			statusStart := markerStart + len(prefix)
			if markerEnd := bytes.IndexByte(data[statusStart:], suffix); markerEnd >= 0 {
				markerEnd += statusStart
				exitCode, parseErr := strconv.Atoi(string(data[statusStart:markerEnd]))
				if parseErr != nil {
					return nil, fmt.Errorf("parse shell command exit code: %w", parseErr)
				}
				return &domain.SandboxShellCommandResult{SessionID: session.id, Output: string(data[:markerStart]), ExitCode: exitCode, Truncated: truncated}, nil
			}
		}
		select {
		case <-commandCtx.Done():
			data, truncated := session.output.snapshot()
			return &domain.SandboxShellCommandResult{SessionID: session.id, Output: string(data), ExitCode: -1, TimedOut: true, Truncated: truncated}, nil
		case <-session.done:
			data, _ := session.output.snapshot()
			return nil, fmt.Errorf("shell session closed before command completed: %s", strings.TrimSpace(string(data)))
		case <-ticker.C:
		}
	}
}

func (m *ShellManager) Destroy(ctx context.Context, sessionID string) (*domain.SandboxShellSession, error) {
	session, err := m.session(sessionID)
	if err != nil {
		return nil, err
	}
	session.opMu.Lock()
	defer session.opMu.Unlock()
	_ = session.stdin.Close()
	if session.cmd.Process != nil {
		_ = syscall.Kill(-session.cmd.Process.Pid, syscall.SIGTERM)
	}
	select {
	case <-session.done:
	case <-ctx.Done():
		if session.cmd.Process != nil {
			_ = syscall.Kill(-session.cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
		if session.cmd.Process != nil {
			_ = syscall.Kill(-session.cmd.Process.Pid, syscall.SIGKILL)
		}
		<-session.done
	}
	m.mu.Lock()
	if m.sessions[session.id] == session {
		delete(m.sessions, session.id)
	}
	m.mu.Unlock()
	return shellSessionResult(session, domain.SandboxShellStatusClosed), nil
}

func (m *ShellManager) session(sessionID string) (*shellSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if !validShellSessionID(sessionID) {
		return nil, errors.New("invalid shell session_id")
	}
	m.mu.Lock()
	session := m.sessions[sessionID]
	m.mu.Unlock()
	if session == nil {
		return nil, os.ErrNotExist
	}
	return session, nil
}

func shellSessionResult(session *shellSession, status string) *domain.SandboxShellSession {
	return &domain.SandboxShellSession{SessionID: session.id, Status: status, WorkingDirectory: session.workdir}
}

func validShellSessionID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func randomShellToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate shell command marker: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func (b *shellOutputBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(value)
	b.data = append(b.data, value...)
	if len(b.data) > b.limit {
		overflow := len(b.data) - b.limit
		copy(b.data, b.data[overflow:])
		b.data = b.data[:b.limit]
		b.truncated = true
	}
	return written, nil
}

func (b *shellOutputBuffer) reset() {
	b.mu.Lock()
	b.data = b.data[:0]
	b.truncated = false
	b.mu.Unlock()
}

func (b *shellOutputBuffer) snapshot() ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...), b.truncated
}
