package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

const toolRunProviderOpenAIResponses = "openai.responses"
const defaultRemoteToolReplayMaxBytes = 16 * 1024
const defaultModelToolOutputMaxTokens = 10_000

type ToolRunInput struct {
	Scope               tool.ToolScope
	Model               string
	ContextWindowTokens int
	CatalogModelID      string
	ModelRevision       int64
	ModelPriceID        string
	PricingSnapshot     json.RawMessage
	CredentialID        string
	ProviderBaseURL     string
	ReasoningEffort     string
	ReasoningSummary    string
	TextVerbosity       string
	DisableTools        bool
	Instructions        string
	Input               []llm.ModelItem
	PromptCacheKey      string
	MaxOutputTokens     int
	Metadata            map[string]string
	ParallelToolCalls   *bool
}

type ToolRunResult struct {
	Model        *llm.ModelResult
	Tools        []llm.ModelTool
	ContextItems []llm.ModelItem
}

type ToolOrchestrator struct {
	model                    llm.ModelClient
	catalog                  tool.ToolCatalog
	executor                 tool.ToolExecutor
	publisher                stream.Publisher
	artifacts                ToolArtifactStore
	calls                    ToolCallStore
	remoteToolReplayMaxBytes int
	modelToolOutputMaxTokens int
}

func NewToolOrchestrator(model llm.ModelClient, catalog tool.ToolCatalog, executor tool.ToolExecutor, publisher stream.Publisher, artifacts ToolArtifactStore, calls ToolCallStore) *ToolOrchestrator {
	return &ToolOrchestrator{
		model:     model,
		catalog:   catalog,
		executor:  executor,
		publisher: publisher,
		artifacts: artifacts,
		calls:     calls,
	}
}

func publicToolRunError(err error) string {
	if errors.Is(err, llm.ErrUpstreamRequestFailed) {
		return domain.TurnPublicErrorUpstreamRequestFailed
	}
	return domain.TurnPublicErrorRequestProcessing
}

func (o *ToolOrchestrator) persistTurnRunOutputItems(ctx context.Context, scope tool.ToolScope, stepIndex int, result *llm.ModelResult) error {
	if o == nil || o.artifacts == nil || result == nil || len(result.OutputItems) == 0 {
		return nil
	}

	items := make([]json.RawMessage, 0, len(result.OutputItems))
	for _, item := range result.OutputItems {
		if len(item.Raw) > 0 {
			items = append(items, append(json.RawMessage(nil), item.Raw...))
			continue
		}

		raw, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("marshal turn run output item: %w", err)
		}
		items = append(items, raw)
	}

	payload, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("marshal turn run output items: %w", err)
	}

	key := o.artifacts.TurnRunOutputItemsKey(scope.ConversationID, scope.TurnID, stepIndex)
	if err := o.artifacts.PutBytes(ctx, key, payload, "application/json"); err != nil {
		return fmt.Errorf("persist turn run output items: %w", err)
	}

	return nil
}

func (o *ToolOrchestrator) recordToolCallStart(ctx context.Context, scope tool.ToolScope, run *domain.TurnRun, call tool.ToolCall) (*domain.ToolCallRecord, bool, error) {
	if o == nil || o.artifacts == nil || o.calls == nil || run == nil {
		return nil, true, nil
	}

	argumentsKey := o.artifacts.ToolCallArgumentsKey(scope.ConversationID, scope.TurnID, call.CallID)
	record, acquired, err := o.calls.AcquireToolCall(ctx, scope.TurnID, run.ID, run.Attempt, call, argumentsKey)
	if err != nil {
		return nil, false, fmt.Errorf("acquire tool call: %w", err)
	}
	if record != nil {
		record.ArgumentsBlobKey = argumentsKey
	}
	if acquired {
		payload := toolCallRequestPayload(call)
		if err := o.artifacts.PutBytes(ctx, argumentsKey, payload, "application/json"); err != nil {
			if record != nil {
				_, _ = o.calls.FailToolCall(ctx, record.ID, "", err.Error())
			}
			return nil, false, fmt.Errorf("persist tool call arguments: %w", err)
		}
	}

	return record, acquired, nil
}

