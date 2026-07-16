package workflow

import (
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/llm"
)

func TestCompactTriggerTokenLimitUsesNinetyPercentWindow(t *testing.T) {
	if got := compactTriggerTokenLimit(0, 372_000); got != 334_800 {
		t.Fatalf("automatic limit = %d, want 334800", got)
	}
	if got := compactTriggerTokenLimit(320_000, 372_000); got != 320_000 {
		t.Fatalf("configured limit = %d, want 320000", got)
	}
	if got := compactTriggerTokenLimit(360_000, 372_000); got != 334_800 {
		t.Fatalf("clamped limit = %d, want 334800", got)
	}
	if got := compactTriggerTokenLimit(compactTriggerTokenLimit(0, 372_000), 128_000); got != 115_200 {
		t.Fatalf("compaction-model limit = %d, want 115200", got)
	}
}

func TestTruncateModelContextItemBoundsFunctionOutput(t *testing.T) {
	output := "prefix-" + strings.Repeat("x", 1_000) + "-suffix"
	item := truncateModelContextItem(llm.ModelItem{
		Type:   llm.ModelItemFunctionCallOutput,
		Output: output,
		Raw:    []byte(`{"output":"full"}`),
	}, 50)

	if !strings.Contains(item.Output, "Warning: truncated output") || !strings.Contains(item.Output, "tokens truncated") {
		t.Fatalf("missing truncation metadata: %q", item.Output)
	}
	if !strings.Contains(item.Output, "prefix-") || !strings.HasSuffix(item.Output, "-suffix") {
		t.Fatalf("truncation did not retain output boundaries: %q", item.Output)
	}
	if len(item.Raw) != 0 {
		t.Fatal("truncated item retained raw unbounded output")
	}
	if len(item.Output) > 200 {
		t.Fatalf("truncated output bytes = %d, want at most 200", len(item.Output))
	}
}

func TestEstimateModelContextTokensIncludesInstructionsItemsAndTools(t *testing.T) {
	withoutTools := estimateModelContextTokens("system", []llm.ModelItem{{Type: llm.ModelItemMessage, Content: "hello"}}, nil)
	withTools := estimateModelContextTokens("system", []llm.ModelItem{{Type: llm.ModelItemMessage, Content: "hello"}}, []llm.ModelTool{{Type: llm.ModelToolTypeFunction, Name: "lookup", Description: strings.Repeat("d", 80)}})

	if withoutTools <= 0 || withTools <= withoutTools {
		t.Fatalf("unexpected estimates: without_tools=%d with_tools=%d", withoutTools, withTools)
	}
}

func TestRemainingToolOutputTokensUsesNinetyFivePercentWindow(t *testing.T) {
	request := llm.ModelRequest{ContextWindowTokens: 1_000}
	if got := remainingToolOutputTokens(request, nil, 900); got != 50 {
		t.Fatalf("remaining tool output tokens = %d, want 50", got)
	}
	if got := remainingToolOutputTokens(request, nil, 960); got != 0 {
		t.Fatalf("remaining tool output tokens = %d, want 0", got)
	}
}

func TestEstimateModelContextTokensUsesFixedImageEstimate(t *testing.T) {
	raw := []byte(`{"type":"message","role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,` + strings.Repeat("a", 100_000) + `"}]}`)
	tokens := estimateModelContextTokens("", []llm.ModelItem{{Type: llm.ModelItemMessage, Raw: raw}}, nil)
	if tokens < 2_000 || tokens > 2_100 {
		t.Fatalf("image estimate = %d, want fixed estimate near 2000", tokens)
	}
}

func TestTinyToolOutputBudgetIsEnforced(t *testing.T) {
	item := truncateModelContextItem(llm.ModelItem{Type: llm.ModelItemFunctionCallOutput, Output: strings.Repeat("x", 100)}, 1)
	if len(item.Output) > 4 {
		t.Fatalf("one-token output budget produced %d bytes", len(item.Output))
	}
}
