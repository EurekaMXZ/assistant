package mcpconfig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/credential"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type runtimeRepositoryStub struct {
	tools     []RuntimeTool
	listOwner string
	getOwner  string
	getErr    error
}

func (r *runtimeRepositoryStub) ListEnabledRuntimeTools(_ context.Context, ownerUserID string) ([]RuntimeTool, error) {
	r.listOwner = ownerUserID
	return append([]RuntimeTool(nil), r.tools...), nil
}

func (r *runtimeRepositoryStub) GetEnabledRuntimeTool(_ context.Context, ownerUserID string, serverID string, toolName string) (*RuntimeTool, error) {
	r.getOwner = ownerUserID
	if r.getErr != nil {
		return nil, r.getErr
	}
	for index := range r.tools {
		if r.tools[index].ServerID == serverID && r.tools[index].ToolName == toolName {
			result := r.tools[index]
			return &result, nil
		}
	}
	return nil, domain.ErrNotFound
}

type fixedCatalog []llm.ModelTool

func (c fixedCatalog) ListTools(context.Context, tool.ToolScope) ([]llm.ModelTool, error) {
	return append([]llm.ModelTool(nil), c...), nil
}

type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network interrupted")
}

func TestRuntimeToolNameIsStableSafeAndCollisionResistant(t *testing.T) {
	first := RuntimeTool{ServerID: "server-1", ToolName: "lookup customer records with a very long name that must be bounded"}
	name := RuntimeToolName(first)
	if name != RuntimeToolName(first) || len(name) > maxProviderToolName || !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(name) {
		t.Fatalf("runtime tool name = %q", name)
	}
	space := RuntimeToolName(RuntimeTool{ServerID: "server-1", ToolName: "a b"})
	at := RuntimeToolName(RuntimeTool{ServerID: "server-1", ToolName: "a@b"})
	if space == at {
		t.Fatalf("SafeToolName collision was not disambiguated: %q", space)
	}
	otherServer := RuntimeToolName(RuntimeTool{ServerID: "server-2", ToolName: "a b"})
	if space == otherServer {
		t.Fatalf("server identity was not included in name: %q", space)
	}
}

func TestCompositeRuntimeCatalogChecksOwnerAndGlobalNameConflicts(t *testing.T) {
	runtimeTool := RuntimeTool{
		ServerID: "server-1", ServerName: "CRM", ToolName: "lookup", Description: "Find a customer",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
	repository := &runtimeRepositoryStub{tools: []RuntimeTool{runtimeTool}}
	runtime := &CompositeRuntime{
		StaticCatalog: fixedCatalog{{Type: llm.ModelToolTypeFunction, Name: "builtin_lookup"}},
		Repository:    repository,
	}
	tools, err := runtime.ListTools(t.Context(), tool.ToolScope{OwnerUserID: "owner-1"})
	if err != nil {
		t.Fatal(err)
	}
	if repository.listOwner != "owner-1" || len(tools) != 2 || tools[0].Name != "builtin_lookup" || tools[1].Name != RuntimeToolName(runtimeTool) || !strings.Contains(tools[1].Description, "CRM") {
		t.Fatalf("owner=%q tools=%#v", repository.listOwner, tools)
	}
	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "endpoint") || strings.Contains(string(encoded), "header") {
		t.Fatalf("catalog exposed runtime configuration: %s", encoded)
	}

	repository.listOwner = ""
	withoutOwner, err := runtime.ListTools(t.Context(), tool.ToolScope{})
	if err != nil || len(withoutOwner) != 1 || repository.listOwner != "" {
		t.Fatalf("unowned catalog tools=%#v owner=%q err=%v", withoutOwner, repository.listOwner, err)
	}

	runtime.StaticCatalog = fixedCatalog{{Type: llm.ModelToolTypeFunction, Name: RuntimeToolName(runtimeTool)}}
	if _, err := runtime.ListTools(t.Context(), tool.ToolScope{OwnerUserID: "owner-1"}); err == nil {
		t.Fatal("catalog accepted static/runtime provider name collision")
	}
}

func TestCompositeRuntimeRejectsDisabledToolForOwner(t *testing.T) {
	runtimeTool := RuntimeTool{ServerID: "server-1", ToolName: "lookup", InputSchema: json.RawMessage(`{}`)}
	repository := &runtimeRepositoryStub{tools: []RuntimeTool{runtimeTool}, getErr: domain.ErrNotFound}
	runtime := &CompositeRuntime{Repository: repository}
	_, err := runtime.Execute(t.Context(), tool.ToolScope{OwnerUserID: "owner-1"}, tool.ToolCall{Name: RuntimeToolName(runtimeTool)})
	if err == nil || !tool.IsRecoverableError(err) {
		t.Fatalf("disabled tool error = %v, want recoverable rejection", err)
	}
	if repository.listOwner != "owner-1" || repository.getOwner != "owner-1" {
		t.Fatalf("runtime owner checks list=%q get=%q", repository.listOwner, repository.getOwner)
	}
}

