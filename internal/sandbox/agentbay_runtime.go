package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/tool"
	aliyunagentbay "github.com/aliyun/wuying-agentbay-sdk/golang/pkg/agentbay"
	agentbaycommand "github.com/aliyun/wuying-agentbay-sdk/golang/pkg/agentbay/command"
)

const (
	defaultAgentBayRegion        = "cn-hangzhou"
	defaultAgentBayImage         = "code_latest"
	defaultAgentBayAPITimeout    = time.Minute
	defaultCommandTimeoutSeconds = 30
	agentBayConversationLabel    = "assistant_conversation_id"
	agentBayRequestKeyHashLabel  = "assistant_request_key_hash"
)

var _ tool.SandboxManager = (*AgentBayRuntime)(nil)

type AgentBayRuntimeSettings struct {
	APIKey           string
	RegionID         string
	ImageID          string
	PolicyID         string
	APITimeout       time.Duration
	CommandTimeout   time.Duration
	LifecycleTimeout time.Duration
}

type AgentBayRuntime struct {
	client                  agentBayClient
	regionID                string
	imageID                 string
	policyID                string
	lifecycleTimeoutSeconds int
	now                     func() time.Time
}

type agentBayClient interface {
	Find(labels map[string]string, imageID string) (agentBayFindResult, error)
	Create(request agentBayCreateRequest) (agentBayCreateResult, error)
	Session(sessionID string) agentBaySession
}

type agentBaySession interface {
	ID() string
	Status() (string, error)
	Pause(timeoutSeconds int) (agentBayLifecycleResult, error)
	Resume(timeoutSeconds int) (agentBayLifecycleResult, error)
	Delete() (agentBayDeleteResult, error)
	ExecuteCommand(command string, timeoutMs int, workingDirectory string) (agentBayCommandResult, error)
}

type agentBayCreateRequest struct {
	ImageID  string
	PolicyID string
	Labels   map[string]string
}

type agentBayCreateResult struct {
	Session   agentBaySession
	RequestID string
}

type agentBayFindResult struct {
	Session   agentBaySession
	RequestID string
}

type agentBayDeleteResult struct {
	Success      bool
	Accepted     bool
	NotFound     bool
	RequestID    string
	ErrorMessage string
}

type agentBayLifecycleResult struct {
	Success      bool
	Status       string
	RequestID    string
	ErrorMessage string
}

type agentBayCommandResult struct {
	Output       string
	Stdout       string
	Stderr       string
	ExitCode     int
	ErrorMessage string
	TimedOut     bool
}

func NewAgentBayRuntime(settings AgentBayRuntimeSettings) (*AgentBayRuntime, error) {
	settings = normalizeAgentBaySettings(settings)
	if settings.APIKey == "" {
		return nil, fmt.Errorf("AgentBay API key is required")
	}
	if settings.APITimeout.Milliseconds() > int64(maxInt()) {
		return nil, fmt.Errorf("AgentBay API timeout is too large")
	}

	client, err := aliyunagentbay.NewAgentBay(settings.APIKey, aliyunagentbay.WithConfig(&aliyunagentbay.Config{
		RegionID:  settings.RegionID,
		TimeoutMs: int(settings.APITimeout.Milliseconds()),
	}))
	if err != nil {
		return nil, fmt.Errorf("create AgentBay client: %w", err)
	}
	return newAgentBayRuntime(settings, &sdkAgentBayClient{client: client}), nil
}

func newAgentBayRuntime(settings AgentBayRuntimeSettings, client agentBayClient) *AgentBayRuntime {
	settings = normalizeAgentBaySettings(settings)
	return &AgentBayRuntime{
		client:                  client,
		regionID:                settings.RegionID,
		imageID:                 settings.ImageID,
		policyID:                settings.PolicyID,
		lifecycleTimeoutSeconds: max(1, int(settings.LifecycleTimeout.Seconds())),
		now:                     time.Now,
	}
}

