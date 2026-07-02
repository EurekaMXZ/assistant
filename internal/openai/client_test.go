package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/credential"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

type stubCredentialResolver struct {
	credentialID string
	resolved     *credential.Resolved
	err          error
}

func (s *stubCredentialResolver) ResolveCredential(_ context.Context, credentialID string) (*credential.Resolved, error) {
	s.credentialID = credentialID
	return s.resolved, s.err
}

func newTestClient(baseURL string, settings Settings) *Client {
	client := New(settings)
	client.SetCredentialResolver(&stubCredentialResolver{resolved: &credential.Resolved{
		Provider: "openai", BaseURL: baseURL, APIKey: "test-secret",
	}})
	return client
}

func TestNewUsesSettings(t *testing.T) {
	client := New(Settings{
		UserAgent:         " assistant-test/1.0 ",
		HTTPClientTimeout: 42 * time.Second,
	})

	if client.userAgent != "assistant-test/1.0" {
		t.Fatalf("userAgent = %q, want %q", client.userAgent, "assistant-test/1.0")
	}
	if client.httpClient.Timeout != 0 {
		t.Fatalf("streaming client timeout = %v, want no absolute timeout", client.httpClient.Timeout)
	}
	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.httpClient.Transport)
	}
	if transport.ResponseHeaderTimeout != 42*time.Second {
		t.Fatalf("response header timeout = %v, want %v", transport.ResponseHeaderTimeout, 42*time.Second)
	}
}

func TestStreamResponseClassifiesAndSanitizesProviderFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_1\",\"status\":\"failed\",\"error\":{\"message\":\"sensitive provider detail\"}}}\n\n"))
	}))
	defer server.Close()

	client := newTestClient(server.URL, Settings{HTTPClientTimeout: 5 * time.Second})
	var events []llm.ModelEvent
	_, err := client.StreamResponse(t.Context(), llm.ModelRequest{Model: "gpt-test", CredentialID: "credential-1"}, func(event llm.ModelEvent) error {
		events = append(events, event)
		return nil
	})
	if !errors.Is(err, llm.ErrUpstreamRequestFailed) {
		t.Fatalf("error = %v, want upstream request failure", err)
	}
	if !strings.Contains(err.Error(), "sensitive provider detail") {
		t.Fatalf("internal error should retain provider detail, got %v", err)
	}
	if len(events) != 1 || events[0].Error != domain.TurnPublicErrorUpstreamRequestFailed {
		t.Fatalf("unexpected public events: %#v", events)
	}
}

func TestStreamResponseSendsConfiguredUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "assistant-test/1.0" {
			t.Errorf("User-Agent = %q, want %q", got, "assistant-test/1.0")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	client := newTestClient(server.URL, Settings{
		UserAgent:         "assistant-test/1.0",
		HTTPClientTimeout: 5 * time.Second,
	})
	if _, err := client.StreamResponse(t.Context(), llm.ModelRequest{Model: "gpt-test", CredentialID: "credential-1"}, nil); err != nil {
		t.Fatalf("stream response: %v", err)
	}
}

func TestStreamResponseUsesResolvedCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer dynamic-secret" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	resolver := &stubCredentialResolver{resolved: &credential.Resolved{Provider: "openai", BaseURL: server.URL, APIKey: "dynamic-secret"}}
	client := New(Settings{HTTPClientTimeout: 5 * time.Second})
	client.SetCredentialResolver(resolver)

	if _, err := client.StreamResponse(t.Context(), llm.ModelRequest{Model: "gpt-test", CredentialID: "credential-1"}, nil); err != nil {
		t.Fatalf("stream response: %v", err)
	}
	if resolver.credentialID != "credential-1" {
		t.Fatalf("resolved credential id = %q", resolver.credentialID)
	}
}

func TestStreamResponseDoesNotLeakCredentialResolutionError(t *testing.T) {
	resolver := &stubCredentialResolver{err: errors.New("secret provider detail")}
	client := New(Settings{})
	client.SetCredentialResolver(resolver)

	_, err := client.StreamResponse(t.Context(), llm.ModelRequest{Model: "gpt-test", CredentialID: "credential-1"}, nil)
	if !errors.Is(err, llm.ErrUpstreamRequestFailed) {
		t.Fatalf("error = %v, want upstream request failure", err)
	}
}