func (o *ToolOrchestrator) recordToolCallSuccess(ctx context.Context, scope tool.ToolScope, record *domain.ToolCallRecord, call tool.ToolCall, execution *tool.ToolExecutionResult) error {
	if o == nil || record == nil || o.artifacts == nil || o.calls == nil {
		return nil
	}

	outputKey := ""
	if execution != nil && strings.TrimSpace(execution.OutputItem.Output) != "" {
		outputKey = o.artifacts.ToolCallOutputKey(scope.ConversationID, scope.TurnID, call.CallID)
		if err := o.artifacts.PutBytes(ctx, outputKey, []byte(execution.OutputItem.Output), "application/json"); err != nil {
			return fmt.Errorf("persist tool call output: %w", err)
		}
	}
	record.OutputBlobKey = outputKey
	record.ErrorMessage = ""
	record.Status = domain.ToolCallStatusCompleted

	if _, err := o.calls.CompleteToolCall(ctx, record.ID, outputKey); err != nil {
		return fmt.Errorf("complete tool call: %w", err)
	}

	return nil
}

func (o *ToolOrchestrator) recordToolCallFailure(ctx context.Context, scope tool.ToolScope, record *domain.ToolCallRecord, call tool.ToolCall, message string, output string) error {
	if o == nil || record == nil || o.calls == nil {
		return nil
	}

	outputKey := ""
	if o.artifacts != nil {
		outputKey = o.artifacts.ToolCallOutputKey(scope.ConversationID, scope.TurnID, call.CallID)
		if err := o.artifacts.PutBytes(ctx, outputKey, []byte(output), "application/json"); err != nil {
			return fmt.Errorf("persist failed tool call output: %w", err)
		}
	}
	record.OutputBlobKey = outputKey
	record.ErrorMessage = message
	record.Status = domain.ToolCallStatusFailed

	if _, err := o.calls.FailToolCall(ctx, record.ID, outputKey, message); err != nil {
		return fmt.Errorf("fail tool call: %w", err)
	}

	return nil
}

func (o *ToolOrchestrator) recordToolCallAmbiguous(ctx context.Context, record *domain.ToolCallRecord, message string) error {
	if o == nil || record == nil || o.calls == nil {
		return nil
	}
	record.OutputBlobKey = ""
	record.ErrorMessage = message
	record.Status = domain.ToolCallStatusAmbiguous
	if _, err := o.calls.MarkToolCallAmbiguous(ctx, record.ID, message); err != nil {
		return fmt.Errorf("mark tool call ambiguous: %w", err)
	}
	return nil
}

func (o *ToolOrchestrator) recordRemoteToolCall(ctx context.Context, scope tool.ToolScope, run *domain.TurnRun, item llm.ModelItem) (*domain.ToolCallRecord, error) {
	if o == nil || run == nil || o.calls == nil {
		return nil, nil
	}

	call := toolCallFromModelItem(item)
	record, acquired, err := o.recordToolCallStart(ctx, scope, run, call)
	if err != nil {
		return nil, err
	}
	if !acquired && record != nil && record.Status != domain.ToolCallStatusCompleted {
		return nil, fmt.Errorf("remote tool call %s has no recoverable durable result", describeToolCall(call))
	}
	if record != nil && record.Status == domain.ToolCallStatusCompleted {
		return record, nil
	}

	payload, contentType := observedToolCallPayload(item)
	if item.Error != "" {
		if record == nil || o.calls == nil {
			return record, nil
		}

		outputKey := ""
		if o.artifacts != nil && len(payload) > 0 {
			outputKey = o.artifacts.ToolCallOutputKey(scope.ConversationID, scope.TurnID, call.CallID)
			if err := o.artifacts.PutBytes(ctx, outputKey, payload, contentType); err != nil {
				return nil, fmt.Errorf("persist failed remote tool call output: %w", err)
			}
		}
		if record != nil {
			record.OutputBlobKey = outputKey
			record.ErrorMessage = item.Error
			record.Status = domain.ToolCallStatusFailed
		}

		if _, err := o.calls.FailToolCall(ctx, record.ID, outputKey, item.Error); err != nil {
			return nil, fmt.Errorf("fail remote tool call: %w", err)
		}
		return record, nil
	}

	outputKey := ""
	if record != nil && o.artifacts != nil && len(payload) > 0 {
		outputKey = o.artifacts.ToolCallOutputKey(scope.ConversationID, scope.TurnID, call.CallID)
		if err := o.artifacts.PutBytes(ctx, outputKey, payload, contentType); err != nil {
			return nil, fmt.Errorf("persist remote tool call output: %w", err)
		}
	}
	if record != nil {
		record.OutputBlobKey = outputKey
		record.ErrorMessage = ""
		record.Status = domain.ToolCallStatusCompleted
	}

	if record != nil {
		if _, err := o.calls.CompleteToolCall(ctx, record.ID, outputKey); err != nil {
			return nil, fmt.Errorf("complete remote tool call: %w", err)
		}
	}

	return record, nil
}

