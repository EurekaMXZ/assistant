package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	cubesandbox "github.com/tencentcloud/CubeSandbox/sdk/go"
)

type stubCubeRuntimeClient struct {
	created       *cubeSandbox
	connected     *cubeSandbox
	commandResult *cubeCommandResult
	createOptions cubeCreateOptions
	command       string
	commandOpts   cubeCommandOptions
	state         string
	inspectErr    error
	killErrors    []error
	pauseCalls    int
	connectCalls  int
	killCalls     int
}

func (s *stubCubeRuntimeClient) Create(_ context.Context, opts cubeCreateOptions) (*cubeSandbox, error) {
	s.createOptions = opts
	return s.created, nil
}

func (s *stubCubeRuntimeClient) Connect(_ context.Context, _ string) (*cubeSandbox, error) {
	s.connectCalls++
	return s.connected, nil
}

func (s *stubCubeRuntimeClient) Inspect(context.Context, string) (string, error) {
	return s.state, s.inspectErr
}

func (s *stubCubeRuntimeClient) Pause(context.Context, string, time.Duration) error {
	s.pauseCalls++
	return nil
}

func (s *stubCubeRuntimeClient) Kill(context.Context, string) error {
	s.killCalls++
	if len(s.killErrors) >= s.killCalls {
		return s.killErrors[s.killCalls-1]
	}
	return nil
}

func (s *stubCubeRuntimeClient) RunCommand(_ context.Context, _ *cubeSandbox, command string, opts cubeCommandOptions) (*cubeCommandResult, error) {
	s.command = command
	s.commandOpts = opts
	return s.commandResult, nil
}

