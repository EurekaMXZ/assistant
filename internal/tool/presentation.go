package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type PublicToolLink struct {
	URL   string
	Label string
}

type PublicToolPresentation struct {
	Title            string
	Summary          string
	Details          []string
	InputLabel       string
	InputText        string
	Links            []PublicToolLink
	Command          string
	WorkingDirectory string
	CommandOutput    string
	ExitCode         *int
	TimedOut         bool
}

func BuildPublicToolPresentation(namespace string, serverLabel string, name string, status string, arguments json.RawMessage, output []byte, errorMessage string) PublicToolPresentation {
	toolName := presentationToolName(namespace, serverLabel, name)
	status = strings.TrimSpace(status)
	args := decodeObject(arguments)
	result := decodeValue(output)

	summary, details := summarizeKnownTool(toolName, status, args, result)
	if summary == "" {
		summary = genericToolSummary(toolName, status)
	}
	if len(details) == 0 {
		details = append(details, "Tool: "+toolName)
	}

	if errorMessage = strings.TrimSpace(errorMessage); errorMessage != "" {
		details = append(details, "Error: "+truncateDisplayValue(errorMessage, 240))
	}

	presentation := PublicToolPresentation{
		Summary: summary,
		Details: compactDetails(details),
	}
	applyTavilyPublicPresentation(&presentation, toolName, args, result)
	applySandboxPublicPresentation(&presentation, toolName, status, args, result)
	return presentation
}

func applySandboxPublicPresentation(presentation *PublicToolPresentation, toolName string, status string, args map[string]any, result any) {
	if presentation == nil {
		return
	}
	switch toolName {
	case SandboxCreate:
		presentation.Title = statusSummary(status, "正在创建沙箱", "沙箱已创建", "创建沙箱失败")
	case SandboxDestroy:
		presentation.Title = statusSummary(status, "正在销毁沙箱", "沙箱已销毁", "销毁沙箱失败")
	case SandboxExec:
		presentation.Title = statusSummary(status, "正在执行命令", "命令执行完成", "命令执行失败")
		commandSource := args
		if output := nestedObject(result, "result"); output != nil {
			commandSource = output
			presentation.CommandOutput = rawStringField(output, "output")
			if _, hasOutput := output["output"]; !hasOutput {
				presentation.CommandOutput = mergeLegacyCommandOutput(rawStringField(output, "stdout"), rawStringField(output, "stderr"))
			}
			if exitCode, ok := intField(output, "exit_code"); ok {
				presentation.ExitCode = &exitCode
			}
			presentation.TimedOut, _ = output["timed_out"].(bool)
		}
		presentation.Command = commandLineRaw(commandSource)
		presentation.WorkingDirectory = strings.TrimSpace(rawStringField(commandSource, "working_directory"))
	case SandboxImportAttachment:
		presentation.Title = statusSummary(status, "正在导入附件", "附件已导入沙箱", "导入附件失败")
		presentation.InputLabel = "Attachment"
		presentation.InputText = stringField(args, "attachment_id")
		if attachment := nestedObject(result, "attachment"); attachment != nil {
			presentation.Details = compactDetails([]string{
				"File: " + rawStringField(attachment, "filename"),
				"Sandbox path: " + rawStringField(attachment, "sandbox_path"),
			})
		}
	}
}

func mergeLegacyCommandOutput(stdout string, stderr string) string {
	if stdout == "" {
		return stderr
	}
	if stderr == "" {
		return stdout
	}
	separator := "\n"
	if strings.HasSuffix(stdout, "\n") {
		separator = ""
	}
	return stdout + separator + stderr
}

func applyTavilyPublicPresentation(presentation *PublicToolPresentation, toolName string, args map[string]any, result any) {
	if presentation == nil {
		return
	}
	includeArgumentURLs := false
	switch toolName {
	case WebSearch:
		presentation.Title = "Searching the Web"
		presentation.InputLabel = "Keywords"
		presentation.InputText = stringField(args, "query")
	case WebExtract:
		presentation.Title = "Reading Web Content"
		presentation.InputLabel = "Query"
		presentation.InputText = firstNonEmptyStringField(args, "query", "extraction_prompt")
		includeArgumentURLs = true
	default:
		return
	}

	links := make([]PublicToolLink, 0)
	if includeArgumentURLs {
		links = appendPublicArgumentLinks(links, args)
	}
	links = collectPublicToolLinks(links, result, false)
	presentation.Links = compactPublicToolLinks(links, 24)
	presentation.Details = tavilyPublicDetails(toolName, presentation.InputLabel, presentation.InputText, result)
}

