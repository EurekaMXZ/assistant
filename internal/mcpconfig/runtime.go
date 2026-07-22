package mcpconfig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/credential"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	runtimeToolPrefix     = "mcp_"
	runtimeToolHashLength = 12
	maxProviderToolName   = 64
	maxRuntimeOutputBytes = 1 << 20
)

type CompositeRuntime struct {
	StaticCatalog tool.ToolCatalog
	LocalExecutor tool.ToolExecutor
	Repository    RuntimeRepository
	Cipher        *credential.Cipher
	Client        *SDKToolLister
}

var _ tool.ToolCatalog = (*CompositeRuntime)(nil)
var _ tool.ToolExecutor = (*CompositeRuntime)(nil)

func (r *CompositeRuntime) ListTools(ctx context.Context, scope tool.ToolScope) ([]llm.ModelTool, error) {
	var tools []llm.ModelTool
	if r.StaticCatalog != nil {
		staticTools, err := r.StaticCatalog.ListTools(ctx, scope)
		if err != nil {
			return nil, err
		}
		tools = append(tools, staticTools...)
	}
	visibleNames := make(map[string]struct{})
	if err := collectProviderToolNames(tools, "", visibleNames); err != nil {
		return nil, err
	}
	if strings.TrimSpace(scope.OwnerUserID) == "" {
		return tools, nil
	}
	if r.Repository == nil {
		return nil, errors.New("MCP runtime repository is not configured")
	}
	runtimeTools, err := r.Repository.ListEnabledRuntimeTools(ctx, scope.OwnerUserID)
	if err != nil {
		return nil, fmt.Errorf("list runtime MCP tools: %w", err)
	}
	for _, runtimeTool := range runtimeTools {
		name := RuntimeToolName(runtimeTool)
		if _, duplicate := visibleNames[name]; duplicate {
			return nil, fmt.Errorf("duplicate provider-visible tool name %q", name)
		}
		visibleNames[name] = struct{}{}
		description := fmt.Sprintf("MCP server %q: %s", runtimeTool.ServerName, strings.TrimSpace(runtimeTool.Description))
		tools = append(tools, llm.ModelTool{
			Type:        llm.ModelToolTypeFunction,
			Name:        name,
			Description: strings.TrimSpace(description),
			Parameters:  append(json.RawMessage(nil), runtimeTool.InputSchema...),
		})
	}
	return tools, nil
}

func (r *CompositeRuntime) Execute(ctx context.Context, scope tool.ToolScope, call tool.ToolCall) (*tool.ToolExecutionResult, error) {
	name := runtimeCallName(call)
	if !strings.HasPrefix(name, runtimeToolPrefix) {
		if r.LocalExecutor == nil {
			return nil, errors.New("local tool executor is not configured")
		}
		return r.LocalExecutor.Execute(ctx, scope, call)
	}
	if strings.TrimSpace(scope.OwnerUserID) == "" || r.Repository == nil {
		return nil, errors.New("MCP tool is unavailable")
	}
	definitions, err := r.Repository.ListEnabledRuntimeTools(ctx, scope.OwnerUserID)
	if err != nil {
		return nil, errors.New("unable to validate MCP tool availability")
	}
	var selected *RuntimeTool
	for index := range definitions {
		if RuntimeToolName(definitions[index]) == name {
			selected = &definitions[index]
			break
		}
	}
	if selected == nil {
		return nil, errors.New("MCP tool is disabled or unavailable")
	}
	runtimeTool, err := r.Repository.GetEnabledRuntimeTool(ctx, scope.OwnerUserID, selected.ServerID, selected.ToolName)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, errors.New("MCP tool is disabled or unavailable")
	}
	if err != nil {
		return nil, errors.New("unable to validate MCP tool availability")
	}
	return r.executeMCP(ctx, call, runtimeTool)
}

