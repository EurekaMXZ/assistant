package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/EurekaMXZ/assistant/internal/domain"
	process "github.com/TencentCloudAgentRuntime/ags-go-sdk/pb/process"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/pb/process/processconnect"
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
	writtenPath   string
	writtenData   []byte
	writeErr      error
	readPath      string
	readData      []byte
	readErr       error
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

func (s *stubCubeRuntimeClient) WriteFile(_ context.Context, _ *cubeSandbox, path string, reader io.Reader, _ int64) error {
	s.writtenPath = path
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.writtenData = append([]byte(nil), data...)
	return s.writeErr
}

func (s *stubCubeRuntimeClient) ReadFile(_ context.Context, _ *cubeSandbox, path string) (io.ReadCloser, int64, error) {
	s.readPath = path
	if s.readErr != nil {
		return nil, 0, s.readErr
	}
	data := append([]byte(nil), s.readData...)
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
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

func TestCubeRuntimeExecReturnsNonzeroExitAsCommandResult(t *testing.T) {
	client := &stubCubeRuntimeClient{
		connected:     &cubeSandbox{ID: "cube-1", TemplateID: "tpl-1"},
		commandResult: &cubeCommandResult{Output: "missing file\n", ExitCode: 1},
	}
	runtime := mustCubeRuntime(t, client)

	result, err := runtime.ExecSandboxCommand(t.Context(), domain.SandboxHandle{
		Provider: ProviderCubeSandbox, RuntimeID: "cube-1",
	}, domain.SandboxCommandRequest{Command: "cat", Args: []string{"missing.txt"}}, "exec-key")
	if err != nil {
		t.Fatalf("exec nonzero command: %v", err)
	}
	if result.ExitCode != 1 || result.Output != "missing file\n" || result.TimedOut {
		t.Fatalf("nonzero command result = %#v", result)
	}
}

func TestCubeRuntimeReadsAndWritesFilesThroughConnectedEnvdSession(t *testing.T) {
	client := &stubCubeRuntimeClient{connected: &cubeSandbox{ID: "cube-1", TemplateID: "tpl-1"}, readData: []byte("result\n")}
	runtime := mustCubeRuntime(t, client)
	handle := domain.SandboxHandle{Provider: ProviderCubeSandbox, RuntimeID: "cube-1"}
	if err := runtime.WriteSandboxFile(t.Context(), handle, "/workspace/input.csv", strings.NewReader("a,b\n"), 4, "write-key"); err != nil {
		t.Fatalf("write sandbox file: %v", err)
	}
	if client.connectCalls != 1 || client.writtenPath != "/workspace/input.csv" || string(client.writtenData) != "a,b\n" {
		t.Fatalf("unexpected file write: %#v", client)
	}
	reader, size, err := runtime.ReadSandboxFile(t.Context(), handle, "/workspace/result.txt")
	if err != nil {
		t.Fatalf("read sandbox file: %v", err)
	}
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || size != 7 || string(data) != "result\n" || client.readPath != "/workspace/result.txt" {
		t.Fatalf("unexpected file read: size=%d data=%q path=%q err=%v", size, data, client.readPath, err)
	}
}

func TestCubeSDKClientRunCommandUsesGeneratedEnvdClient(t *testing.T) {
	exitStatus := "exit status 3"
	handler := connect.NewServerStreamHandler(
		processconnect.ProcessStartProcedure,
		func(_ context.Context, request *connect.Request[process.StartRequest], stream *connect.ServerStream[process.StartResponse]) error {
			if got := request.Header().Get("Authorization"); got != "Basic cm9vdDo=" {
				t.Errorf("Authorization = %q", got)
			}
			if got := request.Header().Get("X-Access-Token"); got != "envd-token" {
				t.Errorf("X-Access-Token = %q", got)
			}
			if got := request.Header().Get("Connect-Timeout-Ms"); got == "" {
				t.Error("Connect-Timeout-Ms is empty")
			}
			if request.Msg.GetProcess().GetCmd() != "/bin/sh" || request.Msg.GetProcess().GetCwd() != "/" {
				t.Errorf("unexpected process config: %#v", request.Msg.GetProcess())
			}
			wantArgs := []string{"-c", "printf one", "assistant-sandbox"}
			if strings.Join(request.Msg.GetProcess().GetArgs(), "\x00") != strings.Join(wantArgs, "\x00") {
				t.Errorf("args = %#v, want %#v", request.Msg.GetProcess().GetArgs(), wantArgs)
			}

			for _, response := range []*process.StartResponse{
				{Event: &process.ProcessEvent{Event: &process.ProcessEvent_Start{Start: &process.ProcessEvent_StartEvent{Pid: 7}}}},
				{Event: &process.ProcessEvent{Event: &process.ProcessEvent_Data{Data: &process.ProcessEvent_DataEvent{Output: &process.ProcessEvent_DataEvent_Stdout{Stdout: []byte("one\n")}}}}},
				{Event: &process.ProcessEvent{Event: &process.ProcessEvent_Data{Data: &process.ProcessEvent_DataEvent{Output: &process.ProcessEvent_DataEvent_Stderr{Stderr: []byte("two\n")}}}}},
				{Event: &process.ProcessEvent{Event: &process.ProcessEvent_Data{Data: &process.ProcessEvent_DataEvent{Output: &process.ProcessEvent_DataEvent_Stdout{Stdout: []byte(strings.Repeat("x", 40))}}}}},
				{Event: &process.ProcessEvent{Event: &process.ProcessEvent_End{End: &process.ProcessEvent_EndEvent{ExitCode: 3, Exited: true, Status: exitStatus, Error: &exitStatus}}}},
			} {
				if err := stream.Send(response); err != nil {
					return err
				}
			}
			return nil
		},
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: time.Second}
	transport.DialContext = func(ctx context.Context, network string, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, server.Listener.Addr().String())
	}
	client := &cubeSDKClient{
		data:          &http.Client{Transport: transport},
		proxyScheme:   "http",
		sandboxDomain: "cube.test",
	}
	result, err := client.RunCommand(t.Context(), &cubeSandbox{
		ID:              "cube-1",
		EnvdAccessToken: "envd-token",
	}, "/bin/sh", cubeCommandOptions{
		Args:           []string{"-c", "printf one", "assistant-sandbox"},
		Timeout:        3 * time.Second,
		Cwd:            "/",
		MaxOutputBytes: 32,
	})
	if err != nil {
		t.Fatalf("run command: %v", err)
	}
	if result.ExitCode != 3 || len(result.Output) > 32 || !strings.HasPrefix(result.Output, "one\ntwo\n") || !strings.Contains(result.Output, "truncated") {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestCubeSDKClientStreamsFileToEnvd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/files" || request.URL.Query().Get("path") != "/workspace/input.csv" {
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.String())
		}
		if request.Header.Get("Authorization") != "Basic cm9vdDo=" || request.Header.Get("X-Access-Token") != "envd-token" {
			t.Errorf("unexpected auth headers: %#v", request.Header)
		}
		if request.ContentLength != 4 {
			t.Errorf("ContentLength = %d, want 4", request.ContentLength)
		}
		data, err := io.ReadAll(request.Body)
		if err != nil || string(data) != "a,b\n" {
			t.Errorf("body = %q err=%v", data, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: time.Second}
	transport.DialContext = func(ctx context.Context, network string, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, server.Listener.Addr().String())
	}
	client := &cubeSDKClient{data: &http.Client{Transport: transport}, proxyScheme: "http", sandboxDomain: "cube.test"}
	if err := client.WriteFile(t.Context(), &cubeSandbox{ID: "cube-1", EnvdAccessToken: "envd-token"}, "/workspace/input.csv", strings.NewReader("a,b\n"), 4); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestCubeSDKClientReadsChunkedFileFromEnvd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/files" || request.URL.Query().Get("path") != "/workspace/result.txt" {
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.String())
		}
		if request.Header.Get("Authorization") != "Basic cm9vdDo=" || request.Header.Get("X-Access-Token") != "envd-token" {
			t.Errorf("unexpected auth headers: %#v", request.Header)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("result\n"))
	}))
	defer server.Close()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: time.Second}
	transport.DialContext = func(ctx context.Context, network string, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, server.Listener.Addr().String())
	}
	client := &cubeSDKClient{data: &http.Client{Transport: transport}, proxyScheme: "http", sandboxDomain: "cube.test"}
	reader, size, err := client.ReadFile(t.Context(), &cubeSandbox{ID: "cube-1", EnvdAccessToken: "envd-token"}, "/workspace/result.txt")
	if err != nil {
		t.Fatalf("read chunked file: %v", err)
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || size != int64(len("result\n")) || string(data) != "result\n" {
		t.Fatalf("chunked file result: size=%d data=%q readErr=%v closeErr=%v", size, data, readErr, closeErr)
	}
}

func TestCubeCommandExitCodeClassifiesProcessExitAndInfrastructureFailure(t *testing.T) {
	exitStatus := "exit status 1"
	for _, end := range []*process.ProcessEvent_EndEvent{
		{ExitCode: 1, Exited: true, Error: &exitStatus},
		{Error: &exitStatus},
	} {
		exitCode, err := cubeCommandExitCode(end)
		if err != nil || exitCode != 1 {
			t.Fatalf("exit event = %#v, exitCode=%d err=%v", end, exitCode, err)
		}
	}

	infrastructureError := "process transport failed"
	if _, err := cubeCommandExitCode(&process.ProcessEvent_EndEvent{Error: &infrastructureError}); err == nil || !strings.Contains(err.Error(), infrastructureError) {
		t.Fatalf("infrastructure error = %v", err)
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
