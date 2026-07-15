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
	sandboxImportAttachmentName = "import_attachment"
	internetSearchName          = "search"
	internetExtractName         = "extract"
)

const (
	ConversationRenameTitle = conversationNamespace + "." + conversationRenameTitleName
	SandboxCreate           = sandboxNamespace + "." + sandboxCreateName
	SandboxDestroy          = sandboxNamespace + "." + sandboxDestroyName
	SandboxExec             = sandboxNamespace + "." + sandboxExecName
	SandboxImportAttachment = sandboxNamespace + "." + sandboxImportAttachmentName
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
		sandboxImportAttachmentDefinition(),
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

func sandboxImportAttachmentDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeFunction,
		Name:        sandboxImportAttachmentName,
		Description: "Import one user attachment into the active sandbox on demand. Use the attachment ID shown in the user message; the tool returns the sandbox path.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"attachment_id":{
					"type":"string",
					"description":"The UUID of an attachment belonging to the current conversation."
				}
			},
			"required":["attachment_id"],
			"additionalProperties":false
		}`),
		Strict: true,
	}
}

func internetSearchDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeFunction,
		Name:        internetSearchName,
		Description: "Search the web for current information on any topic. Returns snippets and source URLs. Use internet.extract to read selected pages before relying on their full contents.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"query":{
					"type":"string",
					"description":"Search query"
				},
				"search_depth":{
					"type":"string",
					"enum":["basic","advanced","fast","ultra-fast"],
					"description":"The depth of the search. basic for generic results, advanced for more thorough search, fast for optimized low latency with high relevance, ultra-fast for prioritizing latency above all else.",
					"default":"basic"
				},
				"topic":{
					"type":"string",
					"enum":["general"],
					"description":"The category of the search.",
					"default":"general"
				},
				"time_range":{
					"type":"string",
					"enum":["day","week","month","year"],
					"description":"The time range back from the current date to include in the search results."
				},
				"start_date":{
					"type":"string",
					"description":"Will return all results after the specified start date. Required format: YYYY-MM-DD.",
					"default":""
				},
				"end_date":{
					"type":"string",
					"description":"Will return all results before the specified end date. Required format: YYYY-MM-DD.",
					"default":""
				},
				"max_results":{
					"type":"number",
					"minimum":5,
					"maximum":20,
					"description":"The maximum number of search results to return.",
					"default":5
				},
				"include_images":{
					"type":"boolean",
					"description":"Include a list of query-related images in the response.",
					"default":false
				},
				"include_image_descriptions":{
					"type":"boolean",
					"description":"Include descriptions for returned images.",
					"default":false
				},
				"include_raw_content":{
					"type":"boolean",
					"description":"Include the cleaned and parsed HTML content of each search result.",
					"default":false
				},
				"include_domains":{
					"type":"array",
					"description":"A list of domains to specifically include in the search results.",
					"items":{"type":"string"},
					"default":[]
				},
				"exclude_domains":{
					"type":"array",
					"description":"A list of domains to specifically exclude from the search results.",
					"items":{"type":"string"},
					"default":[]
				},
				"country":{
					"type":"string",
					"description":"Boost results from a country. Use the full country name, not an ISO code. Available only for general search.",
					"default":""
				},
				"include_favicon":{
					"type":"boolean",
					"description":"Whether to include the favicon URL for each result.",
					"default":false
				},
				"exact_match":{
					"type":"boolean",
					"description":"Only return results containing the exact phrase or phrases in quotes in the query."
				}
			},
			"required":["query"]
		}`),
	}
}

func internetExtractDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeFunction,
		Name:        internetExtractName,
		Description: "Extract content from URLs selected from internet.search results. Returns raw page content in markdown or text format.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"urls":{
					"type":"array",
					"description":"List of URLs to extract content from.",
					"items":{"type":"string"}
				},
				"extract_depth":{
					"type":"string",
					"enum":["basic","advanced"],
					"description":"Use advanced for LinkedIn, protected sites, or tables and embedded content.",
					"default":"basic"
				},
				"include_images":{
					"type":"boolean",
					"description":"Include images from pages.",
					"default":false
				},
				"format":{
					"type":"string",
					"enum":["markdown","text"],
					"description":"Output format.",
					"default":"markdown"
				},
				"include_favicon":{
					"type":"boolean",
					"description":"Include favicon URLs.",
					"default":false
				},
				"query":{
					"type":"string",
					"description":"Query to rerank content chunks by relevance."
				}
			},
			"required":["urls"]
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
