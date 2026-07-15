package tool

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/llm"
)

type StaticCatalog struct {
	Tools             []llm.ModelTool
	EnableSandboxExec bool
}

func (c StaticCatalog) listTools(scope ToolScope) ([]llm.ModelTool, error) {
	tools := c.Tools
	if len(tools) == 0 {
		tools = DefaultTools()
	}

	filtered := make([]llm.ModelTool, 0, len(tools))
	for _, tool := range tools {
		filteredTool, ok := c.filterToolForScope(tool, "", scope)
		if !ok {
			continue
		}
		filtered = append(filtered, filteredTool)
	}
	return filtered, nil
}

func (c StaticCatalog) ListTools(_ context.Context, scope ToolScope) ([]llm.ModelTool, error) {
	return c.listTools(scope)
}

func cloneModelTool(tool llm.ModelTool) llm.ModelTool {
	cloned := tool
	cloned.Parameters = append(json.RawMessage(nil), tool.Parameters...)
	if len(tool.AllowedTools) > 0 {
		cloned.AllowedTools = append([]string(nil), tool.AllowedTools...)
	}
	if len(tool.Headers) > 0 {
		cloned.Headers = make(map[string]string, len(tool.Headers))
		for key, value := range tool.Headers {
			cloned.Headers[key] = value
		}
	}
	if len(tool.Raw) > 0 {
		cloned.Raw = append(json.RawMessage(nil), tool.Raw...)
	}
	if len(tool.Tools) > 0 {
		cloned.Tools = make([]llm.ModelTool, len(tool.Tools))
		for i, child := range tool.Tools {
			cloned.Tools[i] = cloneModelTool(child)
		}
	}
	return cloned
}

func (c StaticCatalog) filterToolForScope(tool llm.ModelTool, namespace string, scope ToolScope) (llm.ModelTool, bool) {
	fullName := qualifiedToolName(namespace, tool.Name)
	if tool.Type == llm.ModelToolTypeNamespace {
		cloned := cloneModelTool(tool)
		cloned.Tools = nil
		for _, child := range tool.Tools {
			filteredChild, ok := c.filterToolForScope(child, fullName, scope)
			if !ok {
				continue
			}
			cloned.Tools = append(cloned.Tools, filteredChild)
		}
		if len(cloned.Tools) == 0 {
			return llm.ModelTool{}, false
		}
		return cloned, true
	}

	if !c.toolEnabledInScope(fullName, scope) {
		return llm.ModelTool{}, false
	}

	return cloneModelTool(tool), true
}

func (c StaticCatalog) toolEnabledInScope(toolName string, scope ToolScope) bool {
	switch toolName {
	case SandboxCreate:
		return !scope.HasSandbox
	case SandboxDestroy:
		return scope.HasSandbox
	case SandboxExec, SandboxImportAttachment:
		return scope.HasSandbox && c.EnableSandboxExec
	default:
		return true
	}
}

func qualifiedToolName(namespace string, name string) string {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" {
		return name
	}
	if name == "" {
		return namespace
	}
	return namespace + "." + name
}
