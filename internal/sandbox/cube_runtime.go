package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/tool"
	process "github.com/TencentCloudAgentRuntime/ags-go-sdk/pb/process"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/pb/process/processconnect"
	cubesandbox "github.com/tencentcloud/CubeSandbox/sdk/go"
)

const (
	defaultCubeRequestTimeout = 30 * time.Second
	defaultCubePauseTimeout   = 30 * time.Second
	defaultCubeMaxOutputBytes = 1 << 20
	defaultCubeClusterID      = "default"
)

var (
	errCubeSandboxNotFound = errors.New("cube sandbox not found")
	errCubeSandboxConflict = errors.New("cube sandbox conflict")
)

var _ tool.SandboxManager = (*CubeRuntime)(nil)
var _ tool.SandboxShellManager = (*CubeRuntime)(nil)

type CubeRuntimeSettings struct {
	APIURL              string
	APIKey              string
	TemplateID          string
	ProxyNodeIP         string
	ProxyPortHTTP       int
	ProxyScheme         string
	SandboxDomain       string
	ClusterID           string
	RequestTimeout      time.Duration
	PauseTimeout        time.Duration
	MaxOutputBytes      int
	AllowInternetAccess bool
	AllowOut            []string
	DenyOut             []string
}

type CubeRuntime struct {
	settings CubeRuntimeSettings
	client   cubeRuntimeClient
}

type cubeCreateOptions struct {
	TemplateID          string
	ConversationID      string
	RequestKey          string
	AllowInternetAccess bool
	AllowOut            []string
	DenyOut             []string
}

type cubeSandbox struct {
	ID              string
	TemplateID      string
	ClientID        string
	EnvdVersion     string
	EnvdAccessToken string
	Domain          string
	SDK             *cubesandbox.Sandbox
}

type cubeCommandOptions struct {
	Args           []string
	Timeout        time.Duration
	Cwd            string
	MaxOutputBytes int
}

type cubeCommandResult struct {
	Output   string
	ExitCode int
	TimedOut bool
}

type cubeRuntimeClient interface {
	Create(context.Context, cubeCreateOptions) (*cubeSandbox, error)
	Connect(context.Context, string) (*cubeSandbox, error)
	Inspect(context.Context, string) (string, error)
	Pause(context.Context, string, time.Duration) error
	Kill(context.Context, string) error
	RunCommand(context.Context, *cubeSandbox, string, cubeCommandOptions) (*cubeCommandResult, error)
	CreateShell(context.Context, *cubeSandbox, domain.SandboxShellCreateRequest) (*domain.SandboxShellSession, error)
	ExecShell(context.Context, *cubeSandbox, domain.SandboxShellCommandRequest, int) (*domain.SandboxShellCommandResult, error)
	DestroyShell(context.Context, *cubeSandbox, string) (*domain.SandboxShellSession, error)
	WriteFile(context.Context, *cubeSandbox, string, io.Reader, int64) error
	ReadFile(context.Context, *cubeSandbox, string) (io.ReadCloser, int64, error)
}

type cubeSDKClient struct {
	sdk           *cubesandbox.Client
	apiURL        string
	apiKey        string
	control       *http.Client
	data          *http.Client
	proxyScheme   string
	sandboxDomain string
}

type cubeRuntimeMetadata struct {
	ClusterID   string `json:"cluster_id"`
	TemplateID  string `json:"template_id"`
	ClientID    string `json:"client_id,omitempty"`
	EnvdVersion string `json:"envd_version,omitempty"`
	Domain      string `json:"domain,omitempty"`
}

func NewCubeRuntime(settings CubeRuntimeSettings) (*CubeRuntime, error) {
	normalized, err := normalizeCubeRuntimeSettings(settings)
	if err != nil {
		return nil, err
	}
	client := newCubeSDKClient(normalized)
	return &CubeRuntime{settings: normalized, client: client}, nil
}

func newCubeRuntimeWithClient(settings CubeRuntimeSettings, client cubeRuntimeClient) (*CubeRuntime, error) {
	normalized, err := normalizeCubeRuntimeSettings(settings)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("cube sandbox client is required")
	}
	return &CubeRuntime{settings: normalized, client: client}, nil
}

