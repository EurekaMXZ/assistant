package workflow

import (
	"testing"

	"github.com/EurekaMXZ/assistant/internal/llm"
)

func TestBillableImageGenerationCount(t *testing.T) {
	result := &llm.ModelResult{OutputItems: []llm.ModelItem{
		{Type: llm.ModelItemImageGenerationCall, Result: "image-a"},
		{Type: llm.ModelItemImageGenerationCall, Result: "  "},
		{Type: llm.ModelItemMessage, Result: "not-an-image"},
		{Type: llm.ModelItemImageGenerationCall, Result: "image-b"},
	}}
	if got := billableImageGenerationCount(result); got != 2 {
		t.Fatalf("billable image count = %d, want 2", got)
	}
}