func (r *AgentBayRuntime) CreateSandbox(ctx context.Context, conversationID string, requestKey string) (*domain.SandboxHandle, error) {
	if err := r.ready(ctx); err != nil {
		return nil, err
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return nil, fmt.Errorf("conversation ID is required")
	}

	labels := map[string]string{agentBayConversationLabel: conversationID}
	if requestKey = strings.TrimSpace(requestKey); requestKey != "" {
		labels[agentBayRequestKeyHashLabel] = hashLabelValue(requestKey)
		found, err := r.client.Find(labels, r.imageID)
		if err != nil {
			return nil, fmt.Errorf("find existing AgentBay session: %w", err)
		}
		if found.Session != nil && strings.TrimSpace(found.Session.ID()) != "" {
			return r.handle(found.Session.ID(), conversationID, true, map[string]any{
				"lookup_request_id": strings.TrimSpace(found.RequestID),
				"reused":            true,
			})
		}
	}
	result, err := r.client.Create(agentBayCreateRequest{
		ImageID:  r.imageID,
		PolicyID: r.policyID,
		Labels:   labels,
	})
	if err != nil {
		return nil, fmt.Errorf("create AgentBay session: %w", err)
	}
	if result.Session == nil || strings.TrimSpace(result.Session.ID()) == "" {
		return nil, fmt.Errorf("create AgentBay session: response did not include a session ID")
	}
	if err := ctx.Err(); err != nil {
		_, _ = result.Session.Delete()
		return nil, err
	}

	return r.handle(result.Session.ID(), conversationID, false, map[string]any{
		"created_at":        r.now().UTC().Format(time.RFC3339Nano),
		"create_request_id": strings.TrimSpace(result.RequestID),
	})
}

func (r *AgentBayRuntime) DestroySandbox(ctx context.Context, handle domain.SandboxHandle, _ string) (*domain.SandboxHandle, error) {
	runtimeID, session, err := r.lifecycleSession(ctx, handle)
	if err != nil {
		return nil, err
	}
	status, err := session.Status()
	if err != nil {
		if isAgentBayNotFound(err.Error()) {
			return &domain.SandboxHandle{Provider: ProviderAgentBay, RuntimeID: runtimeID, Metadata: append(json.RawMessage(nil), handle.Metadata...)}, nil
		}
		return nil, fmt.Errorf("get AgentBay session %q status before deletion: %w", runtimeID, err)
	}
	if isAgentBayPausedStatus(status) {
		resumeResult, resumeErr := session.Resume(r.lifecycleTimeoutSeconds)
		if resumeErr != nil {
			return nil, fmt.Errorf("resume AgentBay session %q before deletion: %w", runtimeID, resumeErr)
		}
		if !resumeResult.Success {
			return nil, fmt.Errorf("resume AgentBay session %q before deletion: %s", runtimeID, firstNonEmpty(resumeResult.ErrorMessage, resumeResult.Status, "unknown error"))
		}
	}
	result, err := session.Delete()
	if err != nil {
		return nil, fmt.Errorf("delete AgentBay session %q: %w", handle.RuntimeID, err)
	}
	if result.Accepted {
		return nil, fmt.Errorf("delete AgentBay session %q is not yet confirmed: %s", runtimeID, firstNonEmpty(result.ErrorMessage, "deletion pending"))
	}
	if !result.Success && !result.NotFound {
		message := strings.TrimSpace(result.ErrorMessage)
		if message == "" {
			message = "AgentBay session deletion failed"
		}
		return nil, fmt.Errorf("delete AgentBay session %q: %s", handle.RuntimeID, message)
	}
	return &domain.SandboxHandle{Provider: ProviderAgentBay, RuntimeID: runtimeID, Metadata: append(json.RawMessage(nil), handle.Metadata...)}, nil
}

func (r *AgentBayRuntime) StopSandbox(ctx context.Context, handle domain.SandboxHandle, _ string) (*domain.SandboxHandle, error) {
	runtimeID, session, err := r.lifecycleSession(ctx, handle)
	if err != nil {
		return nil, err
	}
	status, err := session.Status()
	if err != nil {
		return nil, fmt.Errorf("get AgentBay session %q status: %w", runtimeID, err)
	}
	if strings.EqualFold(status, aliyunagentbay.SessionStatusPaused.String()) {
		return r.lifecycleHandle(handle, "stopped", r.now().UTC())
	}
	result, err := session.Pause(r.lifecycleTimeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("pause AgentBay session %q: %w", runtimeID, err)
	}
	if !result.Success {
		return nil, fmt.Errorf("pause AgentBay session %q: %s", runtimeID, firstNonEmpty(result.ErrorMessage, result.Status, "unknown error"))
	}
	return r.lifecycleHandle(handle, "stopped", r.now().UTC())
}