func normalizeCubeRuntimeSettings(settings CubeRuntimeSettings) (CubeRuntimeSettings, error) {
	settings.APIURL = strings.TrimRight(strings.TrimSpace(settings.APIURL), "/")
	settings.APIKey = strings.TrimSpace(settings.APIKey)
	settings.TemplateID = strings.TrimSpace(settings.TemplateID)
	settings.ProxyNodeIP = strings.TrimSpace(settings.ProxyNodeIP)
	settings.ProxyScheme = strings.ToLower(strings.TrimSpace(settings.ProxyScheme))
	settings.SandboxDomain = strings.TrimSpace(settings.SandboxDomain)
	settings.ClusterID = strings.TrimSpace(settings.ClusterID)
	if settings.APIURL == "" {
		return settings, errors.New("cube sandbox api url is required")
	}
	parsedAPIURL, err := url.ParseRequestURI(settings.APIURL)
	if err != nil || (parsedAPIURL.Scheme != "http" && parsedAPIURL.Scheme != "https") || parsedAPIURL.Host == "" {
		return settings, fmt.Errorf("cube sandbox api url %q is invalid", settings.APIURL)
	}
	if settings.APIKey == "" {
		return settings, errors.New("cube sandbox api key is required")
	}
	if settings.TemplateID == "" {
		return settings, errors.New("cube sandbox template id is required")
	}
	if settings.ProxyPortHTTP <= 0 {
		settings.ProxyPortHTTP = 80
	}
	if settings.ProxyScheme == "" {
		if settings.ProxyPortHTTP == 443 {
			settings.ProxyScheme = "https"
		} else {
			settings.ProxyScheme = "http"
		}
	}
	if settings.ProxyScheme != "http" && settings.ProxyScheme != "https" {
		return settings, fmt.Errorf("cube sandbox proxy scheme must be http or https, got %q", settings.ProxyScheme)
	}
	if settings.SandboxDomain == "" {
		settings.SandboxDomain = "cube.app"
	}
	if settings.ClusterID == "" {
		settings.ClusterID = defaultCubeClusterID
	}
	if settings.RequestTimeout <= 0 {
		settings.RequestTimeout = defaultCubeRequestTimeout
	}
	if settings.PauseTimeout <= 0 {
		settings.PauseTimeout = defaultCubePauseTimeout
	}
	if settings.MaxOutputBytes <= 0 {
		settings.MaxOutputBytes = defaultCubeMaxOutputBytes
	}
	settings.AllowOut = normalizedNonEmptyStrings(settings.AllowOut)
	settings.DenyOut = normalizedNonEmptyStrings(settings.DenyOut)
	if settings.AllowInternetAccess && cubeAllowOutHasDomain(settings.AllowOut) && !containsString(settings.DenyOut, "0.0.0.0/0") {
		return settings, errors.New("cube sandbox domain allow list requires internet access to be disabled or SANDBOX_CUBE_DENY_OUT to contain 0.0.0.0/0")
	}
	return settings, nil
}

func newCubeSDKClient(settings CubeRuntimeSettings) *cubeSDKClient {
	sdk := cubesandbox.NewClient(cubesandbox.Config{
		APIURL:         settings.APIURL,
		APIKey:         settings.APIKey,
		TemplateID:     settings.TemplateID,
		ProxyNodeIP:    settings.ProxyNodeIP,
		ProxyPortHTTP:  settings.ProxyPortHTTP,
		ProxyScheme:    settings.ProxyScheme,
		SandboxDomain:  settings.SandboxDomain,
		RequestTimeout: settings.RequestTimeout,
	})
	dataTransport := http.DefaultTransport.(*http.Transport).Clone()
	dataTransport.Proxy = nil
	dataTransport.DialContext = (&net.Dialer{Timeout: settings.RequestTimeout, KeepAlive: 30 * time.Second}).DialContext
	if settings.ProxyNodeIP != "" {
		target := net.JoinHostPort(settings.ProxyNodeIP, strconv.Itoa(settings.ProxyPortHTTP))
		dialer := &net.Dialer{Timeout: settings.RequestTimeout, KeepAlive: 30 * time.Second}
		dataTransport.DialContext = func(ctx context.Context, network string, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, target)
		}
	}
	return &cubeSDKClient{
		sdk:           sdk,
		apiURL:        settings.APIURL,
		apiKey:        settings.APIKey,
		control:       &http.Client{Timeout: settings.RequestTimeout},
		data:          &http.Client{Transport: dataTransport},
		proxyScheme:   settings.ProxyScheme,
		sandboxDomain: settings.SandboxDomain,
	}
}

func (r *CubeRuntime) CreateSandbox(ctx context.Context, conversationID string, requestKey string) (*domain.SandboxHandle, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("cube sandbox runtime is not configured")
	}
	sandbox, err := r.client.Create(ctx, cubeCreateOptions{
		TemplateID:          r.settings.TemplateID,
		ConversationID:      strings.TrimSpace(conversationID),
		RequestKey:          strings.TrimSpace(requestKey),
		AllowInternetAccess: r.settings.AllowInternetAccess,
		AllowOut:            append([]string(nil), r.settings.AllowOut...),
		DenyOut:             append([]string(nil), r.settings.DenyOut...),
	})
	if err != nil {
		return nil, fmt.Errorf("create cube sandbox: %w", err)
	}
	return r.handleForSandbox(sandbox)
}

func (r *CubeRuntime) StopSandbox(ctx context.Context, handle domain.SandboxHandle, _ string) (*domain.SandboxHandle, error) {
	if err := r.validateHandle(handle); err != nil {
		return nil, err
	}
	if err := r.client.Pause(ctx, handle.RuntimeID, r.settings.PauseTimeout); err != nil {
		return nil, fmt.Errorf("pause cube sandbox %q: %w", handle.RuntimeID, err)
	}
	return cubeHandleWithMetadata(handle.RuntimeID, handle.Metadata), nil
}

func (r *CubeRuntime) ResumeSandbox(ctx context.Context, handle domain.SandboxHandle, _ string) (*domain.SandboxHandle, error) {
	if err := r.validateHandle(handle); err != nil {
		return nil, err
	}
	sandbox, err := r.client.Connect(ctx, handle.RuntimeID)
	if err != nil {
		return nil, fmt.Errorf("resume cube sandbox %q: %w", handle.RuntimeID, err)
	}
	return r.handleForSandbox(sandbox)
}

