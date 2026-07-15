package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/tool"
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
	payload, err := json.Marshal(map[string]any{
		"process": map[string]any{
			"cmd":  command,
			"args": append([]string(nil), opts.Args...),
			"envs": map[string]string{},
			"cwd":  opts.Cwd,
		},
		"stdin": false,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal cube sandbox command: %w", err)
	}
	domain := sandbox.Domain
	if domain == "" {
		domain = c.sandboxDomain
	}
	host := "49983-" + sandbox.ID + "." + domain
	target := c.proxyScheme + "://" + host + "/process.Process/Start"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create cube sandbox command request: %w", err)
	}
	req.Header.Set("Content-Type", "application/connect+json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("root:")))
	if sandbox.EnvdAccessToken != "" {
		req.Header.Set("X-Access-Token", sandbox.EnvdAccessToken)
	}
	if opts.Timeout > 0 {
		req.Header.Set("Connect-Timeout-Ms", strconv.FormatInt(opts.Timeout.Milliseconds(), 10))
	}
	response, err := c.data.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &cubeCommandResult{Output: "command timed out", ExitCode: -1, TimedOut: true}, nil
		}
		return nil, fmt.Errorf("send cube sandbox command request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		return nil, decodeCubeHTTPError(response)
	}
	result, err := parseCubeCommandStream(response.Body, opts.MaxOutputBytes)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &cubeCommandResult{Output: "command timed out", ExitCode: -1, TimedOut: true}, nil
	}
	return result, err
}

type cubeProcessResponse struct {
	Event *struct {
		Data *struct {
			Stdout string `json:"stdout,omitempty"`
			Stderr string `json:"stderr,omitempty"`
			PTY    string `json:"pty,omitempty"`
		} `json:"data,omitempty"`
		End *struct {
			ExitCode      *int   `json:"exitCode,omitempty"`
			ExitCodeSnake *int   `json:"exit_code,omitempty"`
			Error         string `json:"error,omitempty"`
		} `json:"end,omitempty"`
	} `json:"event"`
}

func parseCubeCommandStream(reader io.Reader, maxOutputBytes int) (*cubeCommandResult, error) {
	if maxOutputBytes <= 0 {
		maxOutputBytes = defaultCubeMaxOutputBytes
	}
	output := &cubeOutputBuffer{limit: maxOutputBytes}
	result := &cubeCommandResult{}
	sawEnd := false
	for {
		flags, payload, err := readCubeConnectEnvelope(reader, maxOutputBytes)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if flags&0x01 != 0 {
			return nil, errors.New("compressed cube sandbox command events are not supported")
		}
		if flags&0x02 != 0 {
			var end struct {
				Error *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error,omitempty"`
			}
			if len(payload) > 0 {
				if err := json.Unmarshal(payload, &end); err != nil {
					return nil, fmt.Errorf("decode cube sandbox end stream: %w", err)
				}
				if end.Error != nil {
					return nil, fmt.Errorf("cube sandbox command stream %s: %s", end.Error.Code, end.Error.Message)
				}
			}
			continue
		}

		var event cubeProcessResponse
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, fmt.Errorf("decode cube sandbox command event: %w", err)
		}
		if event.Event == nil {
			continue
		}
		if event.Event.Data != nil {
			for _, encoded := range []string{event.Event.Data.Stdout, event.Event.Data.Stderr, event.Event.Data.PTY} {
				if encoded == "" {
					continue
				}
				decoded, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					return nil, fmt.Errorf("decode cube sandbox command output: %w", err)
				}
				_, _ = output.Write(decoded)
			}
		}
		if event.Event.End != nil {
			switch {
			case event.Event.End.ExitCode != nil:
				result.ExitCode = *event.Event.End.ExitCode
			case event.Event.End.ExitCodeSnake != nil:
				result.ExitCode = *event.Event.End.ExitCodeSnake
			case event.Event.End.Error != "":
				return nil, fmt.Errorf("cube sandbox command failed: %s", event.Event.End.Error)
			default:
				return nil, errors.New("cube sandbox command ended without an exit code")
			}
			sawEnd = true
		}
	}
	if !sawEnd {
		return nil, errors.New("cube sandbox command stream ended without an end event")
	}
	result.Output = output.String()
	return result, nil
}

func readCubeConnectEnvelope(reader io.Reader, maxOutputBytes int) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return 0, nil, err
	}
	size := int64(binary.BigEndian.Uint32(header[1:]))
	maxEnvelopeBytes := int64(maxOutputBytes)*2 + 64*1024
	if size > maxEnvelopeBytes {
		return 0, nil, fmt.Errorf("cube sandbox command event is too large: %d bytes", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	return header[0], payload, nil
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
	}
}
