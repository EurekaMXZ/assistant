package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

var _ tool.SandboxManager = (*HTTPRuntime)(nil)

type HTTPRuntimeSettings struct {
	BaseURL           string
	Token             string
	HTTPClientTimeout time.Duration
	CommandTimeout    time.Duration
}

type HTTPRuntime struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHTTPRuntime(settings HTTPRuntimeSettings) (*HTTPRuntime, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("sandbox bridge url is required")
	}
	timeout := settings.HTTPClientTimeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	if commandTimeout := settings.CommandTimeout + 10*time.Second; settings.CommandTimeout > 0 && timeout < commandTimeout {
		timeout = commandTimeout
	}
	return &HTTPRuntime{
		baseURL: baseURL,
		token:   strings.TrimSpace(settings.Token),
		client:  &http.Client{Timeout: timeout},
	}, nil
}

func (r *HTTPRuntime) CreateSandbox(ctx context.Context, conversationID string, requestKey string) (*domain.SandboxHandle, error) {
	var response domain.SandboxHandle
	if err := r.doJSON(ctx, http.MethodPost, "/sandboxes", map[string]any{
		"conversation_id": strings.TrimSpace(conversationID),
	}, &response, requestKey); err != nil {
		return nil, err
	}
	return &response, nil
}

func (r *HTTPRuntime) DestroySandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error) {
	var response domain.SandboxHandle
	if err := r.doJSON(ctx, http.MethodDelete, "/sandboxes/"+pathEscape(handle.RuntimeID), nil, &response, requestKey); err != nil {
		return nil, err
	}
	return &response, nil
}

func (r *HTTPRuntime) StopSandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error) {
	return r.lifecycleRequest(ctx, handle, requestKey, "stop")
}

func (r *HTTPRuntime) ResumeSandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error) {
	return r.lifecycleRequest(ctx, handle, requestKey, "resume")
}

func (r *HTTPRuntime) lifecycleRequest(ctx context.Context, handle domain.SandboxHandle, requestKey string, operation string) (*domain.SandboxHandle, error) {
	var response domain.SandboxHandle
	path := "/sandboxes/" + pathEscape(handle.RuntimeID) + "/" + operation
	if err := r.doJSON(ctx, http.MethodPost, path, nil, &response, requestKey); err != nil {
		return nil, err
	}
	return &response, nil
}

func (r *HTTPRuntime) ExecSandboxCommand(ctx context.Context, handle domain.SandboxHandle, request domain.SandboxCommandRequest, requestKey string) (*domain.SandboxCommandResult, error) {
	var response domain.SandboxCommandResult
	if err := r.doJSON(ctx, http.MethodPost, "/sandboxes/"+pathEscape(handle.RuntimeID)+"/exec", request, &response, requestKey); err != nil {
		return nil, err
	}
	return &response, nil
}

func (r *HTTPRuntime) WriteSandboxFile(ctx context.Context, handle domain.SandboxHandle, path string, data []byte, requestKey string) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("sandbox http runtime is not configured")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("sandbox file path is required")
	}
	if int64(len(data)) > domain.SandboxFileMaxBytes {
		return fmt.Errorf("sandbox file exceeds %d bytes", domain.SandboxFileMaxBytes)
	}
	target := r.baseURL + "/sandboxes/" + pathEscape(handle.RuntimeID) + "/files?path=" + url.QueryEscape(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create sandbox bridge file request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	if requestKey = strings.TrimSpace(requestKey); requestKey != "" {
		req.Header.Set("Idempotency-Key", requestKey)
	}
	res, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("send sandbox bridge file request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= http.StatusBadRequest {
		message := readBridgeError(res)
		return errors.New(message)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

func (r *HTTPRuntime) doJSON(ctx context.Context, method string, path string, input any, output any, requestKey string) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("sandbox http runtime is not configured")
	}

	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("marshal sandbox bridge request: %w", err)
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, r.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create sandbox bridge request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	if requestKey = strings.TrimSpace(requestKey); requestKey != "" {
		req.Header.Set("Idempotency-Key", requestKey)
	}

	res, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("send sandbox bridge request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= http.StatusBadRequest {
		return errors.New(readBridgeError(res))
	}
	if output == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(output); err != nil {
		return fmt.Errorf("decode sandbox bridge response: %w", err)
	}
	return nil
}

func readBridgeError(res *http.Response) string {
	message := fmt.Sprintf("sandbox bridge request failed: status=%d", res.StatusCode)
	var payload struct {
		Error string `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&payload) == nil && strings.TrimSpace(payload.Error) != "" {
		return strings.TrimSpace(payload.Error)
	}
	return message
}

func pathEscape(value string) string {
	return url.PathEscape(strings.TrimSpace(value))
}