func tavilyPublicDetails(toolName string, inputLabel string, inputText string, result any) []string {
	if toolName == WebSearch {
		inputLabel = "Query"
	}
	details := optionalDetail(inputLabel, inputText)
	if toolName == WebSearch {
		if count, ok := resultCount(result); ok {
			details = append(details, fmt.Sprintf("Results: %d", count))
		}
	}
	return compactDetails(details)
}

func firstNonEmptyStringField(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringField(object, key); value != "" {
			return value
		}
	}
	return ""
}

func appendPublicArgumentLinks(links []PublicToolLink, args map[string]any) []PublicToolLink {
	if args == nil {
		return links
	}
	if value, ok := args["url"].(string); ok {
		links = appendSanitizedPublicToolLink(links, value)
	}
	if values, ok := args["urls"].([]any); ok {
		for _, value := range values {
			if text, ok := value.(string); ok {
				links = appendSanitizedPublicToolLink(links, text)
			}
		}
	}
	return links
}

func collectPublicToolLinks(links []PublicToolLink, value any, allowString bool) []PublicToolLink {
	switch typed := value.(type) {
	case string:
		if allowString {
			links = appendSanitizedPublicToolLink(links, typed)
		}
	case []any:
		for _, item := range typed {
			links = collectPublicToolLinks(links, item, allowString)
		}
	case map[string]any:
		for key, item := range typed {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "url", "href", "source_url":
				links = collectPublicToolLinks(links, item, true)
			case "urls", "results", "sources", "failed_results":
				links = collectPublicToolLinks(links, item, true)
			case "data", "result", "response":
				links = collectPublicToolLinks(links, item, false)
			}
		}
	}
	return links
}

func appendSanitizedPublicToolLink(links []PublicToolLink, raw string) []PublicToolLink {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) || parsed.Hostname() == "" {
		return links
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.User = nil
	parsed.Fragment = ""
	query := parsed.Query()
	for key := range query {
		if isSensitiveURLParameter(key) {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	value := parsed.String()
	if len(value) > 2048 {
		return links
	}
	label := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	return append(links, PublicToolLink{URL: value, Label: label})
}

func isSensitiveURLParameter(key string) bool {
	normalized := strings.NewReplacer("-", "", "_", "", ".", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch normalized {
	case "apikey", "accesstoken", "authtoken", "authorization", "credential", "credentials", "key", "password", "secret", "signature", "sig", "token":
		return true
	default:
		return false
	}
}

func compactPublicToolLinks(links []PublicToolLink, limit int) []PublicToolLink {
	compacted := make([]PublicToolLink, 0, len(links))
	seen := make(map[string]struct{}, len(links))
	for _, link := range links {
		if _, ok := seen[link.URL]; ok {
			continue
		}
		seen[link.URL] = struct{}{}
		compacted = append(compacted, link)
		if limit > 0 && len(compacted) >= limit {
			break
		}
	}
	if len(compacted) == 0 {
		return nil
	}
	return compacted
}

func presentationToolName(namespace string, serverLabel string, name string) string {
	namespace = strings.TrimSpace(namespace)
	serverLabel = strings.TrimSpace(serverLabel)
	name = strings.TrimSpace(name)
	if namespace == "" {
		namespace = serverLabel
	}
	if namespace == "" {
		return name
	}
	if name == "" {
		return namespace
	}
	return namespace + "." + name
}

func summarizeKnownTool(toolName string, status string, args map[string]any, result any) (string, []string) {
	switch toolName {
	case ConversationRenameTitle:
		title := stringField(args, "title")
		return statusSummary(status, "Renaming conversation", "Renamed conversation", "Could not rename conversation"), optionalDetail("New title", title)
	case SandboxCreate:
		return statusSummary(status, "Creating sandbox", "Created sandbox", "Could not create sandbox"), nil
	case SandboxDestroy:
		return statusSummary(status, "Destroying sandbox", "Destroyed sandbox", "Could not destroy sandbox"), nil
	case SandboxExec:
		command := commandLine(args)
		return statusSummary(status, "Running sandbox command", "Ran sandbox command", "Sandbox command failed"), optionalDetail("Command", command)
	case WebSearch:
		details := optionalDetail("Query", stringField(args, "query"))
		if count, ok := resultCount(result); ok {
			details = append(details, fmt.Sprintf("Results: %d", count))
		}
		return statusSummary(status, "Searching the web", "Searched the web", "Web search failed"), details
	case WebExtract:
		details := urlsDetail(args)
		if count, ok := resultCount(result); ok {
			details = append(details, fmt.Sprintf("Pages read: %d", count))
		}
		return statusSummary(status, "Reading web content", "Read web content", "Could not read web content"), details
	default:
		return "", nil
	}
}

func statusSummary(status string, started string, completed string, failed string) string {
	switch status {
	case "started":
		return started
	case "completed":
		return completed
	case "failed":
		return failed
	default:
		if status == "" {
			return completed
		}
		return completed
	}
}

func genericToolSummary(toolName string, status string) string {
	label := humanizeToolName(toolName)
	switch status {
	case "started":
		return "Using " + label
	case "failed":
		return label + " failed"
	default:
		return "Used " + label
	}
}

func humanizeToolName(toolName string) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return "tool"
	}
	toolName = strings.ReplaceAll(toolName, ".", " ")
	toolName = strings.ReplaceAll(toolName, "_", " ")
	return toolName
}

func decodeObject(raw json.RawMessage) map[string]any {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil
	}
	return object
}

