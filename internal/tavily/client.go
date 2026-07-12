package tavily

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func New(settings Settings) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	timeout := settings.HTTPClientTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return &Client{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(settings.APIKey),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Search(ctx context.Context, request SearchRequest) (*SearchResponse, error) {
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	payload := SearchRequest{
		Query:                    query,
		Topic:                    strings.TrimSpace(request.Topic),
		SearchDepth:              strings.TrimSpace(request.SearchDepth),
		ChunksPerSource:          request.ChunksPerSource,
		MaxResults:               request.MaxResults,
		TimeRange:                strings.TrimSpace(request.TimeRange),
		StartDate:                strings.TrimSpace(request.StartDate),
		EndDate:                  strings.TrimSpace(request.EndDate),
		Days:                     request.Days,
		IncludeAnswer:            request.IncludeAnswer,
		IncludeRawContent:        request.IncludeRawContent,
		IncludeImages:            request.IncludeImages,
		IncludeImageDescriptions: request.IncludeImageDescriptions,
		IncludeFavicon:           request.IncludeFavicon,
		IncludeUsage:             request.IncludeUsage,
		IncludeDomains:           compactStrings(request.IncludeDomains),
		ExcludeDomains:           compactStrings(request.ExcludeDomains),
		Country:                  strings.TrimSpace(request.Country),
		AutoParameters:           request.AutoParameters,
		ExactMatch:               request.ExactMatch && CanUseExactMatch(query),
	}

	body, err := c.postRaw(ctx, "search", "/search", payload)
	if err != nil {
		return nil, err
	}

	var envelope struct {
		Query        string         `json:"query"`
		Answer       string         `json:"answer"`
		Results      []SearchResult `json:"results"`
		Images       []SearchImage  `json:"images"`
		ResponseTime any            `json:"response_time"`
		RequestID    string         `json:"request_id"`
		Usage        map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode tavily search response: %w", err)
	}

	return &SearchResponse{
		Query:        envelope.Query,
		Answer:       envelope.Answer,
		Results:      envelope.Results,
		Images:       envelope.Images,
		ResponseTime: normalizeResponseTime(envelope.ResponseTime),
		RequestID:    envelope.RequestID,
		Usage:        envelope.Usage,
	}, nil
}

func CanUseExactMatch(query string) bool {
	query = strings.TrimSpace(query)
	for offset := 0; offset < len(query); {
		start := strings.IndexByte(query[offset:], '"')
		if start < 0 {
			return false
		}
		start += offset
		end := strings.IndexByte(query[start+1:], '"')
		if end < 0 {
			return false
		}
		end += start + 1
		if strings.TrimSpace(query[start+1:end]) != "" {
			return true
		}
		offset = end + 1
	}
	return false
}

func (c *Client) Extract(ctx context.Context, request ExtractRequest) (json.RawMessage, error) {
	urls := compactStrings(request.URLs)
	if len(urls) == 0 {
		return nil, fmt.Errorf("urls is required")
	}

	payload := ExtractRequest{
		URLs:             urls,
		ExtractDepth:     strings.TrimSpace(request.ExtractDepth),
		Format:           strings.TrimSpace(request.Format),
		Timeout:          request.Timeout,
		IncludeImages:    request.IncludeImages,
		IncludeFavicon:   request.IncludeFavicon,
		IncludeUsage:     request.IncludeUsage,
		Query:            strings.TrimSpace(request.Query),
		ChunksPerSource:  request.ChunksPerSource,
		ExtractionPrompt: strings.TrimSpace(request.ExtractionPrompt),
		Schema:           compactRawJSON(request.Schema),
	}
	return c.postRaw(ctx, "extract", "/extract", payload)
}

func (c *Client) postRaw(ctx context.Context, operation string, path string, payload any) (json.RawMessage, error) {
	rawRequest, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal tavily %s request: %w", operation, err)
	}
	return c.do(ctx, http.MethodPost, operation, path, rawRequest, nil)
}

func (c *Client) do(ctx context.Context, method string, operation string, path string, body []byte, headers map[string]string) (json.RawMessage, error) {
	if c == nil {
		return nil, fmt.Errorf("tavily client is nil")
	}
	if c.apiKey == "" {
		return nil, fmt.Errorf("tavily api key is required")
	}

	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("create tavily %s request: %w", operation, err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpRequest.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		httpRequest.Header.Set(key, value)
	}

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("send tavily %s request: %w", operation, err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read tavily %s response: %w", operation, err)
	}
	responseBody = bytes.TrimSpace(responseBody)

	if response.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("tavily %s failed: status=%d body=%s", operation, response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if len(responseBody) == 0 {
		responseBody = []byte(`{}`)
	}
	return append(json.RawMessage(nil), responseBody...), nil
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	compacted := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		compacted = append(compacted, value)
	}
	if len(compacted) == 0 {
		return nil
	}
	return compacted
}

func compactRawJSON(value json.RawMessage) json.RawMessage {
	value = bytes.TrimSpace(value)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func normalizeResponseTime(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}