func (r *CompositeRuntime) executeMCP(ctx context.Context, call tool.ToolCall, runtimeTool *RuntimeTool) (*tool.ToolExecutionResult, error) {
	if runtimeTool == nil || r.Cipher == nil {
		return nil, errors.New("MCP tool configuration is unavailable")
	}
	parameters, err := decryptRuntimeSecrets(r.Cipher, runtimeTool.ServerID, parametersPurpose, runtimeTool.EncryptedParameters, runtimeTool.ParametersNonce)
	if err != nil {
		return nil, errors.New("MCP tool configuration cannot be decrypted")
	}
	headers, err := decryptRuntimeSecrets(r.Cipher, runtimeTool.ServerID, headersPurpose, runtimeTool.EncryptedHeaders, runtimeTool.HeadersNonce)
	if err != nil {
		return nil, errors.New("MCP tool configuration cannot be decrypted")
	}
	arguments, err := decodeToolArguments(call.Arguments)
	if err != nil {
		return nil, domain.NewValidationError("MCP tool arguments must be a JSON object")
	}
	client := r.Client
	if client == nil {
		client = &SDKToolLister{}
	}
	runtimeCtx, cancel := context.WithTimeout(ctx, connectionTimeout)
	defer cancel()
	// Runtime calls deliberately use a fresh session so configuration and enabled state are revalidated per call.
	session, err := client.connect(runtimeCtx, runtimeTool.EndpointURL, parameters, headers)
	if err != nil {
		return nil, errors.New("MCP tool transport failed before tools/call completed")
	}
	defer closeClientSession(runtimeCtx, session)
	result, err := session.CallTool(runtimeCtx, &mcpsdk.CallToolParams{Name: runtimeTool.ToolName, Arguments: arguments})
	if err != nil {
		return nil, errors.New("MCP tools/call outcome is uncertain")
	}
	return runtimeExecutionResult(call.CallID, result)
}

func RuntimeToolName(runtimeTool RuntimeTool) string {
	hash := sha256.Sum256([]byte(runtimeTool.ServerID + "\x00" + runtimeTool.ToolName))
	suffix := hex.EncodeToString(hash[:])[:runtimeToolHashLength]
	safeToolName := strings.Trim(llm.SafeToolName(runtimeTool.ToolName), "_-")
	if safeToolName == "" {
		safeToolName = "tool"
	}
	maxSafeLength := maxProviderToolName - len(runtimeToolPrefix) - 1 - len(suffix)
	if len(safeToolName) > maxSafeLength {
		safeToolName = safeToolName[:maxSafeLength]
	}
	return runtimeToolPrefix + safeToolName + "_" + suffix
}

func collectProviderToolNames(tools []llm.ModelTool, namespace string, names map[string]struct{}) error {
	for _, modelTool := range tools {
		name := strings.TrimSpace(modelTool.Name)
		switch modelTool.Type {
		case llm.ModelToolTypeNamespace:
			if err := collectProviderToolNames(modelTool.Tools, joinRuntimeNamespace(namespace, name), names); err != nil {
				return err
			}
		case llm.ModelToolTypeFunction:
			visibleName := llm.SafeToolName(joinRuntimeNamespace(namespace, name))
			if visibleName == "" || len(visibleName) > maxProviderToolName {
				return fmt.Errorf("provider-visible tool name is invalid")
			}
			if _, duplicate := names[visibleName]; duplicate {
				return fmt.Errorf("duplicate provider-visible tool name %q", visibleName)
			}
			names[visibleName] = struct{}{}
		}
	}
	return nil
}

func joinRuntimeNamespace(namespace string, name string) string {
	if strings.TrimSpace(namespace) == "" {
		return strings.TrimSpace(name)
	}
	if strings.Contains(name, ".") {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(namespace) + "." + strings.TrimSpace(name)
}

func runtimeCallName(call tool.ToolCall) string {
	if strings.TrimSpace(call.Namespace) == "" {
		return strings.TrimSpace(call.Name)
	}
	return llm.SafeToolName(joinRuntimeNamespace(call.Namespace, call.Name))
}

func decryptRuntimeSecrets(cipher *credential.Cipher, serverID string, purpose string, ciphertext []byte, nonce []byte) (map[string]string, error) {
	payload, err := cipher.Decrypt(serverID, purpose, ciphertext, nonce)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	if err := json.Unmarshal([]byte(payload), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func decodeToolArguments(raw json.RawMessage) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var arguments map[string]any
	if err := decoder.Decode(&arguments); err != nil || arguments == nil {
		return nil, errors.New("invalid arguments")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid arguments")
	}
	return arguments, nil
}

func runtimeExecutionResult(callID string, result *mcpsdk.CallToolResult) (*tool.ToolExecutionResult, error) {
	if result == nil {
		return nil, errors.New("MCP tools/call returned no result")
	}
	payload := map[string]any{
		"ok":      !result.IsError,
		"content": result.Content,
	}
	if result.StructuredContent != nil {
		payload["structured_content"] = result.StructuredContent
	}
	if result.IsError {
		payload["is_error"] = true
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("marshal MCP tool result")
	}
	failed := result.IsError
	if len(encoded) > maxRuntimeOutputBytes {
		failed = true
		encoded = []byte(`{"ok":false,"is_error":true,"error":{"type":"mcp_result_too_large","message":"MCP tool result exceeded the output limit"}}`)
	}
	return &tool.ToolExecutionResult{
		Failed: failed,
		OutputItem: llm.ModelItem{
			Type: llm.ModelItemFunctionCallOutput, CallID: callID, Output: string(encoded),
		},
	}, nil
}
