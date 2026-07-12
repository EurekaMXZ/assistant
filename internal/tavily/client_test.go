package tavily

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestClientSearch(t *testing.T) {
	var (
		authorization string
		requestBody   SearchRequest
	)
	client := New(Settings{
		BaseURL:           "https://search.example.test",
		APIKey:            "test-key",
		HTTPClientTimeout: time.Second,
	})
	client.httpClient = &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/search" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			authorization = r.Header.Get("Authorization")
			if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
				t.Fatalf("decode request body: %v", err)
			}

			body, err := json.Marshal(map[string]any{
				"query":         requestBody.Query,
				"answer":        "Current answer",
				"response_time": 1.23,
				"request_id":    "req-1",
				"results": []map[string]any{
					{
						"title":   "OpenAI",
						"url":     "https://openai.com/",
						"content": "Latest docs",
						"score":   0.9,
					},
				},
			})
			if err != nil {
				t.Fatalf("marshal response body: %v", err)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(string(body))),
			}, nil
		}),
	}

	response, err := client.Search(context.Background(), SearchRequest{
		Query:          " latest openai docs ",
		SearchDepth:    "basic",
		MaxResults:     3,
		ExactMatch:     true,
		IncludeDomains: []string{" developers.openai.com ", ""},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if authorization != "Bearer test-key" {
		t.Fatalf("authorization = %q, want %q", authorization, "Bearer test-key")
	}
	if requestBody.Query != "latest openai docs" {
		t.Fatalf("query = %q", requestBody.Query)
	}
	if len(requestBody.IncludeDomains) != 1 || requestBody.IncludeDomains[0] != "developers.openai.com" {
		t.Fatalf("unexpected include domains: %#v", requestBody.IncludeDomains)
	}
	if requestBody.ExactMatch {
		t.Fatalf("unquoted query retained exact_match: %#v", requestBody)
	}
	if response.Query != "latest openai docs" || response.Answer != "Current answer" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if response.ResponseTime != "1.23" {
		t.Fatalf("response time = %q, want %q", response.ResponseTime, "1.23")
	}
	if len(response.Results) != 1 || response.Results[0].Title != "OpenAI" {
		t.Fatalf("unexpected results: %#v", response.Results)
	}
}

func TestClientSearchPreservesExactMatchForQuotedPhrase(t *testing.T) {
	var requestBody SearchRequest
	client := New(Settings{BaseURL: "https://search.example.test", APIKey: "test-key"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"query":"quoted","results":[]}`)),
		}, nil
	})}

	if _, err := client.Search(t.Context(), SearchRequest{Query: `"John Smith" CEO`, ExactMatch: true}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if !requestBody.ExactMatch {
		t.Fatalf("quoted query lost exact_match: %#v", requestBody)
	}
}

func TestCanUseExactMatchRequiresNonEmptyQuotedPhrase(t *testing.T) {
	for _, test := range []struct {
		query string
		want  bool
	}{
		{query: "John Smith CEO", want: false},
		{query: `"John Smith" CEO`, want: true},
		{query: `"" CEO`, want: false},
		{query: `"   " CEO`, want: false},
		{query: `unclosed "John Smith`, want: false},
	} {
		if got := CanUseExactMatch(test.query); got != test.want {
			t.Fatalf("CanUseExactMatch(%q) = %t, want %t", test.query, got, test.want)
		}
	}
}

func TestClientSearchReturnsHTTPError(t *testing.T) {
	client := New(Settings{
		BaseURL: "https://search.example.test",
		APIKey:  "test-key",
	})
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(strings.NewReader(`{"detail":"bad request"}`)),
			}, nil
		}),
	}

	_, err := client.Search(context.Background(), SearchRequest{Query: "latest openai docs"})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got == "" || got == "bad request" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientTavilyEndpoints(t *testing.T) {
	type observedRequest struct {
		method        string
		path          string
		authorization string
		body          map[string]any
	}

	var requests []observedRequest
	client := New(Settings{
		BaseURL: "https://api.example.test",
		APIKey:  "test-key",
	})
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			observed := observedRequest{
				method:        r.Method,
				path:          r.URL.EscapedPath(),
				authorization: r.Header.Get("Authorization"),
			}
			if r.Body != nil {
				_ = json.NewDecoder(r.Body).Decode(&observed.body)
			}
			requests = append(requests, observed)

			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		}),
	}

	calls := []struct {
		name string
		call func() (json.RawMessage, error)
		path string
	}{
		{
			name: "extract",
			call: func() (json.RawMessage, error) {
				return client.Extract(context.Background(), ExtractRequest{URLs: []string{" https://example.com "}, ExtractDepth: "advanced"})
			},
			path: "/extract",
		},
	}

	for _, test := range calls {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.call()
			if err != nil {
				t.Fatalf("%s: %v", test.name, err)
			}
			if string(result) != `{"ok":true}` {
				t.Fatalf("unexpected raw result: %s", result)
			}
		})
	}

	if len(requests) != len(calls) {
		t.Fatalf("request count = %d, want %d", len(requests), len(calls))
	}
	for i, request := range requests {
		if request.path != calls[i].path {
			t.Fatalf("request %d path = %q, want %q", i, request.path, calls[i].path)
		}
		if request.authorization != "Bearer test-key" {
			t.Fatalf("request %d authorization = %q", i, request.authorization)
		}
	}
	if requests[0].method != http.MethodPost || requests[0].body["urls"] == nil {
		t.Fatalf("unexpected extract request: %#v", requests[0])
	}
	if requests[0].method != http.MethodPost {
		t.Fatalf("unexpected extract method: %#v", requests[0])
	}
}