func (r *CubeRuntime) DestroySandbox(ctx context.Context, handle domain.SandboxHandle, _ string) (*domain.SandboxHandle, error) {
	if err := r.validateHandle(handle); err != nil {
		return nil, err
	}

	killErr := r.client.Kill(ctx, handle.RuntimeID)
	if killErr == nil || errors.Is(killErr, errCubeSandboxNotFound) {
		return cubeHandleWithMetadata(handle.RuntimeID, handle.Metadata), nil
	}

	state, err := r.client.Inspect(ctx, handle.RuntimeID)
	if errors.Is(err, errCubeSandboxNotFound) {
		return cubeHandleWithMetadata(handle.RuntimeID, handle.Metadata), nil
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("destroy cube sandbox %q: %w", handle.RuntimeID, killErr), fmt.Errorf("inspect cube sandbox after destroy failure: %w", err))
	}
	if state == "paused" || state == "pausing" {
		if _, err := r.client.Connect(ctx, handle.RuntimeID); errors.Is(err, errCubeSandboxNotFound) {
			return cubeHandleWithMetadata(handle.RuntimeID, handle.Metadata), nil
		} else if err != nil {
			return nil, errors.Join(fmt.Errorf("destroy paused cube sandbox %q: %w", handle.RuntimeID, killErr), fmt.Errorf("resume before destroy: %w", err))
		}
	} else {
		return nil, fmt.Errorf("destroy cube sandbox %q: %w", handle.RuntimeID, killErr)
	}
	if err := r.client.Kill(ctx, handle.RuntimeID); errors.Is(err, errCubeSandboxNotFound) {
		return cubeHandleWithMetadata(handle.RuntimeID, handle.Metadata), nil
	} else if err != nil {
		return nil, fmt.Errorf("destroy cube sandbox %q: %w", handle.RuntimeID, err)
	}
	return cubeHandleWithMetadata(handle.RuntimeID, handle.Metadata), nil
}