func (o *ToolOrchestrator) listTools(ctx context.Context, scope tool.ToolScope) ([]llm.ModelTool, error) {
	if o.catalog == nil {
		return nil, nil
	}

	tools, err := o.catalog.ListTools(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("list model tools: %w", err)
	}

	return tools, nil
}

func firstUnsupportedAction(result *llm.ModelResult) *llm.ModelItem {
	if result == nil {
		return nil
	}

	for _, item := range result.OutputItems {
		switch item.Type {
		case llm.ModelItemToolSearchCall:
			action := item
			return &action
		}
	}

	return nil
}

func (o *ToolOrchestrator) publish(ctx context.Context, event stream.Event) {
	if o == nil || o.publisher == nil {
		return
	}
	_ = o.publisher.Publish(ctx, event)
}

func (o *ToolOrchestrator) replayOutputItems(items []llm.ModelItem) []llm.ModelItem {
	replayed := cloneModelItems(items)
	limit := o.remoteToolReplayLimit()
	if limit <= 0 {
		return replayed
	}

	for index := range replayed {
		replayed[index] = clipRemoteReplayItem(replayed[index], limit)
	}
	return replayed
}

func (o *ToolOrchestrator) remoteToolReplayLimit() int {
	if o == nil || o.remoteToolReplayMaxBytes <= 0 {
		return defaultRemoteToolReplayMaxBytes
	}
	return o.remoteToolReplayMaxBytes
}

func (o *ToolOrchestrator) modelToolOutputTokenLimit() int {
	if o == nil || o.modelToolOutputMaxTokens <= 0 {
		return defaultModelToolOutputMaxTokens
	}
	return o.modelToolOutputMaxTokens
}

func functionCalls(result *llm.ModelResult) []llm.ModelItem {
	if result == nil {
		return nil
	}

	var items []llm.ModelItem
	for _, item := range result.OutputItems {
		if item.Type != llm.ModelItemFunctionCall {
			continue
		}
		items = append(items, item)
	}
	return items
}

func approvalRequests(result *llm.ModelResult) []llm.ModelItem {
	if result == nil {
		return nil
	}

	var items []llm.ModelItem
	for _, item := range result.OutputItems {
		if item.Type != llm.ModelItemMCPApprovalRequest {
			continue
		}
		items = append(items, item)
	}
	return items
}

func remoteToolCalls(result *llm.ModelResult) []llm.ModelItem {
	if result == nil {
		return nil
	}

	var items []llm.ModelItem
	for _, item := range result.OutputItems {
		if item.Type != llm.ModelItemMCPCall {
			continue
		}
		items = append(items, item)
	}
	return items
}

func remoteContinuationItems(result *llm.ModelResult) []llm.ModelItem {
	if result == nil {
		return nil
	}

	var items []llm.ModelItem
	for _, item := range result.OutputItems {
		switch item.Type {
		case llm.ModelItemMCPListTools, llm.ModelItemMCPCall:
			items = append(items, item)
		}
	}
	return items
}