func TestStreamResponseHasNoAbsoluteDurationLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(60 * time.Millisecond)
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	client := newTestClient(server.URL, Settings{HTTPClientTimeout: 20 * time.Millisecond})
	if _, err := client.StreamResponse(t.Context(), llm.ModelRequest{Model: "gpt-test", CredentialID: "credential-1"}, nil); err != nil {
		t.Fatalf("stream exceeded response-header timeout after headers arrived: %v", err)
	}
}

func TestMarshalRequestBuildsResponsesPayload(t *testing.T) {
	client := New(Settings{HTTPClientTimeout: 5 * time.Second})
	parallelToolCalls := false

	raw, err := client.MarshalRequest(llm.ModelRequest{
		Model:        "gpt-test",
		Instructions: " system ",
		Input:        []llm.ModelItem{{Type: llm.ModelItemMessage, Role: "user", Content: "hello"}},
		Tools: []llm.ModelTool{
			{
				Type:        llm.ModelToolTypeNamespace,
				Name:        "conversation",
				Description: "Conversation tools.",
				Tools: []llm.ModelTool{
					{
						Type:        llm.ModelToolTypeFunction,
						Name:        "rename_title",
						Description: "Rename the current conversation.",
						Parameters:  json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}`),
						Strict:      true,
					},
				},
			},
		},
		MaxOutputTokens: 321,
		PromptCacheKey:  " assistant-conversation-abc123 ",
		ToolChoice:      " none ",
		Metadata: map[string]string{
			"conversation_id": "conv-1",
		},
		ParallelToolCalls: &parallelToolCalls,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var payload struct {
		Model        string `json:"model"`
		Instructions string `json:"instructions"`
		Reasoning    struct {
			Effort  string `json:"effort"`
			Summary string `json:"summary"`
		} `json:"reasoning"`
		Stream            bool              `json:"stream"`
		Store             bool              `json:"store"`
		Include           []string          `json:"include"`
		MaxOutputTokens   int               `json:"max_output_tokens"`
		PromptCacheKey    string            `json:"prompt_cache_key"`
		ToolChoice        string            `json:"tool_choice"`
		Metadata          map[string]string `json:"metadata"`
		ParallelToolCalls bool              `json:"parallel_tool_calls"`
		Input             []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"input"`
		Tools []struct {
			Type        string          `json:"type"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
			Strict      bool            `json:"strict"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if payload.Model != "gpt-test" {
		t.Fatalf("model = %q, want %q", payload.Model, "gpt-test")
	}
	if payload.Instructions != "system" {
		t.Fatalf("instructions = %q, want %q", payload.Instructions, "system")
	}
	if payload.Reasoning.Effort != "xhigh" || payload.Reasoning.Summary != "detailed" {
		t.Fatalf("unexpected reasoning payload: %#v", payload.Reasoning)
	}
	if !payload.Stream || payload.Store {
		t.Fatalf("unexpected stream/store flags: stream=%v store=%v", payload.Stream, payload.Store)
	}
	if len(payload.Include) != 1 || payload.Include[0] != llm.ModelIncludeReasoningEncryptedContent {
		t.Fatalf("unexpected include payload: %#v", payload.Include)
	}
	if payload.MaxOutputTokens != 321 {
		t.Fatalf("max_output_tokens = %d, want 321", payload.MaxOutputTokens)
	}
	if payload.PromptCacheKey != "assistant-conversation-abc123" {
		t.Fatalf("prompt_cache_key = %q", payload.PromptCacheKey)
	}
	if payload.ToolChoice != "none" {
		t.Fatalf("tool_choice = %q, want none", payload.ToolChoice)
	}
	if payload.Metadata["conversation_id"] != "conv-1" {
		t.Fatalf("metadata = %#v, want conversation_id", payload.Metadata)
	}
	if len(payload.Tools) != 1 || payload.Tools[0].Type != llm.ModelToolTypeFunction || payload.Tools[0].Name != "conversation_rename_title" {
		t.Fatalf("unexpected tool payload: %#v", payload.Tools)
	}
	if !json.Valid(payload.Tools[0].Parameters) {
		t.Fatalf("tool parameters are not valid JSON: %s", payload.Tools[0].Parameters)
	}
	if !payload.Tools[0].Strict {
		t.Fatal("expected strict tool payload")
	}
	if payload.ParallelToolCalls {
		t.Fatal("expected parallel_tool_calls=false")
	}
	if len(payload.Input) != 1 || payload.Input[0].Type != "message" || payload.Input[0].Role != "user" || payload.Input[0].Content != "hello" {
		t.Fatalf("unexpected input payload: %#v", payload.Input)
	}
}

func TestMarshalRequestOmitsEmptyPromptCacheKey(t *testing.T) {
	client := New(Settings{})
	raw, err := client.MarshalRequest(llm.ModelRequest{
		Model: "gpt-test",
		Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if strings.Contains(string(raw), "prompt_cache_key") {
		t.Fatalf("empty prompt cache key was not omitted: %s", raw)
	}
}

func TestParseOutputItemsSupportsObjectArgumentsAndOutput(t *testing.T) {
	items := parseOutputItems([]json.RawMessage{
		json.RawMessage(`{"type":"mcp_call","server_label":"tavily","name":"search","call_id":"mcp_1","arguments":{"query":"latest openai news"},"output":{"results":[{"title":"OpenAI"}]}}`),
	})

	if len(items) != 1 {
		t.Fatalf("expected one parsed item, got %#v", items)
	}
	if items[0].Type != llm.ModelItemMCPCall || items[0].ServerLabel != "tavily" || items[0].CallID != "mcp_1" {
		t.Fatalf("unexpected parsed item: %#v", items[0])
	}
	if string(items[0].Arguments) != `{"query":"latest openai news"}` {
		t.Fatalf("unexpected parsed arguments: %s", items[0].Arguments)
	}
	if items[0].Output != `{"results":[{"title":"OpenAI"}]}` {
		t.Fatalf("unexpected parsed output: %s", items[0].Output)
	}
}

func TestMarshalRequestIncludesImageGenerationTool(t *testing.T) {
	client := New(Settings{HTTPClientTimeout: 5 * time.Second})
	compression := 80

	raw, err := client.MarshalRequest(llm.ModelRequest{
		Model: "gpt-test",
		Input: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: "user", Content: "draw a red circle"}},
		Tools: []llm.ModelTool{
			{
				Type:              llm.ModelToolTypeImageGeneration,
				Size:              "1024x1024",
				Quality:           "low",
				OutputFormat:      "png",
				OutputCompression: &compression,
				Background:        "opaque",
				Moderation:        "auto",
				PartialImages:     2,
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var payload struct {
		Tools []struct {
			Type              string `json:"type"`
			Size              string `json:"size"`
			Quality           string `json:"quality"`
			OutputFormat      string `json:"output_format"`
			OutputCompression int    `json:"output_compression"`
			Background        string `json:"background"`
			Moderation        string `json:"moderation"`
			PartialImages     int    `json:"partial_images"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Tools) != 1 || payload.Tools[0].Type != llm.ModelToolTypeImageGeneration {
		t.Fatalf("unexpected image tool payload: %#v", payload.Tools)
	}
	tool := payload.Tools[0]
	if tool.Size != "1024x1024" || tool.Quality != "low" || tool.OutputFormat != "png" || tool.OutputCompression != 80 || tool.Background != "opaque" || tool.Moderation != "auto" || tool.PartialImages != 2 {
		t.Fatalf("unexpected image tool options: %#v", tool)
	}
}

func TestParseOutputItemsSupportsImageGenerationCalls(t *testing.T) {
	items := parseOutputItems([]json.RawMessage{
		json.RawMessage(`{"id":"ig_1","type":"image_generation_call","status":"generating","revised_prompt":"A red circle.","result":"aW1hZ2U="}`),
	})

	if len(items) != 1 {
		t.Fatalf("expected one parsed item, got %#v", items)
	}
	if items[0].Type != llm.ModelItemImageGenerationCall || items[0].ID != "ig_1" || items[0].Status != "generating" {
		t.Fatalf("unexpected parsed image item: %#v", items[0])
	}
	if items[0].RevisedPrompt != "A red circle." || items[0].Result != "aW1hZ2U=" {
		t.Fatalf("unexpected image payload fields: %#v", items[0])
	}
}

func TestParseOutputItemsExtractsStructuredErrorMessage(t *testing.T) {
	items := parseOutputItems([]json.RawMessage{
		json.RawMessage(`{"type":"mcp_call","server_label":"tavily","name":"search","call_id":"mcp_2","error":{"message":"upstream failed"}}`),
	})

	if len(items) != 1 {
		t.Fatalf("expected one parsed item, got %#v", items)
	}
	if items[0].Error != "upstream failed" {
		t.Fatalf("unexpected parsed error: %#v", items[0].Error)
	}
}

func TestParseModelUsageIncludesCacheAndReasoningBreakdown(t *testing.T) {
	usage, err := parseModelUsage(json.RawMessage(`{
		"input_tokens": 1200,
		"input_tokens_details": {"cached_tokens": 300, "cache_creation_tokens": 100},
		"output_tokens": 500,
		"output_tokens_details": {"reasoning_tokens": 200},
		"total_tokens": 1700
	}`))
	if err != nil {
		t.Fatalf("parse model usage: %v", err)
	}
	if usage.InputTokens != 1200 || usage.CacheReadInputTokens != 300 || usage.CacheCreationInputTokens != 100 || usage.OutputTokens != 500 || usage.ReasoningOutputTokens != 200 || usage.TotalTokens != 1700 {
		t.Fatalf("unexpected usage: %#v", usage)
	}
	if !json.Valid(usage.Raw) {
		t.Fatalf("expected raw usage json, got %s", usage.Raw)
	}
}

func TestStreamResponseKeepsMultipleOutputTextItemsSeparate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			`event: response.created
data: {"type":"response.created","response":{"id":"resp_1"}}

`,
			`event: response.output_text.delta
data: {"type":"response.output_text.delta","response_id":"resp_1","item_id":"msg_1","output_index":0,"content_index":0,"delta":"Preamble"}

`,
			`event: response.output_text.done
data: {"type":"response.output_text.done","response_id":"resp_1","item_id":"msg_1","output_index":0,"content_index":0,"text":"Preamble"}

`,
			`event: response.output_text.delta
data: {"type":"response.output_text.delta","response_id":"resp_1","item_id":"msg_2","output_index":2,"content_index":0,"delta":"Final"}

`,
			`event: response.output_text.done
data: {"type":"response.output_text.done","response_id":"resp_1","item_id":"msg_2","output_index":2,"content_index":0,"text":"Final"}

`,
			`event: response.completed
data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3},"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Preamble"}]},{"id":"rs_1","type":"reasoning","summary":[]},{"id":"msg_2","type":"message","status":"completed","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Final"}]}]}}

`,
		}
		for _, frame := range frames {
			_, _ = w.Write([]byte(frame))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL, Settings{HTTPClientTimeout: 5 * time.Second})
	var events []llm.ModelEvent
	result, err := client.StreamResponse(t.Context(), llm.ModelRequest{Model: "gpt-test", CredentialID: "credential-1"}, func(event llm.ModelEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("stream response: %v", err)
	}

	if result.FinalText != "PreambleFinal" {
		t.Fatalf("FinalText = %q, want concatenated text", result.FinalText)
	}
	if len(result.TextItems) != 2 || result.TextItems[0].ItemID != "msg_1" || result.TextItems[1].ItemID != "msg_2" {
		t.Fatalf("unexpected text items: %#v", result.TextItems)
	}
	if result.OutputItems[0].Phase != "commentary" || result.OutputItems[2].Phase != "final_answer" {
		t.Fatalf("message phases were not preserved: %#v", result.OutputItems)
	}
	if len(events) != 6 {
		t.Fatalf("expected 6 passthrough events, got %#v", events)
	}
	if events[1].Type != "response.output_text.delta" || events[1].ItemID != "msg_1" || events[1].Delta != "Preamble" {
		t.Fatalf("unexpected first text delta event: %#v", events[1])
	}
	if events[4].Type != "response.output_text.done" || events[4].ItemID != "msg_2" || events[4].Text != "Final" {
		t.Fatalf("unexpected final text done event: %#v", events[4])
	}
	if !strings.Contains(string(events[4].Raw), `"item_id":"msg_2"`) {
		t.Fatalf("expected raw event payload, got %s", events[4].Raw)
	}
}

func TestStreamResponseReadsLargeSSEDataLine(t *testing.T) {
	largeImageResult := strings.Repeat("a", 3*1024*1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`event: response.completed
data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3},"output":[{"id":"ig_1","type":"image_generation_call","status":"completed","result":"` + largeImageResult + `"}]}}

`))
	}))
	defer server.Close()

	client := newTestClient(server.URL, Settings{HTTPClientTimeout: 5 * time.Second})
	result, err := client.StreamResponse(t.Context(), llm.ModelRequest{Model: "gpt-test", CredentialID: "credential-1"}, nil)
	if err != nil {
		t.Fatalf("stream response: %v", err)
	}
	if len(result.OutputItems) != 1 || result.OutputItems[0].Type != llm.ModelItemImageGenerationCall {
		t.Fatalf("unexpected output items: %#v", result.OutputItems)
	}
	if got := len(result.OutputItems[0].Result); got != len(largeImageResult) {
		t.Fatalf("image result length = %d, want %d", got, len(largeImageResult))
	}
}