func TestCompositeRuntimeCallsSDKStreamableHTTPTool(t *testing.T) {
	type toolInput struct {
		Query string `json:"query"`
	}
	mcpServer := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "runtime-test", Version: "1.0.0"}, nil)
	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{Name: "lookup", Description: "Lookup"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, input toolInput) (*mcpsdk.CallToolResult, any, error) {
			if input.Query == "fail" {
				return &mcpsdk.CallToolResult{
					Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "not found"}}, IsError: true,
				}, nil, nil
			}
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "result:" + input.Query}},
			}, nil, nil
		})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return mcpServer }, nil)
	testServer := httptest.NewServer(handler)
	defer testServer.Close()
	targetURL, err := url.Parse(testServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	rewriter := &rewriteTransport{base: http.DefaultTransport, target: targetURL}
	client := &SDKToolLister{httpClientFactory: func(_ *url.URL, parameters map[string]string, headers map[string]string) *http.Client {
		return &http.Client{Transport: &requestPolicyTransport{base: rewriter, parameters: parameters, headers: headers}}
	}}
	cipher, err := credential.NewCipher(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	serverID := "48eb3032-829c-4a17-8eda-58607ae59ea0"
	parameters, parametersNonce, err := cipher.Encrypt(serverID, parametersPurpose, `{"api_key":"query-secret"}`)
	if err != nil {
		t.Fatal(err)
	}
	headers, headersNonce, err := cipher.Encrypt(serverID, headersPurpose, `{"Authorization":"Bearer header-secret"}`)
	if err != nil {
		t.Fatal(err)
	}
	runtimeTool := RuntimeTool{
		ServerID: serverID, ServerName: "Runtime", ServerSlug: "runtime", EndpointURL: "http://mcp.example.com/mcp",
		ToolName: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`),
		EncryptedParameters: parameters, ParametersNonce: parametersNonce,
		EncryptedHeaders: headers, HeadersNonce: headersNonce,
	}
	repository := &runtimeRepositoryStub{tools: []RuntimeTool{runtimeTool}}
	runtime := &CompositeRuntime{Repository: repository, Cipher: cipher, Client: client}

	result, err := runtime.Execute(t.Context(), tool.ToolScope{OwnerUserID: "owner-1"}, tool.ToolCall{
		Name: RuntimeToolName(runtimeTool), CallID: "call-1", Arguments: json.RawMessage(`{"query":"hello"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed || result.OutputItem.Type != llm.ModelItemFunctionCallOutput || !strings.Contains(result.OutputItem.Output, "result:hello") {
		t.Fatalf("runtime result = %#v", result)
	}

	failed, err := runtime.Execute(t.Context(), tool.ToolScope{OwnerUserID: "owner-1"}, tool.ToolCall{
		Name: RuntimeToolName(runtimeTool), CallID: "call-2", Arguments: json.RawMessage(`{"query":"fail"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !failed.Failed || !strings.Contains(failed.OutputItem.Output, `"is_error":true`) || !strings.Contains(failed.OutputItem.Output, "not found") {
		t.Fatalf("MCP IsError result = %#v", failed)
	}
	rewriter.mu.Lock()
	defer rewriter.mu.Unlock()
	if rewriter.query != "query-secret" || rewriter.authorizer != "Bearer header-secret" {
		t.Fatalf("runtime transport query=%q authorization=%q", rewriter.query, rewriter.authorizer)
	}
}

func TestCompositeRuntimeTransportErrorRemainsAmbiguous(t *testing.T) {
	cipher, err := credential.NewCipher(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	serverID := "48eb3032-829c-4a17-8eda-58607ae59ea0"
	parameters, parametersNonce, err := cipher.Encrypt(serverID, parametersPurpose, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	headers, headersNonce, err := cipher.Encrypt(serverID, headersPurpose, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	runtimeTool := RuntimeTool{
		ServerID: serverID, ToolName: "lookup", EndpointURL: "https://mcp.example.com", InputSchema: json.RawMessage(`{}`),
		EncryptedParameters: parameters, ParametersNonce: parametersNonce,
		EncryptedHeaders: headers, HeadersNonce: headersNonce,
	}
	repository := &runtimeRepositoryStub{tools: []RuntimeTool{runtimeTool}}
	client := &SDKToolLister{httpClientFactory: func(*url.URL, map[string]string, map[string]string) *http.Client {
		return &http.Client{Transport: failingRoundTripper{}}
	}}
	runtime := &CompositeRuntime{Repository: repository, Cipher: cipher, Client: client}
	_, err = runtime.Execute(t.Context(), tool.ToolScope{OwnerUserID: "owner-1"}, tool.ToolCall{Name: RuntimeToolName(runtimeTool)})
	if err == nil || tool.IsRecoverableError(err) || errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("transport/configuration error = %v, want ordinary ambiguous error", err)
	}
}