func cloneModelItems(items []llm.ModelItem) []llm.ModelItem {
	if len(items) == 0 {
		return nil
	}

	cloned := make([]llm.ModelItem, 0, len(items))
	for _, item := range items {
		item.Arguments = append(json.RawMessage(nil), item.Arguments...)
		item.Raw = append(json.RawMessage(nil), item.Raw...)
		if item.Type == llm.ModelItemImageGenerationCall {
			item.Result = ""
			item.Raw = nil
		}
		cloned = append(cloned, item)
	}
	return cloned
}

func clipRemoteReplayItem(item llm.ModelItem, maxBytes int) llm.ModelItem {
	if item.Type != llm.ModelItemMCPCall || maxBytes <= 0 {
		return item
	}

	output := strings.TrimSpace(item.Output)
	if len(output) <= maxBytes {
		return item
	}

	preview := output
	if len(preview) > maxBytes {
		preview = preview[:maxBytes]
	}

	summary, err := json.Marshal(map[string]any{
		"truncated":           true,
		"preview":             preview,
		"original_size_bytes": len(output),
		"note":                "assistant truncated remote tool output before replaying it to the next model step",
	})
	if err != nil {
		return item
	}

	item.Output = string(summary)
	if raw, err := replayRawModelItem(item); err == nil {
		item.Raw = raw
	}
	return item
}