func (r *CubeRuntime) ExecSandboxCommand(ctx context.Context, handle domain.SandboxHandle, request domain.SandboxCommandRequest, _ string) (*domain.SandboxCommandResult, error) {
	if err := r.validateHandle(handle); err != nil {
		return nil, err
	}
	command := strings.TrimSpace(request.Command)
	if command == "" {
		return nil, errors.New("cube sandbox command is required")
	}
	timeout := time.Duration(request.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	sandbox, err := r.client.Connect(ctx, handle.RuntimeID)
	if err != nil {
		return nil, fmt.Errorf("connect cube sandbox %q for command: %w", handle.RuntimeID, err)
	}
	workingDirectory := cubeWorkingDirectory(request.WorkingDirectory)
	runtimeCommand, runtimeArgs := cubeCommandProcess(command, request.Args, workingDirectory)
	result, err := r.client.RunCommand(ctx, sandbox, runtimeCommand, cubeCommandOptions{
		Args:           runtimeArgs,
		Timeout:        timeout,
		Cwd:            "/",
		MaxOutputBytes: r.settings.MaxOutputBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("execute command in cube sandbox %q: %w", handle.RuntimeID, err)
	}
	if result == nil {
		return nil, fmt.Errorf("cube sandbox %q returned an empty command result", handle.RuntimeID)
	}

	return &domain.SandboxCommandResult{
		RuntimeID:        handle.RuntimeID,
		Command:          command,
		Args:             append([]string(nil), request.Args...),
		WorkingDirectory: workingDirectory,
		Output:           result.Output,
		ExitCode:         result.ExitCode,
		TimedOut:         result.TimedOut,
	}, nil
}

func (r *CubeRuntime) CreateSandboxShell(ctx context.Context, handle domain.SandboxHandle, request domain.SandboxShellCreateRequest, _ string) (*domain.SandboxShellSession, error) {
	if err := r.validateHandle(handle); err != nil {
		return nil, err
	}
	sandbox, err := r.client.Connect(ctx, handle.RuntimeID)
	if err != nil {
		return nil, fmt.Errorf("connect cube sandbox %q for shell creation: %w", handle.RuntimeID, err)
	}
	session, err := r.client.CreateShell(ctx, sandbox, request)
	if err != nil {
		return nil, err
	}
	session.RuntimeID = handle.RuntimeID
	return session, nil
}

func (r *CubeRuntime) ExecSandboxShell(ctx context.Context, handle domain.SandboxHandle, request domain.SandboxShellCommandRequest, _ string) (*domain.SandboxShellCommandResult, error) {
	if err := r.validateHandle(handle); err != nil {
		return nil, err
	}
	if request.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(request.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	sandbox, err := r.client.Connect(ctx, handle.RuntimeID)
	if err != nil {
		return nil, fmt.Errorf("connect cube sandbox %q for shell command: %w", handle.RuntimeID, err)
	}
	result, err := r.client.ExecShell(ctx, sandbox, request, r.settings.MaxOutputBytes)
	if err != nil {
		return nil, err
	}
	result.RuntimeID = handle.RuntimeID
	return result, nil
}

func (r *CubeRuntime) DestroySandboxShell(ctx context.Context, handle domain.SandboxHandle, sessionID string, _ string) (*domain.SandboxShellSession, error) {
	if err := r.validateHandle(handle); err != nil {
		return nil, err
	}
	sandbox, err := r.client.Connect(ctx, handle.RuntimeID)
	if err != nil {
		return nil, fmt.Errorf("connect cube sandbox %q for shell destruction: %w", handle.RuntimeID, err)
	}
	session, err := r.client.DestroyShell(ctx, sandbox, sessionID)
	if err != nil {
		return nil, err
	}
	session.RuntimeID = handle.RuntimeID
	return session, nil
}

func (r *CubeRuntime) WriteSandboxFile(ctx context.Context, handle domain.SandboxHandle, path string, reader io.Reader, size int64, _ string) error {
	if err := r.validateHandle(handle); err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("cube sandbox file path is required")
	}
	if reader == nil {
		return errors.New("cube sandbox file reader is required")
	}
	if size < 0 || size > domain.SandboxFileMaxBytes {
		return fmt.Errorf("cube sandbox file exceeds %d bytes", domain.SandboxFileMaxBytes)
	}
	sandbox, err := r.client.Connect(ctx, handle.RuntimeID)
	if err != nil {
		return fmt.Errorf("connect cube sandbox %q for file write: %w", handle.RuntimeID, err)
	}
	if err := r.client.WriteFile(ctx, sandbox, path, io.LimitReader(reader, size), size); err != nil {
		return fmt.Errorf("write cube sandbox file %q: %w", path, err)
	}
	return nil
}

func (r *CubeRuntime) ReadSandboxFile(ctx context.Context, handle domain.SandboxHandle, path string) (io.ReadCloser, int64, error) {
	if err := r.validateHandle(handle); err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(path) == "" {
		return nil, 0, errors.New("cube sandbox file path is required")
	}
	sandbox, err := r.client.Connect(ctx, handle.RuntimeID)
	if err != nil {
		return nil, 0, fmt.Errorf("connect cube sandbox %q for file read: %w", handle.RuntimeID, err)
	}
	reader, size, err := r.client.ReadFile(ctx, sandbox, path)
	if err != nil {
		return nil, 0, fmt.Errorf("read cube sandbox file %q: %w", path, err)
	}
	if size < 0 || size > domain.SandboxFileMaxBytes {
		reader.Close()
		return nil, 0, fmt.Errorf("cube sandbox file exceeds %d bytes", domain.SandboxFileMaxBytes)
	}
	return reader, size, nil
}

func (r *CubeRuntime) validateHandle(handle domain.SandboxHandle) error {
	if r == nil || r.client == nil {
		return errors.New("cube sandbox runtime is not configured")
	}
	if normalizeProvider(handle.Provider) != ProviderCubeSandbox {
		return fmt.Errorf("cube sandbox runtime cannot handle provider %q", handle.Provider)
	}
	if strings.TrimSpace(handle.RuntimeID) == "" {
		return errors.New("cube sandbox runtime id is required")
	}
	return nil
}

func (r *CubeRuntime) handleForSandbox(sandbox *cubeSandbox) (*domain.SandboxHandle, error) {
	if sandbox == nil || strings.TrimSpace(sandbox.ID) == "" {
		return nil, errors.New("cube sandbox returned an empty sandbox id")
	}
	metadata, err := json.Marshal(cubeRuntimeMetadata{
		ClusterID:   r.settings.ClusterID,
		TemplateID:  sandbox.TemplateID,
		ClientID:    sandbox.ClientID,
		EnvdVersion: sandbox.EnvdVersion,
		Domain:      sandbox.Domain,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal cube sandbox metadata: %w", err)
	}
	return cubeHandleWithMetadata(sandbox.ID, metadata), nil
}

func cubeHandleWithMetadata(runtimeID string, metadata json.RawMessage) *domain.SandboxHandle {
	return &domain.SandboxHandle{
		Provider:  ProviderCubeSandbox,
		RuntimeID: strings.TrimSpace(runtimeID),
		Metadata:  append(json.RawMessage(nil), metadata...),
	}
}

func cubeWorkingDirectory(requested string) string {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "/workspace"
	}
	if path.IsAbs(requested) {
		return path.Clean(requested)
	}
	return path.Join("/workspace", requested)
}

func cubeCommandProcess(command string, args []string, workingDirectory string) (string, []string) {
	const script = `workdir=$1
shift
mkdir -p -- "$workdir" && cd -- "$workdir" && exec "$@"`
	runtimeArgs := []string{"-c", script, "assistant-sandbox", workingDirectory, command}
	runtimeArgs = append(runtimeArgs, args...)
	return "/bin/sh", runtimeArgs
}

func normalizedNonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func cubeAllowOutHasDomain(values []string) bool {
	for _, value := range values {
		if net.ParseIP(value) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(value); err == nil {
			continue
		}
		return true
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (c *cubeSDKClient) Create(ctx context.Context, opts cubeCreateOptions) (*cubeSandbox, error) {
	allowInternet := opts.AllowInternetAccess
	sandbox, err := c.sdk.Create(ctx, cubesandbox.CreateOptions{
		TemplateID: opts.TemplateID,
		Timeout:    cubesandbox.DurationPtr(cubesandbox.NeverTimeout),
		Metadata: map[string]string{
			"assistant.provider":        ProviderCubeSandbox,
			"assistant.conversation_id": opts.ConversationID,
			"assistant.request_key":     opts.RequestKey,
		},
		AllowInternetAccess: &allowInternet,
		Network: cubesandbox.NetworkOptions{
			AllowOut: append([]string(nil), opts.AllowOut...),
			DenyOut:  append([]string(nil), opts.DenyOut...),
		},
		Extra: map[string]any{"allow_internet_access": allowInternet},
	})
	if err != nil {
		return nil, normalizeCubeSDKError(err)
	}
	return cubeSandboxFromSDK(sandbox), nil
}

func (c *cubeSDKClient) Connect(ctx context.Context, sandboxID string) (*cubeSandbox, error) {
	sandbox, err := c.sdk.Connect(ctx, strings.TrimSpace(sandboxID))
	if err != nil {
		return nil, normalizeCubeSDKError(err)
	}
	return cubeSandboxFromSDK(sandbox), nil
}

func (c *cubeSDKClient) Inspect(ctx context.Context, sandboxID string) (string, error) {
	var response struct {
		State string `json:"state"`
	}
	if err := c.doControl(ctx, http.MethodGet, "/sandboxes/"+url.PathEscape(strings.TrimSpace(sandboxID)), nil, &response); err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(response.State)), nil
}

func (c *cubeSDKClient) Pause(ctx context.Context, sandboxID string, waitTimeout time.Duration) error {
	sandboxID = strings.TrimSpace(sandboxID)
	err := c.doControl(ctx, http.MethodPost, "/sandboxes/"+url.PathEscape(sandboxID)+"/pause", nil, nil)
	if errors.Is(err, errCubeSandboxConflict) {
		state, inspectErr := c.Inspect(ctx, sandboxID)
		if inspectErr == nil && state == "paused" {
			return nil
		}
	}
	if err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := c.Inspect(waitCtx, sandboxID)
		if err != nil {
			return err
		}
		if state == "paused" {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("cube sandbox %q did not reach paused state: %w", sandboxID, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func (c *cubeSDKClient) Kill(ctx context.Context, sandboxID string) error {
	return c.doControl(ctx, http.MethodDelete, "/sandboxes/"+url.PathEscape(strings.TrimSpace(sandboxID)), nil, nil)
}

func (c *cubeSDKClient) RunCommand(ctx context.Context, sandbox *cubeSandbox, command string, opts cubeCommandOptions) (*cubeCommandResult, error) {
	if sandbox == nil || strings.TrimSpace(sandbox.ID) == "" {
		return nil, errors.New("cube sandbox command session is not connected")
	}
	if c == nil || c.data == nil {
		return nil, errors.New("cube sandbox data client is not configured")
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	domain := sandbox.Domain
	if domain == "" {
		domain = c.sandboxDomain
	}
	host := "49983-" + sandbox.ID + "." + domain
	processClient := processconnect.NewProcessClient(c.data, c.proxyScheme+"://"+host, connect.WithProtoJSON())
	workingDirectory := opts.Cwd
	req := connect.NewRequest(&process.StartRequest{Process: &process.ProcessConfig{
		Cmd:  command,
		Args: append([]string(nil), opts.Args...),
		Envs: map[string]string{},
		Cwd:  &workingDirectory,
	}})
	req.Header().Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("root:")))
	if sandbox.EnvdAccessToken != "" {
		req.Header().Set("X-Access-Token", sandbox.EnvdAccessToken)
	}
	stream, err := processClient.Start(ctx, req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &cubeCommandResult{Output: "command timed out", ExitCode: -1, TimedOut: true}, nil
		}
		return nil, fmt.Errorf("start cube sandbox command: %w", err)
	}
	defer stream.Close()

	output := &cubeOutputBuffer{limit: opts.MaxOutputBytes}
	result := &cubeCommandResult{}
	sawEnd := false
	for stream.Receive() {
		event := stream.Msg().GetEvent()
		if event == nil {
			continue
		}
		if data := event.GetData(); data != nil {
			for _, chunk := range [][]byte{data.GetStdout(), data.GetStderr(), data.GetPty()} {
				_, _ = output.Write(chunk)
			}
		}
		if end := event.GetEnd(); end != nil {
			exitCode, err := cubeCommandExitCode(end)
			if err != nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return &cubeCommandResult{Output: "command timed out", ExitCode: -1, TimedOut: true}, nil
				}
				return nil, fmt.Errorf("cube sandbox command failed: %w", err)
			}
			result.ExitCode = exitCode
			sawEnd = true
		}
	}
	if err := stream.Err(); err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &cubeCommandResult{Output: "command timed out", ExitCode: -1, TimedOut: true}, nil
	} else if err != nil {
		return nil, fmt.Errorf("receive cube sandbox command stream: %w", err)
	}
	if !sawEnd {
		return nil, errors.New("cube sandbox command stream ended without an end event")
	}
	result.Output = output.String()
	return result, nil
}

func (c *cubeSDKClient) CreateShell(ctx context.Context, sandbox *cubeSandbox, request domain.SandboxShellCreateRequest) (*domain.SandboxShellSession, error) {
	processClient, err := c.shellProcessClient(sandbox)
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(request.SessionID)
	if !validCubeShellSessionID(sessionID) {
		return nil, errors.New("invalid cube shell session id")
	}
	tag := cubeShellTag(sessionID)
	listRequest := connect.NewRequest(&process.ListRequest{})
	authorizeCubeProcessRequest(listRequest.Header(), sandbox)
	listed, err := processClient.List(ctx, listRequest)
	if err != nil {
		return nil, fmt.Errorf("list cube shell sessions: %w", err)
	}
	for _, running := range listed.Msg.GetProcesses() {
		if running.GetTag() == tag {
			return &domain.SandboxShellSession{SessionID: sessionID, Status: domain.SandboxShellStatusActive, WorkingDirectory: shellWorkingDirectory(request.WorkingDirectory)}, nil
		}
	}

	workdir := shellWorkingDirectory(request.WorkingDirectory)
	startRequest := connect.NewRequest(&process.StartRequest{
		Process: &process.ProcessConfig{Cmd: "/bin/bash", Args: []string{"--noprofile", "--norc"}, Envs: map[string]string{}, Cwd: &workdir},
		Tag:     &tag,
	})
	authorizeCubeProcessRequest(startRequest.Header(), sandbox)
	stream, err := processClient.Start(ctx, startRequest)
	if err != nil {
		return nil, fmt.Errorf("start cube shell session: %w", err)
	}
	defer stream.Close()
	for stream.Receive() {
		event := stream.Msg().GetEvent()
		if event == nil {
			continue
		}
		if started := event.GetStart(); started != nil && started.GetPid() != 0 {
			return &domain.SandboxShellSession{SessionID: sessionID, Status: domain.SandboxShellStatusActive, WorkingDirectory: workdir}, nil
		}
		if ended := event.GetEnd(); ended != nil {
			return nil, fmt.Errorf("cube shell session exited during startup: %s", cubeProcessEndMessage(ended))
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("receive cube shell startup: %w", err)
	}
	return nil, errors.New("cube shell startup ended without a process id")
}

func (c *cubeSDKClient) ExecShell(ctx context.Context, sandbox *cubeSandbox, request domain.SandboxShellCommandRequest, maxOutputBytes int) (*domain.SandboxShellCommandResult, error) {
	processClient, err := c.shellProcessClient(sandbox)
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(request.SessionID)
	if !validCubeShellSessionID(sessionID) {
		return nil, errors.New("invalid cube shell session id")
	}
	selector := &process.ProcessSelector{Selector: &process.ProcessSelector_Tag{Tag: cubeShellTag(sessionID)}}
	connectRequest := connect.NewRequest(&process.ConnectRequest{Process: selector})
	authorizeCubeProcessRequest(connectRequest.Header(), sandbox)
	stream, err := processClient.Connect(ctx, connectRequest)
	if err != nil {
		return nil, fmt.Errorf("connect cube shell session: %w", err)
	}
	defer stream.Close()

	token, err := cubeShellToken()
	if err != nil {
		return nil, err
	}
	startMarker := []byte("\x1eassistant-shell-" + token + "-start\x1f")
	if err := sendCubeShellInput(ctx, processClient, sandbox, selector, "builtin printf '\\036assistant-shell-"+token+"-start\\037'\n"); err != nil {
		return nil, err
	}
	handshake := &cubeShellOutputBuffer{limit: 4096}
	started := false
	for !started && stream.Receive() {
		event := stream.Msg().GetEvent()
		if event == nil {
			continue
		}
		if data := event.GetData(); data != nil {
			for _, chunk := range [][]byte{data.GetStdout(), data.GetStderr(), data.GetPty()} {
				handshake.Write(chunk)
			}
			started = bytes.Contains(handshake.data, startMarker)
		}
		if end := event.GetEnd(); end != nil {
			return nil, fmt.Errorf("cube shell session is closed: %s", cubeProcessEndMessage(end))
		}
	}
	if !started {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &domain.SandboxShellCommandResult{SessionID: sessionID, ExitCode: -1, TimedOut: true}, nil
		}
		if err := stream.Err(); err != nil {
			return nil, fmt.Errorf("connect cube shell input stream: %w", err)
		}
		return nil, errors.New("cube shell session ended before synchronization")
	}

	endPrefix := []byte("\x1eassistant-shell-" + token + ":")
	payload := strings.TrimSpace(request.Command) + "\n" + "builtin printf '\\036assistant-shell-" + token + ":%s\\037' \"$?\"\n"
	if err := sendCubeShellInput(ctx, processClient, sandbox, selector, payload); err != nil {
		return nil, err
	}
	if maxOutputBytes <= 0 {
		maxOutputBytes = defaultCubeMaxOutputBytes
	}
	output := &cubeShellOutputBuffer{limit: maxOutputBytes + 512}
	for stream.Receive() {
		event := stream.Msg().GetEvent()
		if event == nil {
			continue
		}
		if data := event.GetData(); data != nil {
			for _, chunk := range [][]byte{data.GetStdout(), data.GetStderr(), data.GetPty()} {
				output.Write(chunk)
			}
			if markerStart := bytes.Index(output.data, endPrefix); markerStart >= 0 {
				statusStart := markerStart + len(endPrefix)
				if markerEnd := bytes.IndexByte(output.data[statusStart:], 0x1f); markerEnd >= 0 {
					markerEnd += statusStart
					exitCode, parseErr := strconv.Atoi(string(output.data[statusStart:markerEnd]))
					if parseErr != nil {
						return nil, fmt.Errorf("parse cube shell exit code: %w", parseErr)
					}
					value := strings.ToValidUTF8(string(output.data[:markerStart]), "\ufffd")
					if len(value) > maxOutputBytes {
						value = truncateValidUTF8(value, maxOutputBytes)
						output.truncated = true
					}
					return &domain.SandboxShellCommandResult{SessionID: sessionID, Output: value, ExitCode: exitCode, Truncated: output.truncated}, nil
				}
			}
		}
		if end := event.GetEnd(); end != nil {
			return nil, fmt.Errorf("cube shell session closed before command completed: %s", cubeProcessEndMessage(end))
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &domain.SandboxShellCommandResult{SessionID: sessionID, Output: strings.ToValidUTF8(string(output.data), "\ufffd"), ExitCode: -1, TimedOut: true, Truncated: output.truncated}, nil
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("receive cube shell output: %w", err)
	}
	return nil, errors.New("cube shell output ended before command completion")
}

func (c *cubeSDKClient) DestroyShell(ctx context.Context, sandbox *cubeSandbox, sessionID string) (*domain.SandboxShellSession, error) {
	processClient, err := c.shellProcessClient(sandbox)
	if err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if !validCubeShellSessionID(sessionID) {
		return nil, errors.New("invalid cube shell session id")
	}
	request := connect.NewRequest(&process.SendSignalRequest{
		Process: &process.ProcessSelector{Selector: &process.ProcessSelector_Tag{Tag: cubeShellTag(sessionID)}},
		Signal:  process.Signal_SIGNAL_SIGTERM,
	})
	authorizeCubeProcessRequest(request.Header(), sandbox)
	if _, err := processClient.SendSignal(ctx, request); err != nil {
		return nil, fmt.Errorf("destroy cube shell session: %w", err)
	}
	return &domain.SandboxShellSession{SessionID: sessionID, Status: domain.SandboxShellStatusClosed}, nil
}

func (c *cubeSDKClient) shellProcessClient(sandbox *cubeSandbox) (processconnect.ProcessClient, error) {
	if sandbox == nil || strings.TrimSpace(sandbox.ID) == "" {
		return nil, errors.New("cube shell session is not connected")
	}
	if c == nil || c.data == nil {
		return nil, errors.New("cube sandbox data client is not configured")
	}
	domainName := sandbox.Domain
	if domainName == "" {
		domainName = c.sandboxDomain
	}
	host := "49983-" + sandbox.ID + "." + domainName
	return processconnect.NewProcessClient(c.data, c.proxyScheme+"://"+host, connect.WithProtoJSON()), nil
}

func sendCubeShellInput(ctx context.Context, client processconnect.ProcessClient, sandbox *cubeSandbox, selector *process.ProcessSelector, value string) error {
	request := connect.NewRequest(&process.SendInputRequest{
		Process: selector,
		Input:   &process.ProcessInput{Input: &process.ProcessInput_Stdin{Stdin: []byte(value)}},
	})
	authorizeCubeProcessRequest(request.Header(), sandbox)
	if _, err := client.SendInput(ctx, request); err != nil {
		return fmt.Errorf("send cube shell input: %w", err)
	}
	return nil
}

func authorizeCubeProcessRequest(header http.Header, sandbox *cubeSandbox) {
	header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("root:")))
	if sandbox != nil && sandbox.EnvdAccessToken != "" {
		header.Set("X-Access-Token", sandbox.EnvdAccessToken)
	}
}

