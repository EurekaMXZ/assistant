package mcpconfig

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type staticResolver struct {
	addresses []net.IPAddr
}

func (r staticResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return r.addresses, nil
}

func TestSafeDialContextRejectsPrivateResolution(t *testing.T) {
	dial := safeDialContext(staticResolver{addresses: []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}}, &net.Dialer{})
	if _, err := dial(t.Context(), "tcp", "mcp.example.com:443"); err == nil {
		t.Fatal("safeDialContext accepted private DNS result")
	}

	dial = safeDialContext(staticResolver{addresses: []net.IPAddr{
		{IP: net.ParseIP("8.8.8.8")},
		{IP: net.ParseIP("127.0.0.1")},
	}}, &net.Dialer{})
	if _, err := dial(t.Context(), "tcp", "mcp.example.com:443"); err == nil {
		t.Fatal("safeDialContext accepted a mixed public/private DNS result")
	}
}

type rewriteTransport struct {
	base       http.RoundTripper
	target     *url.URL
	mu         sync.Mutex
	query      string
	authorizer string
}

func (t *rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	t.mu.Lock()
	t.query = cloned.URL.Query().Get("api_key")
	t.authorizer = cloned.Header.Get("Authorization")
	t.mu.Unlock()
	cloned.URL.Scheme = t.target.Scheme
	cloned.URL.Host = t.target.Host
	cloned.Host = t.target.Host
	return t.base.RoundTrip(cloned)
}

func TestSDKToolListerUsesStreamableHTTPToolsList(t *testing.T) {
	type toolInput struct {
		Query string `json:"query"`
	}
	mcpServer := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{Name: "search", Description: "Search records"},
		func(context.Context, *mcpsdk.CallToolRequest, toolInput) (*mcpsdk.CallToolResult, any, error) {
			return &mcpsdk.CallToolResult{}, nil, nil
		})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return mcpServer }, nil)
	testServer := httptest.NewServer(handler)
	defer testServer.Close()
	target, err := url.Parse(testServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	rewriter := &rewriteTransport{base: http.DefaultTransport, target: target}
	lister := &SDKToolLister{
		httpClientFactory: func(endpoint *url.URL, parameters map[string]string, headers map[string]string) *http.Client {
			if endpoint.RawQuery != "" {
				t.Errorf("SDK endpoint leaked query parameters: %q", endpoint.RawQuery)
			}
			return &http.Client{Transport: &requestPolicyTransport{base: rewriter, parameters: parameters, headers: headers}}
		},
	}

	tools, err := lister.ListTools(t.Context(), "http://mcp.example.com/mcp", map[string]string{"api_key": "query-secret"}, map[string]string{"Authorization": "Bearer header-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "search" || string(tools[0].InputSchema) == "" {
		t.Fatalf("tools = %#v", tools)
	}
	rewriter.mu.Lock()
	defer rewriter.mu.Unlock()
	if rewriter.query != "query-secret" || rewriter.authorizer != "Bearer header-secret" {
		t.Fatalf("transport config query=%q authorization=%q", rewriter.query, rewriter.authorizer)
	}
}
