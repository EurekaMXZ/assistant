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

	if len(tools) != 4 {
		t.Fatalf("tool count = %d, want 4", len(tools))
	}
	web := tools[3]
	if web.Type != llm.ModelToolTypeNamespace || web.Name != internetNamespace {
		t.Fatalf("unexpected web namespace: %#v", web)
	}
	if len(web.Tools) != 2 {
		t.Fatalf("unexpected web tools: %#v", web.Tools)
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
	if !strings.Contains(search.Description, "source URLs") || !strings.Contains(search.Description, "internet.extract") {
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

	extract := internetExtractDefinition()
	if !strings.Contains(extract.Description, "internet.search") || !strings.Contains(extract.Description, "markdown or text") {
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
