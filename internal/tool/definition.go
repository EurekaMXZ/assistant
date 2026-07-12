package tool

import (
	"encoding/json"

	"github.com/EurekaMXZ/assistant/internal/llm"
)

const (
	conversationNamespace = "conversation"
	sandboxNamespace      = "sandbox"
	internetNamespace     = "internet"

	conversationRenameTitleName = "rename_title"
	sandboxCreateName           = "create"
	sandboxDestroyName          = "destroy"
	sandboxExecName             = "exec"
	internetSearchName          = "search"
	internetExtractName         = "extract"
)

const (
	ConversationRenameTitle = conversationNamespace + "." + conversationRenameTitleName
	SandboxCreate           = sandboxNamespace + "." + sandboxCreateName
	SandboxDestroy          = sandboxNamespace + "." + sandboxDestroyName
	SandboxExec             = sandboxNamespace + "." + sandboxExecName
	WebSearch               = internetNamespace + "." + internetSearchName
	WebExtract              = internetNamespace + "." + internetExtractName
)

func DefaultTools() []llm.ModelTool {
	return []llm.ModelTool{
		conversationNamespaceDefinition(),
		sandboxNamespaceDefinition(),
		imageGenerationDefinition(),
	}
}

func DefaultToolsWithTavily() []llm.ModelTool {
	tools := DefaultTools()
	return append(tools, internetNamespaceDefinition())
}

func conversationNamespaceDefinition() llm.ModelTool {
	return namespaceDefinition(
		conversationNamespace,
		"Tools for managing the current conversation.",
		conversationRenameTitleDefinition(),
	)
}

func sandboxNamespaceDefinition() llm.ModelTool {
	return namespaceDefinition(
		sandboxNamespace,
		"Tools for managing the current conversation sandbox.",
		sandboxCreateDefinition(),
		sandboxDestroyDefinition(),
		sandboxExecDefinition(),
	)
}

func internetNamespaceDefinition() llm.ModelTool {
	return namespaceDefinition(
		internetNamespace,
		"Tools for public web research. Use search to discover candidate URLs, then use extract to read the smallest relevant set of search results before relying on their page content.",
		internetSearchDefinition(),
		internetExtractDefinition(),
	)
}

func imageGenerationDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:         llm.ModelToolTypeImageGeneration,
		Size:         "auto",
		Quality:      "auto",
		OutputFormat: "png",
		Background:   "auto",
		Moderation:   "auto",
	}
}

func conversationRenameTitleDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeFunction,
		Name:        conversationRenameTitleName,
		Description: "Rename the current conversation when the user asks to rename it or when a clearer title should replace a vague one.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"title":{
					"type":"string",
					"description":"The new human-readable title for the current conversation."
				}
			},
			"required":["title"],
			"additionalProperties":false
		}`),
		Strict: true,
	}
}

func sandboxCreateDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeFunction,
		Name:        sandboxCreateName,
		Description: "Create a sandbox for the current conversation when the user needs an isolated execution environment.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{},
			"additionalProperties":false
		}`),
		Strict: true,
	}
}

func sandboxDestroyDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeFunction,
		Name:        sandboxDestroyName,
		Description: "Destroy the active sandbox for the current conversation when it is no longer needed.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{},
			"additionalProperties":false
		}`),
		Strict: true,
	}
}

func sandboxExecDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeFunction,
		Name:        sandboxExecName,
		Description: "Execute one command inside the active sandbox for the current conversation.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"command":{
					"type":"string",
					"description":"The executable to run inside the sandbox."
				},
				"args":{
					"type":"array",
					"items":{"type":"string"},
					"description":"Optional command-line arguments."
				},
				"working_directory":{
					"type":"string",
					"description":"Optional relative working directory inside the sandbox."
				},
				"timeout_seconds":{
					"type":"integer",
					"description":"Optional timeout for the command."
				}
			},
			"required":["command"],
			"additionalProperties":false
		}`),
	}
}

func internetSearchDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeFunction,
		Name:        internetSearchName,
		Description: "Discover relevant public web sources. This returns candidate URLs and search snippets, not full page content. After choosing the most relevant results, use internet.extract to read those URLs before relying on their contents. Refine the query instead of requesting page bodies through search.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"query":{
					"type":"string",
					"description":"The search query to run."
				},
				"topic":{
					"type":"string",
					"enum":["general","news"],
					"description":"Search topic. Use news for recent news-specific queries; otherwise use general."
				},
				"search_depth":{
					"type":"string",
					"enum":["basic","advanced","fast","ultra-fast"],
					"description":"How thorough or fast the search should be. Use advanced for stronger retrieval and fast/ultra-fast when latency matters."
				},
				"chunks_per_source":{
					"type":"integer",
					"minimum":1,
					"maximum":3,
					"description":"Number of content chunks per source when supported by the selected search depth."
				},
				"max_results":{
					"type":"integer",
					"minimum":1,
					"maximum":20,
					"description":"The maximum number of search results to return."
				},
				"time_range":{
					"type":"string",
					"enum":["day","week","month","year","d","w","m","y"],
					"description":"Optional recency window for the search."
				},
				"start_date":{
					"type":"string",
					"description":"Optional inclusive start date in YYYY-MM-DD format."
				},
				"end_date":{
					"type":"string",
					"description":"Optional inclusive end date in YYYY-MM-DD format."
				},
				"days":{
					"type":"integer",
					"minimum":1,
					"description":"Optional number of days back to include for news searches."
				},
				"include_images":{
					"type":"boolean",
					"description":"Include query-related images."
				},
				"include_image_descriptions":{
					"type":"boolean",
					"description":"Include descriptions for returned images."
				},
				"include_favicon":{
					"type":"boolean",
					"description":"Include favicon URLs for result domains."
				},
				"include_usage":{
					"type":"boolean",
					"description":"Include Tavily credit usage metadata when needed."
				},
				"include_domains":{
					"type":"array",
					"description":"Optional domains to include in the search.",
					"items":{"type":"string"}
				},
				"exclude_domains":{
					"type":"array",
					"description":"Optional domains to exclude from the search.",
					"items":{"type":"string"}
				},
				"country":{
					"type":"string",
					"description":"Optional lowercase English country name used to bias general web results, for example 'united states'."
				},
				"auto_parameters":{
					"type":"boolean",
					"description":"Let Tavily automatically tune search parameters when appropriate."
				},
				"exact_match":{
					"type":"boolean",
					"description":"Require results to contain a phrase verbatim. Set this only when query contains a non-empty ASCII double-quoted phrase, for example '\"John Smith\" CEO'; otherwise omit it or set false."
				}
			},
			"required":["query"],
			"additionalProperties":false
		}`),
	}
}

func internetExtractDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeFunction,
		Name:        internetExtractName,
		Description: "Read content from URLs selected from internet.search results. Use this after search when grounding an answer in source content, normally with only the 1-3 most relevant URLs and a focused query so Tavily returns relevant chunks.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"urls":{
					"type":"array",
					"description":"The 1-3 most relevant URLs selected from internet.search results.",
					"items":{"type":"string"},
					"minItems":1,
					"maxItems":3
				},
				"extract_depth":{
					"type":"string",
					"enum":["basic","advanced"],
					"description":"Extraction depth. Use advanced for JavaScript-heavy pages, tables, or embedded content."
				},
				"format":{
					"type":"string",
					"enum":["markdown","text"],
					"description":"Content format to return."
				},
				"timeout":{
					"type":"number",
					"minimum":1,
					"maximum":60,
					"description":"Per-request extraction timeout in seconds."
				},
				"include_images":{
					"type":"boolean",
					"description":"Include image URLs extracted from the page."
				},
				"include_favicon":{
					"type":"boolean",
					"description":"Include favicon URLs."
				},
				"include_usage":{
					"type":"boolean",
					"description":"Include Tavily credit usage metadata when needed."
				},
				"query":{
					"type":"string",
					"description":"Focused user intent used to rerank extracted chunks and avoid returning unrelated page content."
				},
				"chunks_per_source":{
					"type":"integer",
					"minimum":1,
					"maximum":5,
					"description":"Number of chunks per source when query reranking is used."
				},
				"extraction_prompt":{
					"type":"string",
					"description":"Optional natural-language extraction instruction for structured extraction."
				},
				"schema":{
					"type":"object",
					"description":"Optional JSON schema for structured extraction."
				}
			},
			"required":["urls"],
			"additionalProperties":false
		}`),
	}
}

func namespaceDefinition(name string, description string, tools ...llm.ModelTool) llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeNamespace,
		Name:        name,
		Description: description,
		Tools:       tools,
	}
}