func (r *AgentBayRuntime) ResumeSandbox(ctx context.Context, handle domain.SandboxHandle, _ string) (*domain.SandboxHandle, error) {
	runtimeID, session, err := r.lifecycleSession(ctx, handle)
	if err != nil {
		return nil, err
	}
	status, err := session.Status()
	if err != nil {
		return nil, fmt.Errorf("get AgentBay session %q status: %w", runtimeID, err)
	}
	if strings.EqualFold(status, aliyunagentbay.SessionStatusRunning.String()) {
		return r.lifecycleHandle(handle, "active", r.now().UTC())
	}
	result, err := session.Resume(r.lifecycleTimeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("resume AgentBay session %q: %w", runtimeID, err)
	}
	if !result.Success {
		return nil, fmt.Errorf("resume AgentBay session %q: %s", runtimeID, firstNonEmpty(result.ErrorMessage, result.Status, "unknown error"))
	}
	return r.lifecycleHandle(handle, "active", r.now().UTC())
}

func (r *AgentBayRuntime) lifecycleSession(ctx context.Context, handle domain.SandboxHandle) (string, agentBaySession, error) {
	runtimeID, err := r.runtimeID(ctx, handle)
	if err != nil {
		return "", nil, err
	}
	session := r.client.Session(runtimeID)
	if session == nil {
		return "", nil, fmt.Errorf("get AgentBay session %q: empty response", runtimeID)
	}
	return runtimeID, session, nil
}

func (r *AgentBayRuntime) lifecycleHandle(handle domain.SandboxHandle, state string, at time.Time) (*domain.SandboxHandle, error) {
	metadata := map[string]any{}
	if len(handle.Metadata) > 0 {
		_ = json.Unmarshal(handle.Metadata, &metadata)
	}
	metadata["lifecycle_state"] = state
	metadata[state+"_at"] = at.Format(time.RFC3339Nano)
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal AgentBay lifecycle metadata: %w", err)
	}
	return &domain.SandboxHandle{Provider: ProviderAgentBay, RuntimeID: strings.TrimSpace(handle.RuntimeID), Metadata: encoded}, nil
}

func (r *AgentBayRuntime) ExecSandboxCommand(ctx context.Context, handle domain.SandboxHandle, request domain.SandboxCommandRequest, _ string) (*domain.SandboxCommandResult, error) {
	if _, err := r.ResumeSandbox(ctx, handle, ""); err != nil {
		return nil, err
	}
	_, session, err := r.lifecycleSession(ctx, handle)
	if err != nil {
		return nil, err
	}
	command := strings.TrimSpace(request.Command)
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}

	timeoutSeconds := request.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultCommandTimeoutSeconds
	}
	if timeoutSeconds > maxInt()/1000 {
		return nil, fmt.Errorf("command timeout is too large")
	}
	workingDirectory := strings.TrimSpace(request.WorkingDirectory)
	// AgentBay otherwise returns separate completed buffers with no cross-stream ordering.
	result, err := session.ExecuteCommand(joinShellCommand(command, request.Args)+" 2>&1", timeoutSeconds*1000, workingDirectory)
	if err != nil {
		return nil, fmt.Errorf("execute AgentBay command: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	output := result.Output
	if output == "" {
		output = mergeLegacyCommandOutput(result.Stdout, result.Stderr)
	}
	if output == "" && result.ExitCode != 0 {
		output = result.ErrorMessage
	}
	return &domain.SandboxCommandResult{
		RuntimeID:        strings.TrimSpace(handle.RuntimeID),
		Command:          command,
		Args:             append([]string(nil), request.Args...),
		WorkingDirectory: workingDirectory,
		Output:           output,
		ExitCode:         result.ExitCode,
		TimedOut:         result.TimedOut,
	}, nil
}

func mergeLegacyCommandOutput(stdout string, stderr string) string {
	if stdout == "" {
		return stderr
	}
	if stderr == "" {
		return stdout
	}
	if strings.HasSuffix(stdout, "\n") {
		return stdout + stderr
	}
	return stdout + "\n" + stderr
}

func (r *AgentBayRuntime) ready(ctx context.Context) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("AgentBay sandbox runtime is not configured")
	}
	return ctx.Err()
}