func shellWorkingDirectory(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/workspace"
	}
	return value
}

func cubeShellTag(sessionID string) string {
	return "assistant-shell-" + sessionID
}

func validCubeShellSessionID(value string) bool {
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

func cubeShellToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate cube shell marker: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func cubeProcessEndMessage(end *process.ProcessEvent_EndEvent) string {
	if end == nil {
		return "process ended"
	}
	for _, value := range []string{end.GetError(), end.GetStatus()} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	if end.GetExited() {
		return fmt.Sprintf("exit code %d", end.GetExitCode())
	}
	return "process ended"
}

type cubeShellOutputBuffer struct {
	data      []byte
	limit     int
	truncated bool
}

func (b *cubeShellOutputBuffer) Write(value []byte) {
	b.data = append(b.data, value...)
	if len(b.data) > b.limit {
		overflow := len(b.data) - b.limit
		copy(b.data, b.data[overflow:])
		b.data = b.data[:b.limit]
		b.truncated = true
	}
}

func cubeCommandExitCode(end *process.ProcessEvent_EndEvent) (int, error) {
	if end == nil {
		return 0, errors.New("process ended without an end event")
	}
	if end.GetExited() {
		return int(end.GetExitCode()), nil
	}
	for _, message := range []string{end.GetError(), end.GetStatus()} {
		value, ok := strings.CutPrefix(strings.TrimSpace(message), "exit status ")
		if !ok {
			continue
		}
		exitCode, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil {
			return exitCode, nil
		}
	}
	message := strings.TrimSpace(end.GetError())
	if message == "" {
		message = strings.TrimSpace(end.GetStatus())
	}
	if message == "" {
		message = "process did not report an exit status"
	}
	return 0, errors.New(message)
}

func (c *cubeSDKClient) WriteFile(ctx context.Context, sandbox *cubeSandbox, filePath string, reader io.Reader, size int64) error {
	if sandbox == nil || strings.TrimSpace(sandbox.ID) == "" {
		return errors.New("cube sandbox file session is not connected")
	}
	if c == nil || c.data == nil {
		return errors.New("cube sandbox data client is not configured")
	}
	domainName := strings.TrimSpace(sandbox.Domain)
	if domainName == "" {
		domainName = c.sandboxDomain
	}
	target := url.URL{
		Scheme: c.proxyScheme,
		Host:   "49983-" + sandbox.ID + "." + domainName,
		Path:   "/files",
	}
	query := target.Query()
	query.Set("path", strings.TrimSpace(filePath))
	target.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), io.LimitReader(reader, size))
	if err != nil {
		return fmt.Errorf("create cube sandbox file request: %w", err)
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("root:")))
	if sandbox.EnvdAccessToken != "" {
		req.Header.Set("X-Access-Token", sandbox.EnvdAccessToken)
	}
	response, err := c.data.Do(req)
	if err != nil {
		return fmt.Errorf("send cube sandbox file request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return fmt.Errorf("cube sandbox file request failed: status=%d body=%s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

func (c *cubeSDKClient) ReadFile(ctx context.Context, sandbox *cubeSandbox, filePath string) (io.ReadCloser, int64, error) {
	if sandbox == nil || strings.TrimSpace(sandbox.ID) == "" {
		return nil, 0, errors.New("cube sandbox file session is not connected")
	}
	if c == nil || c.data == nil {
		return nil, 0, errors.New("cube sandbox data client is not configured")
	}
	domainName := strings.TrimSpace(sandbox.Domain)
	if domainName == "" {
		domainName = c.sandboxDomain
	}
	target := url.URL{Scheme: c.proxyScheme, Host: "49983-" + sandbox.ID + "." + domainName, Path: "/files"}
	query := target.Query()
	query.Set("path", strings.TrimSpace(filePath))
	target.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create cube sandbox file request: %w", err)
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("root:")))
	if sandbox.EnvdAccessToken != "" {
		req.Header.Set("X-Access-Token", sandbox.EnvdAccessToken)
	}
	response, err := c.data.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("send cube sandbox file request: %w", err)
	}
	if response.StatusCode >= http.StatusBadRequest {
		defer response.Body.Close()
		message, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return nil, 0, fmt.Errorf("cube sandbox file request failed: status=%d body=%s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	if response.ContentLength > domain.SandboxFileMaxBytes {
		response.Body.Close()
		return nil, 0, fmt.Errorf("cube sandbox file size must be between 0 and %d bytes", domain.SandboxFileMaxBytes)
	}
	if response.ContentLength < 0 {
		return spoolCubeSandboxFile(response.Body)
	}
	return response.Body, response.ContentLength, nil
}

