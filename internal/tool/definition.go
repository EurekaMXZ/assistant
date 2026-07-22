package tool

import (
	"encoding/json"

	"github.com/EurekaMXZ/assistant/internal/llm"
)

const (
	conversationNamespace = "conversation"
	sandboxNamespace      = "sandbox"
	internetNamespace     = "internet"
	askUserName           = "ask_user"

	conversationRenameTitleName = "rename_title"
	conversationExportTextName  = "export_text"
	sandboxCreateName           = "create"
	sandboxDestroyName          = "destroy"
	sandboxExecName             = "exec"
	sandboxImportAttachmentName = "import_attachment"
	sandboxExportFileName       = "export_file"
	internetSearchName          = "search"
	internetExtractName         = "extract"
)

const (
	ConversationRenameTitle = conversationNamespace + "." + conversationRenameTitleName
	ConversationExportText  = conversationNamespace + "." + conversationExportTextName
	SandboxCreate           = sandboxNamespace + "." + sandboxCreateName
	SandboxDestroy          = sandboxNamespace + "." + sandboxDestroyName
	SandboxExec             = sandboxNamespace + "." + sandboxExecName
	SandboxImportAttachment = sandboxNamespace + "." + sandboxImportAttachmentName
	SandboxExportFileTool   = sandboxNamespace + "." + sandboxExportFileName
	WebSearch               = internetNamespace + "." + internetSearchName
	WebExtract              = internetNamespace + "." + internetExtractName
	AskUser                 = askUserName
)

func DefaultTools() []llm.ModelTool {
	return []llm.ModelTool{
		conversationNamespaceDefinition(),
		sandboxNamespaceDefinition(),
		askUserDefinition(),
		imageGenerationDefinition(),
	}
}

func askUserDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeFunction,
		Name:        askUserName,
		Description: "Pause the current turn for a user decision. Use this tool primarily for binary yes-or-no confirmation. For a simple everyday task such as ordering, first select the single best complete option yourself from context, distance, availability, price, and coupons; then call ask_user once with exactly two options to confirm or reject it. If rejected, select one materially different next-best option and confirm once more instead of interviewing the user. Avoid multi-option questionnaires; use more than two options only when a fixed non-binary choice is genuinely necessary. For ordinary confirmation use single_choice with action null. Use external_action when the user must open an external website or deeplink to continue. Never embed a deeplink in Markdown or ordinary assistant text; pass the exact URL in action.url. The tool may accompany independent tool calls and completes only after the user chooses an option.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"prompt":{"type":"string","description":"The concise question shown to the user. For confirmation, include the relevant impact or order summary."},
				"kind":{"type":"string","enum":["single_choice","external_action"]},
				"options":{
					"type":"array","minItems":2,"maxItems":6,
					"description":"Normally exactly two options representing yes and no. Use additional options only for an exceptional fixed non-binary choice.",
					"items":{
						"type":"object",
						"properties":{
							"id":{"type":"string"},
							"label":{"type":"string"},
							"tone":{"type":"string","enum":["primary","neutral","danger"]}
						},
						"required":["id","label","tone"],
						"additionalProperties":false
					}
				},
				"action":{
					"type":["object","null"],
					"description":"Required for external_action; use null for single_choice. The URL may be a secure HTTPS URL or any deeplink returned by a trusted tool.",
					"properties":{"label":{"type":"string"},"url":{"type":"string"}},
					"required":["label","url"],
					"additionalProperties":false
				}
			},
			"required":["prompt","kind","options","action"],
			"additionalProperties":false
		}`),
		Strict: true,
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
		conversationExportTextDefinition(),
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
		sandboxExportFileDefinition(),
	)
}

func internetNamespaceDefinition() llm.ModelTool {
	return namespaceDefinition(
		internetNamespace,
		"Tools for mandatory two-stage public web research. Use search only to discover candidate sources, then always use extract on the smallest relevant set before answering from web evidence. Never rely on search snippets alone.",
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

func conversationExportTextDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeFunction,
		Name:        conversationExportTextName,
		Description: "Create a UTF-8 text file and attach it to the assistant response. Use this for short text, Markdown, CSV, JSON, XML, code, or configuration files that do not need sandbox processing. The attachment is delivered automatically; do not include a download URL.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"filename":{"type":"string","description":"Download filename, including a useful extension."},
				"content":{"type":"string","description":"Complete UTF-8 file content."}
			},
			"required":["filename","content"],
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

func sandboxExportFileDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeFunction,
		Name:        sandboxExportFileName,
		Description: "Attach an existing regular file from the active sandbox workspace to the assistant response. Use this after creating a file in /workspace. The attachment is delivered automatically; do not include a download URL.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"path":{"type":"string","description":"Absolute path inside /workspace, or a path relative to /workspace."},
				"filename":{"type":["string","null"],"description":"Optional download filename. Use null to preserve the sandbox filename."}
			},
			"required":["path","filename"],
			"additionalProperties":false
		}`),
		Strict: true,
	}
}

