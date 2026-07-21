package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/llm"
)

func TestDefaultToolsWithTavilyIncludesInternetNamespace(t *testing.T) {
	tools := DefaultToolsWithTavily()

	if len(tools) != 5 {
		t.Fatalf("tool count = %d, want 5", len(tools))
	}
	web := tools[len(tools)-1]
	if web.Type != llm.ModelToolTypeNamespace || web.Name != internetNamespace {
		t.Fatalf("unexpected web namespace: %#v", web)
	}
	if len(web.Tools) != 2 {
		t.Fatalf("unexpected web tools: %#v", web.Tools)
	}
	if !strings.Contains(web.Description, "always use extract") || !strings.Contains(web.Description, "Never rely on search snippets alone") {
		t.Fatalf("internet namespace does not enforce two-stage research: %q", web.Description)
	}
	wantNames := []string{
		internetSearchName,
		internetExtractName,
	}
	for i, want := range wantNames {
		if web.Tools[i].Name != want {
			t.Fatalf("web tool %d = %q, want %q", i, web.Tools[i].Name, want)
		}
	}
}

func TestInternetToolsDescribeSearchThenExtractWorkflow(t *testing.T) {
	search := internetSearchDefinition()
	if !strings.Contains(search.Description, "First-stage") || !strings.Contains(search.Description, "always call internet.extract") || !strings.Contains(search.Description, "Do not answer") {
		t.Fatalf("search description does not define discovery workflow: %q", search.Description)
	}

	var searchSchema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(search.Parameters, &searchSchema); err != nil {
		t.Fatalf("decode search schema: %v", err)
	}
	for _, property := range []string{"include_answer", "chunks_per_source", "auto_parameters"} {
		if _, exists := searchSchema.Properties[property]; exists {
			t.Fatalf("search schema exposes %q", property)
		}
	}
	if _, exists := searchSchema.Properties["query"]; !exists {
		t.Fatal("search schema does not expose query")
	}
	var rawContent struct {
		Enum        []bool `json:"enum"`
		Default     bool   `json:"default"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(searchSchema.Properties["include_raw_content"], &rawContent); err != nil {
		t.Fatalf("decode include_raw_content schema: %v", err)
	}
	if len(rawContent.Enum) != 1 || rawContent.Enum[0] || rawContent.Default || !strings.Contains(rawContent.Description, "always be false") || !strings.Contains(rawContent.Description, "internet.extract") {
		t.Fatalf("include_raw_content is not fixed false: %#v", rawContent)
	}
	var maxResults struct {
		Type        string  `json:"type"`
		Minimum     float64 `json:"minimum"`
		Maximum     float64 `json:"maximum"`
		Description string  `json:"description"`
	}
	if err := json.Unmarshal(searchSchema.Properties["max_results"], &maxResults); err != nil {
		t.Fatalf("decode max_results schema: %v", err)
	}
	if maxResults.Type != "integer" || maxResults.Minimum != 5 || maxResults.Maximum != 20 || !strings.Contains(maxResults.Description, "default 5") {
		t.Fatalf("max_results limits are unclear: %#v", maxResults)
	}
	for _, name := range []string{"time_range", "start_date", "end_date"} {
		var property struct {
			Description string `json:"description"`
			Pattern     string `json:"pattern"`
		}
		if err := json.Unmarshal(searchSchema.Properties[name], &property); err != nil {
			t.Fatalf("decode %s schema: %v", name, err)
		}
		if name == "time_range" && (!strings.Contains(property.Description, "Mutually exclusive") || !strings.Contains(property.Description, "never both")) {
			t.Fatalf("%s does not explain date-filter exclusivity: %q", name, property.Description)
		}
		if name != "time_range" && !strings.Contains(property.Description, "omit time_range") {
			t.Fatalf("%s does not explain date-filter exclusivity: %q", name, property.Description)
		}
		if name != "time_range" && property.Pattern == "" {
			t.Fatalf("%s does not constrain its date format", name)
		}
		if name == "start_date" && !strings.Contains(property.Description, "following date") {
			t.Fatalf("start_date does not explain single-day ranges: %q", property.Description)
		}
		if name == "end_date" && (!strings.Contains(property.Description, "later than start_date") || !strings.Contains(property.Description, "must not equal")) {
			t.Fatalf("end_date does not explain ordered ranges: %q", property.Description)
		}
	}

	extract := internetExtractDefinition()
	if !strings.Contains(extract.Description, "Mandatory second-stage") || !strings.Contains(extract.Description, "internet.search") || !strings.Contains(extract.Description, "markdown or text") {
		t.Fatalf("extract description does not define follow-up workflow: %q", extract.Description)
	}
	var extractSchema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(extract.Parameters, &extractSchema); err != nil {
		t.Fatalf("decode extract schema: %v", err)
	}
	for _, property := range []string{"timeout", "chunks_per_source", "extraction_prompt", "schema"} {
		if _, exists := extractSchema.Properties[property]; exists {
			t.Fatalf("extract schema exposes %q", property)
		}
	}
	var urls struct {
		MinItems    int    `json:"minItems"`
		MaxItems    int    `json:"maxItems"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(extractSchema.Properties["urls"], &urls); err != nil {
		t.Fatalf("decode extract urls schema: %v", err)
	}
	if urls.MinItems != 1 || urls.MaxItems != 20 || !strings.Contains(urls.Description, "preceding internet.search") || !strings.Contains(urls.Description, "smallest set") {
		t.Fatalf("extract URL limits or workflow are unclear: %#v", urls)
	}
}

func TestStaticCatalogFiltersSandboxToolsByScope(t *testing.T) {
	catalog := StaticCatalog{
		Tools: []llm.ModelTool{
			sandboxNamespaceDefinition(),
		},
	}

	withoutSandbox, err := catalog.ListTools(context.Background(), ToolScope{HasSandbox: false})
	if err != nil {
		t.Fatalf("list tools without sandbox: %v", err)
	}
	if len(withoutSandbox) != 1 || withoutSandbox[0].Type != llm.ModelToolTypeNamespace || withoutSandbox[0].Name != sandboxNamespace {
		t.Fatalf("unexpected tools without sandbox: %#v", withoutSandbox)
	}
	if len(withoutSandbox[0].Tools) != 1 || withoutSandbox[0].Tools[0].Name != sandboxCreateName {
		t.Fatalf("unexpected sandbox children without sandbox: %#v", withoutSandbox[0].Tools)
	}

	withSandbox, err := catalog.ListTools(context.Background(), ToolScope{HasSandbox: true})
	if err != nil {
		t.Fatalf("list tools with sandbox: %v", err)
	}
	if len(withSandbox) != 1 || withSandbox[0].Type != llm.ModelToolTypeNamespace || withSandbox[0].Name != sandboxNamespace {
		t.Fatalf("unexpected tools with sandbox: %#v", withSandbox)
	}
	if len(withSandbox[0].Tools) != 1 || withSandbox[0].Tools[0].Name != sandboxDestroyName {
		t.Fatalf("unexpected sandbox children with sandbox: %#v", withSandbox[0].Tools)
	}

	catalog.EnableSandboxExec = true
	withExec, err := catalog.ListTools(context.Background(), ToolScope{HasSandbox: true})
	if err != nil {
		t.Fatalf("list tools with sandbox exec enabled: %v", err)
	}
	if len(withExec) != 1 || withExec[0].Type != llm.ModelToolTypeNamespace || len(withExec[0].Tools) != 3 {
		t.Fatalf("unexpected tools with sandbox exec enabled: %#v", withExec)
	}
	if withExec[0].Tools[0].Name != sandboxDestroyName || withExec[0].Tools[1].Name != sandboxExecName || withExec[0].Tools[2].Name != sandboxImportAttachmentName {
		t.Fatalf("unexpected sandbox children with exec enabled: %#v", withExec[0].Tools)
	}
}