type temporaryCubeSandboxFile struct {
	file *os.File
	path string
}

func (r *temporaryCubeSandboxFile) Read(data []byte) (int, error) {
	return r.file.Read(data)
}

func (r *temporaryCubeSandboxFile) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	closeErr := r.file.Close()
	removeErr := os.Remove(r.path)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(closeErr, removeErr)
}

func spoolCubeSandboxFile(source io.ReadCloser) (io.ReadCloser, int64, error) {
	if source == nil {
		return nil, 0, errors.New("cube sandbox file response body is required")
	}
	defer source.Close()
	temporaryDir := os.TempDir()
	if err := os.MkdirAll(temporaryDir, 0o700); err != nil {
		return nil, 0, fmt.Errorf("create cube sandbox temporary directory: %w", err)
	}
	temporaryDirInfo, err := os.Stat(temporaryDir)
	if err != nil {
		return nil, 0, fmt.Errorf("inspect cube sandbox temporary directory: %w", err)
	}
	if !temporaryDirInfo.IsDir() {
		return nil, 0, fmt.Errorf("cube sandbox temporary path %q is not a directory", temporaryDir)
	}
	temporary, err := os.CreateTemp(temporaryDir, "assistant-cube-file-*")
	if err != nil {
		return nil, 0, fmt.Errorf("create temporary cube sandbox file: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
		}
	}()
	size, err := io.Copy(temporary, io.LimitReader(source, domain.SandboxFileMaxBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("buffer cube sandbox file: %w", err)
	}
	if size > domain.SandboxFileMaxBytes {
		return nil, 0, fmt.Errorf("cube sandbox file exceeds %d bytes", domain.SandboxFileMaxBytes)
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("rewind temporary cube sandbox file: %w", err)
	}
	cleanup = false
	return &temporaryCubeSandboxFile{file: temporary, path: temporary.Name()}, size, nil
}

type cubeOutputBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *cubeOutputBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = b.truncated || len(data) > 0
		return written, nil
	}
	if len(data) > remaining {
		_, _ = b.buffer.Write(data[:remaining])
		b.truncated = true
		return written, nil
	}
	_, _ = b.buffer.Write(data)
	return written, nil
}