func replayRawModelItem(item llm.ModelItem) (json.RawMessage, error) {
	switch item.Type {
	case llm.ModelItemMCPCall:
		payload := map[string]any{
			"type":         llm.ModelItemMCPCall,
			"server_label": item.ServerLabel,
			"name":         item.Name,
			"call_id":      item.CallID,
		}
		if len(item.Arguments) > 0 {
			payload["arguments"] = rawJSONValue(item.Arguments)
		}
		if strings.TrimSpace(item.Output) != "" {
			payload["output"] = rawJSONValue([]byte(item.Output))
		}
		if strings.TrimSpace(item.Error) != "" {
			payload["error"] = map[string]any{"message": strings.TrimSpace(item.Error)}
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		return raw, nil
	default:
		if len(item.Raw) > 0 {
			return append(json.RawMessage(nil), item.Raw...), nil
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		return raw, nil
	}
}

func rawJSONValue(raw []byte) any {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return ""
	}

	var decoded any
	if json.Unmarshal([]byte(trimmed), &decoded) == nil {
		return decoded
	}
	return trimmed
}

func describeToolCall(call tool.ToolCall) string {
	label := call.Name
	if call.Namespace == "" {
		return label
	}
	if label == "" {
		return call.Namespace
	}
	return call.Namespace + "." + label
}

func toolCallFromModelItem(item llm.ModelItem) tool.ToolCall {
	namespace := item.Namespace
	if namespace == "" && (item.Type == llm.ModelItemMCPCall || item.Type == llm.ModelItemMCPApprovalRequest) {
		namespace = item.ServerLabel
	}
	callID := item.CallID
	if callID == "" && item.Type == llm.ModelItemMCPApprovalRequest {
		callID = item.ApprovalRequestID
	}

	return tool.ToolCall{
		Type:        item.Type,
		CallID:      callID,
		Name:        item.Name,
		Namespace:   namespace,
		ServerLabel: item.ServerLabel,
		Arguments:   append(json.RawMessage(nil), item.Arguments...),
		Raw:         append(json.RawMessage(nil), item.Raw...),
	}
}

func unsupportedApprovalRequestError(item llm.ModelItem) error {
	label := describeModelItem(item)
	if strings.TrimSpace(item.ApprovalRequestID) != "" {
		return fmt.Errorf("%s requested approval %q, but backend tool approvals are disabled; handle approvals inside the tool", label, item.ApprovalRequestID)
	}
	return fmt.Errorf("%s requested approval, but backend tool approvals are disabled; handle approvals inside the tool", label)
}

func describeModelItem(item llm.ModelItem) string {
	label := item.Type
	if item.Namespace != "" {
		label += " " + item.Namespace
	}
	if item.Name != "" {
		label += "." + item.Name
	}
	return label
}

func observedToolCallPayload(item llm.ModelItem) ([]byte, string) {
	if strings.TrimSpace(item.Output) != "" {
		return []byte(item.Output), "application/json"
	}
	if len(item.Raw) > 0 {
		return append([]byte(nil), item.Raw...), "application/json"
	}
	return nil, "application/json"
}

func toolCallRequestPayload(call tool.ToolCall) []byte {
	if len(call.Arguments) > 0 {
		return append([]byte(nil), call.Arguments...)
	}
	if len(call.Raw) > 0 {
		return append([]byte(nil), call.Raw...)
	}
	return []byte("{}")
}

func recordID(record *domain.ToolCallRecord) string {
	if record == nil {
		return ""
	}
	return record.ID
}

func recordTurnRunID(record *domain.ToolCallRecord) string {
	if record == nil {
		return ""
	}
	return strings.TrimSpace(record.TurnRunID)
}

func cloneToolScope(scope tool.ToolScope) tool.ToolScope {
	return scope
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func applyToolScopeDelta(scope tool.ToolScope, call tool.ToolCall) tool.ToolScope {
	switch normalizedToolName(call) {
	case "sandbox.create":
		scope.HasSandbox = true
	case "sandbox.destroy":
		scope.HasSandbox = false
	}
	return scope
}

func normalizedToolName(call tool.ToolCall) string {
	if call.Namespace == "" {
		return strings.TrimSpace(call.Name)
	}
	if strings.TrimSpace(call.Name) == "" {
		return strings.TrimSpace(call.Namespace)
	}
	return strings.TrimSpace(call.Namespace) + "." + strings.TrimSpace(call.Name)
}

func (o *ToolOrchestrator) publishToolCompleted(ctx context.Context, scope tool.ToolScope, record *domain.ToolCallRecord, call tool.ToolCall, payload []byte) {
	if o == nil {
		return
	}
	o.publish(ctx, stream.Event{
		Type:           stream.EventToolCompleted,
		ConversationID: scope.ConversationID,
		TurnID:         scope.TurnID,
		ToolName:       describeToolCall(call),
		Payload:        toolEventPayload(record, call, stream.ToolEventStatusCompleted, payload, ""),
	})
}

func (o *ToolOrchestrator) publishToolFailed(ctx context.Context, scope tool.ToolScope, record *domain.ToolCallRecord, call tool.ToolCall, message string, payload []byte) {
	if o == nil {
		return
	}
	o.publish(ctx, stream.Event{
		Type:           stream.EventToolFailed,
		ConversationID: scope.ConversationID,
		TurnID:         scope.TurnID,
		ToolName:       describeToolCall(call),
		Payload:        toolEventPayload(record, call, stream.ToolEventStatusFailed, payload, message),
		Error:          strings.TrimSpace(message),
	})
}

func (o *ToolOrchestrator) publishRemoteToolCallEvent(ctx context.Context, scope tool.ToolScope, record *domain.ToolCallRecord, item llm.ModelItem) {
	if o == nil {
		return
	}

	call := toolCallFromModelItem(item)
	payload, _ := observedToolCallPayload(item)
	if item.Error != "" {
		o.publishToolFailed(ctx, scope, record, call, item.Error, payload)
	} else {
		o.publishToolCompleted(ctx, scope, record, call, payload)
	}
}

func toolEventPayload(record *domain.ToolCallRecord, call tool.ToolCall, status string, payload []byte, message string) string {
	body := buildToolStreamPayload(record, call, status, payload, strings.TrimSpace(message))
	return marshalToolStreamPayload(body)
}

func (o *ToolOrchestrator) publishReasoningSummary(ctx context.Context, scope tool.ToolScope, run *domain.TurnRun, stepIndex int, result *llm.ModelResult) {
	if o == nil || result == nil {
		return
	}

	items := reasoningStreamItems(result.OutputItems)
	if len(items) == 0 {
		return
	}

	body := stream.ReasoningStreamPayload{
		TurnRunID:  recordIDFromRun(run),
		ResponseID: strings.TrimSpace(result.ResponseID),
		StepIndex:  stepIndex,
		Items:      items,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return
	}

	o.publish(ctx, stream.Event{
		Type:           stream.EventReasoningSummary,
		ConversationID: scope.ConversationID,
		TurnID:         scope.TurnID,
		ResponseID:     result.ResponseID,
		Payload:        string(raw),
	})
}

func (o *ToolOrchestrator) publishToolStarted(ctx context.Context, scope tool.ToolScope, record *domain.ToolCallRecord, call tool.ToolCall) {
	if o == nil {
		return
	}
	o.publish(ctx, stream.Event{
		Type:           stream.EventToolStarted,
		ConversationID: scope.ConversationID,
		TurnID:         scope.TurnID,
		ToolName:       describeToolCall(call),
		Payload:        toolEventPayload(record, call, stream.ToolEventStatusStarted, nil, ""),
	})
}

func buildToolStreamPayload(record *domain.ToolCallRecord, call tool.ToolCall, status string, payload []byte, message string) stream.ToolStreamPayload {
	body := stream.ToolStreamPayload{
		ToolCallRecordID: recordID(record),
		TurnRunID:        recordTurnRunID(record),
		CallID:           strings.TrimSpace(call.CallID),
		ToolName:         describeToolCall(call),
		ToolType:         stableToolType(record, call),
		Namespace:        stableToolNamespace(record, call),
		ServerLabel:      strings.TrimSpace(call.ServerLabel),
		Status:           strings.TrimSpace(status),
		Arguments:        cloneJSONMessage(call.Arguments),
		Output:           marshalStreamPayloadValue(payload),
	}
	if text := strings.TrimSpace(message); text != "" {
		body.Error = text
	}
	return body
}

func reasoningStreamItems(items []llm.ModelItem) []json.RawMessage {
	if len(items) == 0 {
		return nil
	}

	reasoningItems := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		if item.Type != llm.ModelItemReasoning {
			continue
		}

		raw := item.Raw
		if len(raw) == 0 {
			marshaled, err := json.Marshal(item)
			if err != nil {
				continue
			}
			raw = marshaled
		}

		redacted, err := redactEncryptedJSONPayload(raw)
		if err != nil || len(redacted) == 0 {
			continue
		}
		reasoningItems = append(reasoningItems, redacted)
	}

	return reasoningItems
}

func marshalStreamPayloadValue(payload []byte) json.RawMessage {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return nil
	}
	if json.Valid([]byte(trimmed)) {
		redacted, err := redactEncryptedJSONPayload([]byte(trimmed))
		if err == nil {
			return redacted
		}
	}
	wrapped, err := json.Marshal(trimmed)
	if err != nil {
		return nil
	}
	return wrapped
}

