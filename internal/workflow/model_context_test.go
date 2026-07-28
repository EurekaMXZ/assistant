package workflow

import (
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/llm"
)

func TestCompactTriggerTokenLimitUsesEightyPercentWindow(t *testing.T) {
	if got := compactTriggerTokenLimit(0, 372_000); got != 297_600 {
		t.Fatalf("automatic limit = %d, want 297600", got)
	}
	if got := compactTriggerTokenLimit(280_000, 372_000); got != 280_000 {
		t.Fatalf("configured limit = %d, want 280000", got)
	}
	if got := compactTriggerTokenLimit(360_000, 372_000); got != 297_600 {
		t.Fatalf("clamped limit = %d, want 297600", got)
	}
	if got := compactTriggerTokenLimit(compactTriggerTokenLimit(0, 372_000), 128_000); got != 102_400 {
		t.Fatalf("compaction-model limit = %d, want 102400", got)
	}
}

func TestEstimateModelContextTokensIncludesInstructionsItemsAndTools(t *testing.T) {
	withoutTools := estimateModelContextTokens("system", []llm.ModelItem{{Type: llm.ModelItemMessage, Content: "hello"}}, nil)
	withTools := estimateModelContextTokens("system", []llm.ModelItem{{Type: llm.ModelItemMessage, Content: "hello"}}, []llm.ModelTool{{Type: llm.ModelToolTypeFunction, Name: "lookup", Description: strings.Repeat("d", 80)}})

	if withoutTools <= 0 || withTools <= withoutTools {
		t.Fatalf("unexpected estimates: without_tools=%d with_tools=%d", withoutTools, withTools)
	}
}

func TestEstimateModelContextTokensUsesFixedImageEstimate(t *testing.T) {
	raw := []byte(`{"type":"message","role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,` + strings.Repeat("a", 100_000) + `"}]}`)
	tokens := estimateModelContextTokens("", []llm.ModelItem{{Type: llm.ModelItemMessage, Raw: raw}}, nil)
	if tokens < 2_000 || tokens > 2_100 {
		t.Fatalf("image estimate = %d, want fixed estimate near 2000", tokens)
	}

	referencedTokens := estimateModelContextTokens("", []llm.ModelItem{{
		Type: llm.ModelItemImageGenerationCall,
		Raw:  []byte(`{"type":"image_generation_call","result_ref":{"object_key":"generated.png"}}`),
	}}, nil)
	if referencedTokens < 2_000 || referencedTokens > 2_100 {
		t.Fatalf("generated image estimate = %d, want fixed estimate near 2000", referencedTokens)
	}
}

func TestMarshalModelContextPreservesExternalizedGeneratedImageReference(t *testing.T) {
	data, err := marshalModelContextItems([]llm.ModelItem{{
		ID: "image-1", Type: llm.ModelItemImageGenerationCall, Result: "image-base64",
		Raw: []byte(`{"type":"image_generation_call","result_ref":{"object_key":"generated.png"}}`),
	}})
	if err != nil {
		t.Fatalf("marshal model context: %v", err)
	}
	if !strings.Contains(string(data), `"result_ref"`) || strings.Contains(string(data), "image-base64") {
		t.Fatalf("serialized image context = %s", data)
	}
}