func (r *AgentBayRuntime) runtimeID(ctx context.Context, handle domain.SandboxHandle) (string, error) {
	if err := r.ready(ctx); err != nil {
		return "", err
	}
	if normalizeProvider(handle.Provider) != ProviderAgentBay {
		return "", fmt.Errorf("AgentBay runtime cannot handle sandbox provider %q", handle.Provider)
	}
	runtimeID := strings.TrimSpace(handle.RuntimeID)
	if runtimeID == "" {
		return "", fmt.Errorf("sandbox runtime ID is required")
	}
	return runtimeID, nil
}

func (r *AgentBayRuntime) handle(sessionID string, conversationID string, reused bool, extra map[string]any) (*domain.SandboxHandle, error) {
	metadata := map[string]any{
		"conversation_id": conversationID,
		"image_id":        r.imageID,
		"lifecycle":       "manual_release",
		"region_id":       r.regionID,
	}
	for key, value := range extra {
		metadata[key] = value
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal AgentBay sandbox metadata: %w", err)
	}
	return &domain.SandboxHandle{Provider: ProviderAgentBay, RuntimeID: strings.TrimSpace(sessionID), Metadata: encoded, Reused: reused}, nil
}

func normalizeAgentBaySettings(settings AgentBayRuntimeSettings) AgentBayRuntimeSettings {
	settings.APIKey = strings.TrimSpace(settings.APIKey)
	settings.RegionID = strings.TrimSpace(settings.RegionID)
	if settings.RegionID == "" {
		settings.RegionID = defaultAgentBayRegion
	}
	settings.ImageID = strings.TrimSpace(settings.ImageID)
	if settings.ImageID == "" {
		settings.ImageID = defaultAgentBayImage
	}
	settings.PolicyID = strings.TrimSpace(settings.PolicyID)
	if settings.APITimeout <= 0 {
		settings.APITimeout = defaultAgentBayAPITimeout
	}
	if settings.LifecycleTimeout <= 0 {
		settings.LifecycleTimeout = settings.APITimeout
	}
	if commandTimeout := settings.CommandTimeout + 10*time.Second; settings.CommandTimeout > 0 && settings.APITimeout < commandTimeout {
		settings.APITimeout = commandTimeout
	}
	return settings
}

func joinShellCommand(command string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(command))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func hashLabelValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

type sdkAgentBayClient struct {
	client *aliyunagentbay.AgentBay
}

func (c *sdkAgentBayClient) Find(labels map[string]string, imageID string) (agentBayFindResult, error) {
	limit := int32(10)
	result, err := c.client.List(aliyunagentbay.SessionStatusRunning.String(), labels, nil, &limit, imageID)
	if err != nil {
		return agentBayFindResult{}, err
	}
	if result == nil {
		return agentBayFindResult{}, fmt.Errorf("AgentBay session lookup returned an empty response")
	}
	for _, item := range result.SessionIds {
		sessionID, _ := item["sessionId"].(string)
		if strings.TrimSpace(sessionID) != "" {
			return agentBayFindResult{Session: c.Session(sessionID), RequestID: result.RequestID}, nil
		}
	}
	return agentBayFindResult{RequestID: result.RequestID}, nil
}

func (c *sdkAgentBayClient) Create(request agentBayCreateRequest) (agentBayCreateResult, error) {
	params := aliyunagentbay.NewCreateSessionParams().WithImageId(request.ImageID).WithPolicyId(request.PolicyID).WithLabels(request.Labels)
	params.LifecyclePolicy = aliyunagentbay.NewLifecyclePolicyManualRelease()
	result, err := c.client.Create(params)
	if err != nil {
		return agentBayCreateResult{}, err
	}
	if result == nil || !result.Success || result.Session == nil {
		message := "AgentBay session creation failed"
		if result != nil && strings.TrimSpace(result.ErrorMessage) != "" {
			message = result.ErrorMessage
		}
		return agentBayCreateResult{}, fmt.Errorf("%s", message)
	}
	return agentBayCreateResult{Session: &sdkAgentBaySession{session: result.Session}, RequestID: result.RequestID}, nil
}