func internetSearchDefinition() llm.ModelTool {
	return llm.ModelTool{
		Type:        llm.ModelToolTypeFunction,
		Name:        internetSearchName,
		Description: "First-stage source discovery only. Returns candidate URLs and short snippets, not sufficient page evidence. Do not answer from this output alone: after the final search, always call internet.extract on the smallest relevant set of returned URLs. For a single-day date filter, set start_date to that day and end_date to the following day.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"query":{
					"type":"string",
					"description":"First-stage discovery query. Keep it focused enough to identify candidate sources; use internet.extract afterward for page-level evidence."
				},
				"search_depth":{
					"type":"string",
					"enum":["basic","advanced","fast","ultra-fast"],
					"description":"Discovery depth only: basic is the normal first pass; advanced spends more time finding relevant sources and snippets; fast favors latency with relevance; ultra-fast minimizes latency. Regardless of depth, follow with internet.extract before relying on source content.",
					"default":"basic"
				},
				"topic":{
					"type":"string",
					"enum":["general"],
					"description":"Search category. This integration supports general only; leave it as general.",
					"default":"general"
				},
				"time_range":{
					"type":"string",
					"enum":["day","week","month","year"],
					"description":"Relative date filter. Mutually exclusive with start_date and end_date: use time_range OR explicit dates, never both."
				},
				"start_date":{
					"type":"string",
					"pattern":"^$|^[0-9]{4}-(0[1-9]|1[0-2])-([0-2][0-9]|3[01])$",
					"description":"Inclusive lower date bound in YYYY-MM-DD. If start_date or end_date is set, omit time_range. When both dates are set, start_date must be earlier than end_date. For a single day, use that date here and the following date as end_date.",
					"default":""
				},
				"end_date":{
					"type":"string",
					"pattern":"^$|^[0-9]{4}-(0[1-9]|1[0-2])-([0-2][0-9]|3[01])$",
					"description":"Exclusive upper date bound in YYYY-MM-DD. If start_date or end_date is set, omit time_range. When both dates are set, end_date must be later than start_date and must not equal it.",
					"default":""
				},
				"max_results":{
					"type":"integer",
					"minimum":5,
					"maximum":20,
					"description":"Number of discovery results, integer 5 through 20. Use the default 5 for typical queries; increase only when broader source discovery is necessary.",
					"default":5
				},
				"include_images":{
					"type":"boolean",
					"description":"Include query-related image URLs. Normally false; enable only when the user needs images.",
					"default":false
				},
				"include_image_descriptions":{
					"type":"boolean",
					"description":"Include image descriptions. Meaningful only when include_images is true; otherwise keep false.",
					"default":false
				},
				"include_raw_content":{
					"type":"boolean",
					"enum":[false],
					"description":"Must always be false. Search returns discovery snippets only; use internet.extract to read selected source content.",
					"default":false
				},
				"include_domains":{
					"type":"array",
					"description":"Optional domain allowlist for discovery. Use only when the user requests specific sites; do not place the same domain in exclude_domains.",
					"items":{"type":"string"},
					"default":[]
				},
				"exclude_domains":{
					"type":"array",
					"description":"Optional domain denylist for discovery. Use only when exclusions are required; do not place the same domain in include_domains.",
					"items":{"type":"string"},
					"default":[]
				},
				"country":{
					"type":"string",
					"description":"Optional country boost, available only with topic=general. Use the full country name such as United States or Japan, never an ISO code.",
					"default":""
				},
				"include_favicon":{
					"type":"boolean",
					"description":"Include favicon URLs. Normally false unless the presentation specifically needs them.",
					"default":false
				},
				"exact_match":{
					"type":"boolean",
					"description":"Exact-phrase filtering. Set true only when query contains at least one non-empty phrase in double quotes; otherwise it is ignored."
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
		Description: "Mandatory second-stage web retrieval after internet.search. Read the smallest relevant set of URLs selected from search results before answering from web evidence. Returns page content in markdown or text.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"urls":{
					"type":"array",
					"description":"One to 20 source URLs selected from the preceding internet.search results. Prefer the smallest set needed to verify the answer.",
					"items":{"type":"string"},
					"minItems":1,
					"maxItems":20
				},
				"extract_depth":{
					"type":"string",
					"enum":["basic","advanced"],
					"description":"Extraction depth: basic is the normal choice; use advanced only for protected pages, LinkedIn, tables, or embedded content.",
					"default":"basic"
				},
				"include_images":{
					"type":"boolean",
					"description":"Include page images only when the user needs visual assets; otherwise keep false.",
					"default":false
				},
				"format":{
					"type":"string",
					"enum":["markdown","text"],
					"description":"Extracted content format. Use markdown by default; use text only when markup is not useful.",
					"default":"markdown"
				},
				"include_favicon":{
					"type":"boolean",
					"description":"Include favicon URLs only when needed for presentation; otherwise keep false.",
					"default":false
				},
				"query":{
					"type":"string",
					"description":"Optional focused intent for reranking extracted chunks. Set it to the exact evidence needed when full-page extraction would be noisy."
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