func redactEncryptedJSONPayload(data []byte) (json.RawMessage, error) {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil, nil
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}

	redacted, err := json.Marshal(redactEncryptedJSONValue(value))
	if err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), redacted...), nil
}

func redactEncryptedJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, item := range typed {
			if key == "encrypted_content" {
				continue
			}
			redacted[key] = redactEncryptedJSONValue(item)
		}
		return redacted
	case []any:
		redacted := make([]any, 0, len(typed))
		for _, item := range typed {
			redacted = append(redacted, redactEncryptedJSONValue(item))
		}
		return redacted
	default:
		return value
	}
}

func cloneJSONMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func recordIDFromRun(run *domain.TurnRun) string {
	if run == nil {
		return ""
	}
	return strings.TrimSpace(run.ID)
}

func marshalToolStreamPayload(payload stream.ToolStreamPayload) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(raw)
}

func stableToolType(record *domain.ToolCallRecord, call tool.ToolCall) string {
	if record != nil && strings.TrimSpace(record.ToolType) != "" {
		return strings.TrimSpace(record.ToolType)
	}
	switch strings.TrimSpace(call.Type) {
	case llm.ModelItemFunctionCall:
		return llm.ModelToolTypeFunction
	case llm.ModelItemMCPCall, llm.ModelItemMCPApprovalRequest:
		return llm.ModelToolTypeMCP
	default:
		return strings.TrimSpace(call.Type)
	}
}

func stableToolNamespace(record *domain.ToolCallRecord, call tool.ToolCall) string {
	if record != nil && strings.TrimSpace(record.Namespace) != "" {
		return strings.TrimSpace(record.Namespace)
	}
	if strings.TrimSpace(call.Namespace) != "" {
		return strings.TrimSpace(call.Namespace)
	}
	return ""
}