func (b *cubeOutputBuffer) String() string {
	const suffix = "\n[output truncated]\n"
	value := strings.ToValidUTF8(b.buffer.String(), "\ufffd")
	truncated := b.truncated || len(value) > b.limit
	if !truncated {
		return value
	}
	contentLimit := b.limit - len(suffix)
	if contentLimit <= 0 {
		return suffix[:b.limit]
	}
	return truncateValidUTF8(value, contentLimit) + suffix
}

func truncateValidUTF8(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.ValidString(value[:limit]) {
		limit--
	}
	return value[:limit]
}

func (c *cubeSDKClient) doControl(ctx context.Context, method string, path string, input any, output any) error {
	if c == nil || c.control == nil {
		return errors.New("cube sandbox control client is not configured")
	}
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("marshal cube sandbox request: %w", err)
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiURL+path, body)
	if err != nil {
		return fmt.Errorf("create cube sandbox request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	response, err := c.control.Do(req)
	if err != nil {
		return fmt.Errorf("send cube sandbox request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		return decodeCubeHTTPError(response)
	}
	if output == nil || response.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output); err != nil {
		return fmt.Errorf("decode cube sandbox response: %w", err)
	}
	return nil
}

func decodeCubeHTTPError(response *http.Response) error {
	message := fmt.Sprintf("cube sandbox request failed: status=%d", response.StatusCode)
	var payload struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
		Error   string `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload) == nil {
		for _, candidate := range []string{payload.Message, payload.Detail, payload.Error} {
			if strings.TrimSpace(candidate) != "" {
				message = strings.TrimSpace(candidate)
				break
			}
		}
	}
	switch response.StatusCode {
	case http.StatusNotFound, http.StatusGone:
		return fmt.Errorf("%w: %s", errCubeSandboxNotFound, message)
	case http.StatusConflict:
		return fmt.Errorf("%w: %s", errCubeSandboxConflict, message)
	default:
		return errors.New(message)
	}
}

func normalizeCubeSDKError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, cubesandbox.ErrSandboxNotFound) {
		return fmt.Errorf("%w: %v", errCubeSandboxNotFound, err)
	}
	var apiErr *cubesandbox.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusNotFound, http.StatusGone:
			return fmt.Errorf("%w: %v", errCubeSandboxNotFound, err)
		case http.StatusConflict:
			return fmt.Errorf("%w: %v", errCubeSandboxConflict, err)
		}
	}
	return err
}

func cubeSandboxFromSDK(sandbox *cubesandbox.Sandbox) *cubeSandbox {
	if sandbox == nil {
		return nil
	}
	return &cubeSandbox{
		ID:              strings.TrimSpace(sandbox.SandboxID),
		TemplateID:      strings.TrimSpace(sandbox.TemplateID),
		ClientID:        strings.TrimSpace(sandbox.ClientID),
		EnvdVersion:     strings.TrimSpace(sandbox.EnvdVersion),
		EnvdAccessToken: strings.TrimSpace(sandbox.EnvdAccessToken),
		Domain:          strings.TrimSpace(sandbox.Domain),
		SDK:             sandbox,
	}
}