func TestCubeRuntimeCreateUsesNeverTimeoutPolicyAndSafeMetadata(t *testing.T) {
	client := &stubCubeRuntimeClient{created: &cubeSandbox{
		ID:          "cube-1",
		TemplateID:  "tpl-1",
		ClientID:    "node-1",
		EnvdVersion: "1.2.3",
		Domain:      "cube.internal",
	}}
	runtime := mustCubeRuntime(t, client)

	handle, err := runtime.CreateSandbox(t.Context(), "conv-1", "run-1:call-1")
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	if handle.Provider != ProviderCubeSandbox || handle.RuntimeID != "cube-1" {
		t.Fatalf("unexpected handle: %#v", handle)
	}
	if client.createOptions.TemplateID != "tpl-1" || client.createOptions.ConversationID != "conv-1" || client.createOptions.RequestKey != "run-1:call-1" {
		t.Fatalf("unexpected create options: %#v", client.createOptions)
	}
	if client.createOptions.AllowInternetAccess {
		t.Fatal("expected internet access to default to denied")
	}

	var metadata cubeRuntimeMetadata
	if err := json.Unmarshal(handle.Metadata, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata.ClusterID != "cluster-1" || metadata.TemplateID != "tpl-1" || metadata.ClientID != "node-1" {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	if strings.Contains(string(handle.Metadata), "secret") || strings.Contains(string(handle.Metadata), "run-1:call-1") {
		t.Fatalf("runtime metadata contains credentials or request keys: %s", handle.Metadata)
	}
}

func TestCubeRuntimeRoutesPauseResumeAndPausedDestroy(t *testing.T) {
	client := &stubCubeRuntimeClient{
		state:      "paused",
		connected:  &cubeSandbox{ID: "cube-1", TemplateID: "tpl-1"},
		killErrors: []error{errCubeSandboxConflict, nil},
	}
	runtime := mustCubeRuntime(t, client)
	handle := domain.SandboxHandle{Provider: ProviderCubeSandbox, RuntimeID: "cube-1", Metadata: json.RawMessage(`{"template_id":"tpl-1"}`)}

	stopped, err := runtime.StopSandbox(t.Context(), handle, "stop-key")
	if err != nil {
		t.Fatalf("stop sandbox: %v", err)
	}
	if client.pauseCalls != 1 || string(stopped.Metadata) != string(handle.Metadata) {
		t.Fatalf("unexpected stop result: calls=%d handle=%#v", client.pauseCalls, stopped)
	}
	if _, err := runtime.ResumeSandbox(t.Context(), handle, "resume-key"); err != nil {
		t.Fatalf("resume sandbox: %v", err)
	}
	if _, err := runtime.DestroySandbox(t.Context(), handle, "destroy-key"); err != nil {
		t.Fatalf("destroy sandbox: %v", err)
	}
	if client.connectCalls != 2 {
		t.Fatalf("connect calls = %d, want resume plus paused-destroy resume", client.connectCalls)
	}
	if client.killCalls != 2 {
		t.Fatalf("kill calls = %d, want direct attempt plus post-resume retry", client.killCalls)
	}
}

func TestCubeRuntimeDestroyTreatsMissingSandboxAsSuccess(t *testing.T) {
	client := &stubCubeRuntimeClient{killErrors: []error{errCubeSandboxNotFound}}
	runtime := mustCubeRuntime(t, client)

	handle, err := runtime.DestroySandbox(t.Context(), domain.SandboxHandle{Provider: ProviderCubeSandbox, RuntimeID: "missing"}, "")
	if err != nil {
		t.Fatalf("destroy missing sandbox: %v", err)
	}
	if handle.RuntimeID != "missing" || client.killCalls != 1 {
		t.Fatalf("unexpected destroy result: handle=%#v killCalls=%d", handle, client.killCalls)
	}
}

func TestCubeRuntimeExecUsesArgvAndGuestTimeout(t *testing.T) {
	client := &stubCubeRuntimeClient{
		connected: &cubeSandbox{ID: "cube-1", TemplateID: "tpl-1"},
		commandResult: &cubeCommandResult{
			Output:   "command output",
			ExitCode: -1,
			TimedOut: true,
		},
	}
	runtime, err := newCubeRuntimeWithClient(CubeRuntimeSettings{
		APIURL:         "http://cube.internal:3000",
		APIKey:         "cube-key",
		TemplateID:     "tpl-1",
		ClusterID:      "cluster-1",
		MaxOutputBytes: 32,
	}, client)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runtime.ExecSandboxCommand(t.Context(), domain.SandboxHandle{
		Provider:  ProviderCubeSandbox,
		RuntimeID: "cube-1",
	}, domain.SandboxCommandRequest{
		Command:          "/usr/bin/printf",
		Args:             []string{"%s", "a'b; touch /tmp/bad"},
		WorkingDirectory: "/workspace",
		TimeoutSeconds:   3,
	}, "exec-key")
	if err != nil {
		t.Fatalf("exec sandbox command: %v", err)
	}
	wantCommand := "/bin/sh"
	if client.command != wantCommand {
		t.Fatalf("command = %q, want %q", client.command, wantCommand)
	}
	wantArgs := []string{"-c", "workdir=$1\nshift\nmkdir -p -- \"$workdir\" && cd -- \"$workdir\" && exec \"$@\"", "assistant-sandbox", "/workspace", "/usr/bin/printf", "%s", "a'b; touch /tmp/bad"}
	if strings.Join(client.commandOpts.Args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("args = %#v, want %#v", client.commandOpts.Args, wantArgs)
	}
	if client.commandOpts.Cwd != "/" || client.commandOpts.Timeout != 3*time.Second {
		t.Fatalf("unexpected command options: %#v", client.commandOpts)
	}
	if !result.TimedOut || result.ExitCode != -1 || result.RuntimeID != "cube-1" || result.WorkingDirectory != "/workspace" {
		t.Fatalf("unexpected command result: %#v", result)
	}
	if result.Output != "command output" {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestParseCubeCommandStreamPreservesOrderAndLimitsOutput(t *testing.T) {
	var stream bytes.Buffer
	writeCubeTestEnvelope(t, &stream, 0, map[string]any{"event": map[string]any{"data": map[string]any{
		"stdout": base64.StdEncoding.EncodeToString([]byte("one\n")),
	}}})
	writeCubeTestEnvelope(t, &stream, 0, map[string]any{"event": map[string]any{"data": map[string]any{
		"stderr": base64.StdEncoding.EncodeToString([]byte("two\n")),
	}}})
	writeCubeTestEnvelope(t, &stream, 0, map[string]any{"event": map[string]any{"data": map[string]any{
		"stdout": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 40))),
	}}})
	writeCubeTestEnvelope(t, &stream, 0, map[string]any{"event": map[string]any{"end": map[string]any{"exitCode": 0}}})
	writeCubeTestEnvelope(t, &stream, 0x02, map[string]any{})

	result, err := parseCubeCommandStream(&stream, 32)
	if err != nil {
		t.Fatalf("parse command stream: %v", err)
	}
	if result.ExitCode != 0 || len(result.Output) > 32 || !strings.HasPrefix(result.Output, "one\ntwo\n") || !strings.Contains(result.Output, "truncated") {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestCubeOutputLimitHoldsAfterInvalidUTF8Replacement(t *testing.T) {
	buffer := &cubeOutputBuffer{limit: 24}
	_, _ = buffer.Write(bytes.Repeat([]byte{0xff, 'a'}, 12))
	value := buffer.String()
	if len(value) > 24 || !strings.Contains(value, "truncated") {
		t.Fatalf("invalid UTF-8 output exceeded limit: len=%d value=%q", len(value), value)
	}
}

func TestCubeSDKClientReportsProtocolDeadlineAsTimeout(t *testing.T) {
	client := &cubeSDKClient{
		data: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})},
		proxyScheme:   "http",
		sandboxDomain: "cube.internal",
	}
	result, err := client.RunCommand(t.Context(), &cubeSandbox{ID: "cube-1"}, "sleep", cubeCommandOptions{
		Args:           []string{"60"},
		Timeout:        time.Millisecond,
		MaxOutputBytes: 1024,
	})
	if err != nil {
		t.Fatalf("run command: %v", err)
	}
	if !result.TimedOut || result.ExitCode != -1 {
		t.Fatalf("unexpected timeout result: %#v", result)
	}
}