func decodeValue(raw []byte) any {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	return value
}

func stringField(object map[string]any, key string) string {
	if object == nil {
		return ""
	}
	switch value := object[key].(type) {
	case string:
		return truncateDisplayValue(value, 240)
	case json.Number:
		return value.String()
	case float64:
		return fmt.Sprintf("%g", value)
	case bool:
		if value {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func rawStringField(object map[string]any, key string) string {
	if object == nil {
		return ""
	}
	value, _ := object[key].(string)
	return value
}

func nestedObject(value any, key string) map[string]any {
	object, _ := value.(map[string]any)
	nested, _ := object[key].(map[string]any)
	return nested
}

func intField(object map[string]any, key string) (int, bool) {
	if object == nil {
		return 0, false
	}
	switch value := object[key].(type) {
	case json.Number:
		parsed, err := value.Int64()
		return int(parsed), err == nil
	case float64:
		return int(value), value == float64(int(value))
	default:
		return 0, false
	}
}

func resultCount(value any) (int, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return 0, false
	}
	for _, key := range []string{"results", "items", "urls"} {
		values, ok := object[key].([]any)
		if ok {
			return len(values), true
		}
	}
	return 0, false
}

func optionalDetail(label string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{label + ": " + value}
}

func urlsDetail(args map[string]any) []string {
	if args == nil {
		return nil
	}
	values, ok := args["urls"].([]any)
	if !ok || len(values) == 0 {
		return optionalDetail("URL", stringField(args, "url"))
	}
	details := []string{fmt.Sprintf("URLs: %d", len(values))}
	if first, ok := values[0].(string); ok && strings.TrimSpace(first) != "" {
		details = append(details, "First URL: "+truncateDisplayValue(first, 240))
	}
	return details
}

func commandLine(args map[string]any) string {
	command := stringField(args, "command")
	if command == "" {
		return ""
	}
	values, ok := args["args"].([]any)
	if !ok || len(values) == 0 {
		return command
	}
	parts := []string{command}
	for _, value := range values {
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			continue
		}
		parts = append(parts, text)
	}
	return truncateDisplayValue(strings.Join(parts, " "), 240)
}

func commandLineRaw(args map[string]any) string {
	command := strings.TrimSpace(rawStringField(args, "command"))
	if command == "" {
		return ""
	}
	parts := []string{shellQuote(command)}
	values, _ := args["args"].([]any)
	for _, value := range values {
		text, ok := value.(string)
		if ok {
			parts = append(parts, shellQuote(text))
		}
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("_@%+=:,./-", r) {
			continue
		}
		return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
	}
	return value
}

func compactDetails(values []string) []string {
	compacted := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		compacted = append(compacted, value)
	}
	if len(compacted) == 0 {
		return nil
	}
	return compacted
}

func truncateDisplayValue(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 1 {
		return value[:max]
	}
	return value[:max-1] + "..."
}
