package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestHTTPRuntimeCallsBridge(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bridge-token" {
			t.Fatalf("Authorization = %q, want bearer token", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Idempotency-Key") == "" {
			t.Fatal("missing Idempotency-Key header")
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /sandboxes":
			var request struct {
				ConversationID string `json:"conversation_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			if request.ConversationID != "conv-1" {
				t.Fatalf("ConversationID = %q, want conv-1", request.ConversationID)
			}
			_ = json.NewEncoder(w).Encode(domain.SandboxHandle{Provider: "firecracker", RuntimeID: "runtime-1"})
		case "POST /sandboxes/runtime-1/exec":
			var request domain.SandboxCommandRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode exec request: %v", err)
			}
			if request.Command != "echo" || len(request.Args) != 1 || request.Args[0] != "hello" {
				t.Fatalf("unexpected exec request: %#v", request)
			}
			_ = json.NewEncoder(w).Encode(domain.SandboxCommandResult{RuntimeID: "runtime-1", Command: "echo", Output: "hello\n"})
		case "POST /sandboxes/runtime-1/stop", "POST /sandboxes/runtime-1/resume":
			_ = json.NewEncoder(w).Encode(domain.SandboxHandle{Provider: "firecracker", RuntimeID: "runtime-1"})
		case "DELETE /sandboxes/runtime-1":
			_ = json.NewEncoder(w).Encode(domain.SandboxHandle{Provider: "firecracker", RuntimeID: "runtime-1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runtime, err := NewHTTPRuntime(HTTPRuntimeSettings{BaseURL: server.URL, Token: "bridge-token", HTTPClientTimeout: time.Second})
	if err != nil {
		t.Fatalf("new http runtime: %v", err)
	}

	handle, err := runtime.CreateSandbox(context.Background(), " conv-1 ", "create-key")
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	if handle.Provider != "firecracker" || handle.RuntimeID != "runtime-1" {
		t.Fatalf("unexpected handle: %#v", handle)
	}
	if _, err := runtime.StopSandbox(context.Background(), *handle, "stop-key"); err != nil {
		t.Fatalf("stop sandbox: %v", err)
	}
	if _, err := runtime.ResumeSandbox(context.Background(), *handle, "resume-key"); err != nil {
		t.Fatalf("resume sandbox: %v", err)
	}

	result, err := runtime.ExecSandboxCommand(context.Background(), *handle, domain.SandboxCommandRequest{Command: "echo", Args: []string{"hello"}}, "exec-key")
	if err != nil {
		t.Fatalf("exec command: %v", err)
	}
	if result.Output != "hello\n" {
		t.Fatalf("Output = %q, want hello", result.Output)
	}

	destroyed, err := runtime.DestroySandbox(context.Background(), *handle, "destroy-key")
	if err != nil {
		t.Fatalf("destroy sandbox: %v", err)
	}
	if destroyed.RuntimeID != "runtime-1" {
		t.Fatalf("unexpected destroyed handle: %#v", destroyed)
	}

	want := []string{"POST /sandboxes", "POST /sandboxes/runtime-1/stop", "POST /sandboxes/runtime-1/resume", "POST /sandboxes/runtime-1/exec", "DELETE /sandboxes/runtime-1"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestHTTPRuntimeReturnsBridgeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "bridge failed"})
	}))
	defer server.Close()

	runtime, err := NewHTTPRuntime(HTTPRuntimeSettings{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("new http runtime: %v", err)
	}
	_, err = runtime.CreateSandbox(context.Background(), "conv-1", "request-key")
	if err == nil || err.Error() != "bridge failed" {
		t.Fatalf("error = %v, want bridge failed", err)
	}
}

func TestNewHTTPRuntimeRequiresBaseURL(t *testing.T) {
	_, err := NewHTTPRuntime(HTTPRuntimeSettings{})
	if err == nil || err.Error() != "sandbox bridge url is required" {
		t.Fatalf("error = %v, want missing bridge url", err)
	}
}