func TestCubeWorkingDirectoryMatchesWorkspaceConvention(t *testing.T) {
	for input, want := range map[string]string{
		"":            "/workspace",
		"project/src": "/workspace/project/src",
		"/tmp/build":  "/tmp/build",
	} {
		if got := cubeWorkingDirectory(input); got != want {
			t.Fatalf("cubeWorkingDirectory(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNewCubeRuntimeValidatesRequiredSettings(t *testing.T) {
	for name, settings := range map[string]CubeRuntimeSettings{
		"api url":     {APIKey: "key", TemplateID: "tpl-1"},
		"api key":     {APIURL: "http://cube.internal:3000", TemplateID: "tpl-1"},
		"template id": {APIURL: "http://cube.internal:3000", APIKey: "key"},
		"api syntax":  {APIURL: "ftp://cube.internal", APIKey: "key", TemplateID: "tpl-1"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewCubeRuntime(settings); err == nil {
				t.Fatal("expected settings validation error")
			}
		})
	}
}

func TestCubeSDKClientCreateSendsLifecycleAndNetworkPolicy(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/sandboxes" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer cube-key" {
			t.Errorf("Authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"templateID":"tpl-1","sandboxID":"cube-1","clientID":"node-1","envdVersion":"1.2.3","domain":"cube.internal"}`))
	}))
	defer server.Close()

	client := newCubeSDKClient(CubeRuntimeSettings{
		APIURL:         server.URL,
		APIKey:         "cube-key",
		TemplateID:     "tpl-1",
		ProxyPortHTTP:  80,
		ProxyScheme:    "http",
		SandboxDomain:  "cube.internal",
		RequestTimeout: time.Second,
	})
	defer client.sdk.Close()
	sandbox, err := client.Create(t.Context(), cubeCreateOptions{
		TemplateID:     "tpl-1",
		ConversationID: "conv-1",
		RequestKey:     "run-1:call-1",
		AllowOut:       []string{"api.example.com"},
		DenyOut:        []string{"0.0.0.0/0"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sandbox.ID != "cube-1" || sandbox.ClientID != "node-1" {
		t.Fatalf("unexpected sandbox: %#v", sandbox)
	}
	if request["timeout"] != float64(-1) || request["allow_internet_access"] != false {
		t.Fatalf("unexpected lifecycle/network payload: %#v", request)
	}
	metadata, ok := request["metadata"].(map[string]any)
	if !ok || metadata["assistant.conversation_id"] != "conv-1" || metadata["assistant.request_key"] != "run-1:call-1" {
		t.Fatalf("unexpected request metadata: %#v", request["metadata"])
	}
	network, ok := request["network"].(map[string]any)
	if !ok || len(network["allowOut"].([]any)) != 1 || len(network["denyOut"].([]any)) != 1 {
		t.Fatalf("unexpected network payload: %#v", request["network"])
	}
}

func TestNormalizeCubeSDKErrorClassifiesNotFoundAndConflict(t *testing.T) {
	if !errors.Is(normalizeCubeSDKError(&cubesandbox.APIError{StatusCode: 404, Message: "missing"}), errCubeSandboxNotFound) {
		t.Fatal("expected not-found classification")
	}
	if !errors.Is(normalizeCubeSDKError(&cubesandbox.APIError{StatusCode: 409, Message: "paused"}), errCubeSandboxConflict) {
		t.Fatal("expected conflict classification")
	}
}

func mustCubeRuntime(t *testing.T, client cubeRuntimeClient) *CubeRuntime {
	t.Helper()
	runtime, err := newCubeRuntimeWithClient(CubeRuntimeSettings{
		APIURL:       "http://cube.internal:3000",
		APIKey:       "secret-api-key",
		TemplateID:   "tpl-1",
		ClusterID:    "cluster-1",
		AllowOut:     []string{"api.example.com"},
		DenyOut:      []string{"0.0.0.0/0"},
		PauseTimeout: 5 * time.Second,
	}, client)
	if err != nil {
		t.Fatalf("new cube runtime: %v", err)
	}
	return runtime
}

func writeCubeTestEnvelope(t *testing.T, buffer *bytes.Buffer, flags byte, value any) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var header [5]byte
	header[0] = flags
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	buffer.Write(header[:])
	buffer.Write(payload)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
