package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	connectionTimeout  = 15 * time.Second
	maxResponseBytes   = 4 << 20
	maxDiscoveredTools = 1000
)

var (
	errMCPConnect   = errors.New("unable to connect to MCP server")
	errMCPToolsList = errors.New("MCP server tools/list failed")
	blockedNetworks = mustNetworks(
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8", "2001:db8::/32",
	)
)

type ipResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type SDKToolLister struct {
	Resolver          ipResolver
	Dialer            *net.Dialer
	httpClientFactory func(*url.URL, map[string]string, map[string]string) *http.Client
}

var _ ToolLister = (*SDKToolLister)(nil)

func (l *SDKToolLister) ListTools(ctx context.Context, endpointURL string, parameters map[string]string, headers map[string]string) ([]domain.UserMCPTool, error) {
	session, err := l.connect(ctx, endpointURL, parameters, headers)
	if err != nil {
		return nil, err
	}
	defer closeClientSession(ctx, session)

	tools := make([]domain.UserMCPTool, 0)
	seenTools := make(map[string]struct{})
	cursor := ""
	seenCursors := map[string]struct{}{}
	for page := 0; page < 100; page++ {
		result, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, errMCPToolsList
		}
		for _, tool := range result.Tools {
			if tool == nil || strings.TrimSpace(tool.Name) == "" || len(tool.Name) > 255 || containsControl(tool.Name) || len(tool.Description) > 4000 {
				return nil, errMCPToolsList
			}
			if _, duplicate := seenTools[tool.Name]; duplicate {
				return nil, errMCPToolsList
			}
			seenTools[tool.Name] = struct{}{}
			schema, err := marshalObjectSchema(tool.InputSchema)
			if err != nil {
				return nil, errMCPToolsList
			}
			tools = append(tools, domain.UserMCPTool{
				Name: strings.TrimSpace(tool.Name), Description: tool.Description, InputSchema: schema, Enabled: true,
			})
			if len(tools) > maxDiscoveredTools {
				return nil, errMCPToolsList
			}
		}
		if result.NextCursor == "" {
			return tools, nil
		}
		if _, duplicate := seenCursors[result.NextCursor]; duplicate {
			return nil, errMCPToolsList
		}
		seenCursors[result.NextCursor] = struct{}{}
		cursor = result.NextCursor
	}
	return nil, errMCPToolsList
}

func (l *SDKToolLister) connect(ctx context.Context, endpointURL string, parameters map[string]string, headers map[string]string) (*mcpsdk.ClientSession, error) {
	validatedEndpoint, err := ValidateEndpointURL(endpointURL)
	if err != nil {
		return nil, errMCPConnect
	}
	parsed, err := url.Parse(validatedEndpoint)
	if err != nil {
		return nil, errMCPConnect
	}
	httpClient := l.newHTTPClient(parsed, parameters, headers)
	if l.httpClientFactory != nil {
		httpClient = l.httpClientFactory(parsed, parameters, headers)
	}
	transport := &mcpsdk.StreamableClientTransport{
		Endpoint:   parsed.String(),
		HTTPClient: httpClient,
		MaxRetries: -1,
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "assistant-mcp-validator", Version: "1.0.0"}, &mcpsdk.ClientOptions{
		Capabilities: &mcpsdk.ClientCapabilities{},
	})
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, errMCPConnect
	}
	return session, nil
}

func closeClientSession(ctx context.Context, session *mcpsdk.ClientSession) {
	if session == nil {
		return
	}
	if ctx.Err() != nil {
		go session.Close()
		return
	}
	_ = session.Close()
}

func (l *SDKToolLister) newHTTPClient(endpoint *url.URL, parameters map[string]string, headers map[string]string) *http.Client {
	resolver := l.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialer := l.Dialer
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            safeDialContext(resolver, dialer),
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           4,
		MaxIdleConnsPerHost:    2,
		IdleConnTimeout:        30 * time.Second,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  10 * time.Second,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: 64 << 10,
	}
	origin := endpoint.Scheme + "://" + endpoint.Host
	return &http.Client{
		Transport: &requestPolicyTransport{base: transport, parameters: parameters, headers: headers},
		Timeout:   connectionTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.User != nil || req.URL.Fragment != "" || req.URL.Scheme+"://"+req.URL.Host != origin {
				return errors.New("redirect target is not allowed")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("redirect target is not allowed")
			}
			return validateURLHost(req.URL)
		},
	}
}

type requestPolicyTransport struct {
	base       http.RoundTripper
	parameters map[string]string
	headers    map[string]string
}

func (t *requestPolicyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	query := cloned.URL.Query()
	for name, value := range t.parameters {
		query.Set(name, value)
	}
	cloned.URL.RawQuery = query.Encode()
	for name, value := range t.headers {
		cloned.Header.Set(name, value)
	}
	response, err := t.base.RoundTrip(cloned)
	if err != nil {
		return nil, err
	}
	response.Body = &limitedBody{body: response.Body, remaining: maxResponseBytes}
	return response, nil
}

type limitedBody struct {
	body      io.ReadCloser
	remaining int64
}

func (b *limitedBody) Read(buffer []byte) (int, error) {
	if b.remaining == 0 {
		var probe [1]byte
		n, err := b.body.Read(probe[:])
		if n > 0 {
			return 0, errors.New("MCP response body is too large")
		}
		return 0, err
	}
	if int64(len(buffer)) > b.remaining {
		buffer = buffer[:b.remaining]
	}
	n, err := b.body.Read(buffer)
	b.remaining -= int64(n)
	return n, err
}

func (b *limitedBody) Close() error { return b.body.Close() }

func safeDialContext(resolver ipResolver, dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network string, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid MCP network address")
		}
		if _, err := strconv.Atoi(port); err != nil {
			return nil, errors.New("invalid MCP network address")
		}
		addresses, err := resolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, errors.New("resolve MCP endpoint")
		}
		for _, address := range addresses {
			if !isPublicIP(address.IP) {
				return nil, errors.New("MCP endpoint resolved to a blocked address")
			}
		}
		var lastErr error
		for _, resolved := range addresses {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
			if err == nil {
				return connection, nil
			}
			lastErr = err
		}
		return nil, fmt.Errorf("dial MCP endpoint: %w", lastErr)
	}
}

func isPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() {
		return false
	}
	for _, network := range blockedNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

func mustNetworks(values ...string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic(err)
		}
		networks = append(networks, network)
	}
	return networks
}

func marshalObjectSchema(value any) (json.RawMessage, error) {
	if value == nil {
		return json.RawMessage(`{}`), nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, errors.New("tool input schema must be an object")
	}
	return json.RawMessage(raw), nil
}