func (c *sdkAgentBayClient) Session(sessionID string) agentBaySession {
	return &sdkAgentBaySession{session: aliyunagentbay.NewSession(c.client, sessionID)}
}

type sdkAgentBaySession struct {
	session *aliyunagentbay.Session
}

func (s *sdkAgentBaySession) ID() string {
	return s.session.SessionID
}

func (s *sdkAgentBaySession) Delete() (agentBayDeleteResult, error) {
	result, err := s.session.Delete(false)
	if err != nil {
		if isAgentBayNotFound(err.Error()) {
			return agentBayDeleteResult{NotFound: true}, nil
		}
		return agentBayDeleteResult{}, err
	}
	if result == nil {
		return agentBayDeleteResult{}, fmt.Errorf("AgentBay session deletion returned an empty response")
	}
	return agentBayDeleteResult{
		Success:      result.Success,
		Accepted:     isAgentBayDeleteAccepted(result.ErrorMessage),
		NotFound:     isAgentBayNotFound(result.ErrorMessage),
		RequestID:    result.RequestID,
		ErrorMessage: result.ErrorMessage,
	}, nil
}

func (s *sdkAgentBaySession) Status() (string, error) {
	result, err := s.session.GetStatus()
	if err != nil {
		return "", err
	}
	if result == nil || !result.Success {
		message := "empty response"
		if result != nil && strings.TrimSpace(result.ErrorMessage) != "" {
			message = result.ErrorMessage
		}
		return "", fmt.Errorf("%s", message)
	}
	return strings.TrimSpace(result.Status), nil
}

func (s *sdkAgentBaySession) Pause(timeoutSeconds int) (agentBayLifecycleResult, error) {
	result, err := s.session.BetaPause(timeoutSeconds, 2)
	if err != nil {
		return agentBayLifecycleResult{}, err
	}
	if result == nil {
		return agentBayLifecycleResult{}, fmt.Errorf("AgentBay pause returned an empty response")
	}
	return agentBayLifecycleResult{Success: result.Success, Status: result.Status, RequestID: result.RequestID, ErrorMessage: result.ErrorMessage}, nil
}

func (s *sdkAgentBaySession) Resume(timeoutSeconds int) (agentBayLifecycleResult, error) {
	result, err := s.session.BetaResume(timeoutSeconds, 2)
	if err != nil {
		return agentBayLifecycleResult{}, err
	}
	if result == nil {
		return agentBayLifecycleResult{}, fmt.Errorf("AgentBay resume returned an empty response")
	}
	return agentBayLifecycleResult{Success: result.Success, Status: result.Status, RequestID: result.RequestID, ErrorMessage: result.ErrorMessage}, nil
}

func (s *sdkAgentBaySession) ExecuteCommand(command string, timeoutMs int, workingDirectory string) (agentBayCommandResult, error) {
	options := []interface{}{agentbaycommand.WithTimeoutMs(timeoutMs)}
	if workingDirectory != "" {
		options = append(options, agentbaycommand.WithCwd(workingDirectory))
	}
	result, err := s.session.Command.ExecuteCommand(command, options...)
	if err != nil {
		return agentBayCommandResult{}, err
	}
	if result == nil {
		return agentBayCommandResult{}, fmt.Errorf("AgentBay command returned an empty response")
	}
	return agentBayCommandResult{
		Output:       result.Output,
		Stdout:       result.Stdout,
		Stderr:       result.Stderr,
		ExitCode:     result.ExitCode,
		ErrorMessage: result.ErrorMessage,
		TimedOut:     !result.Success && isAgentBayTimeout(result.ErrorMessage+" "+result.Stderr),
	}, nil
}

func isAgentBayDeleteAccepted(message string) bool {
	return strings.Contains(strings.ToLower(message), "timeout waiting for session deletion")
}

func isAgentBayPausedStatus(status string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(status)), "PAUS")
}

func isAgentBayNotFound(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "notfound") || strings.Contains(message, "not found")
}

func isAgentBayTimeout(message string) bool {
	message = strings.ToLower(message)
	for _, marker := range []string{"timed out", "timeout exceeded", "timeout waiting", "command timeout", "execution timeout", "deadline exceeded"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
