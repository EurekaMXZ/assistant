package postgres

import (
	"encoding/json"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestPrepareAssistantTextNormalizesBlank(t *testing.T) {
	text, tokens := prepareAssistantText("   \n\t  ")
	if text != " " {
		t.Fatalf("normalized text = %q, want single space", text)
	}
	if tokens != domain.EstimateTokens(" ") {
		t.Fatalf("tokens = %d, want %d", tokens, domain.EstimateTokens(" "))
	}
}

func TestBuildTurnRunMetadataMergesRunSummary(t *testing.T) {
	existing := json.RawMessage(`{"foo":"bar","run":{"model":"old"}}`)
	summary := domain.TurnRunSummary{
		Model:        "gpt-test",
		InputTokens:  11,
		OutputTokens: 13,
		TotalTokens:  24,
	}

	merged, err := buildTurnRunMetadata(existing, summary)
	if err != nil {
		t.Fatalf("buildTurnRunMetadata: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(merged, &decoded); err != nil {
		t.Fatalf("unmarshal merged metadata: %v", err)
	}

	if decoded["foo"] != "bar" {
		t.Fatalf("expected existing metadata to survive merge")
	}

	run, ok := decoded["run"].(map[string]any)
	if !ok {
		t.Fatalf("expected run metadata map, got %T", decoded["run"])
	}
	if run["model"] != summary.Model {
		t.Fatalf("run.model = %v, want %s", run["model"], summary.Model)
	}
	if int(run["total_tokens"].(float64)) != summary.TotalTokens {
		t.Fatalf("run.total_tokens = %v, want %d", run["total_tokens"], summary.TotalTokens)
	}
}

func TestShouldRequestCompaction(t *testing.T) {
	head := &domain.ContextHead{
		RawTailStartSeq:     21,
		LastSeq:             25,
		ActiveContextTokens: 901,
	}

	if !shouldRequestCompaction(head, 900) {
		t.Fatal("expected compaction request when threshold is exceeded and raw tail exists")
	}
	head.ActiveContextTokens = 900
	if !shouldRequestCompaction(head, 900) {
		t.Fatal("expected compaction request when threshold is reached")
	}

	head.RawTailStartSeq = 26
	if shouldRequestCompaction(head, 900) {
		t.Fatal("expected no compaction request when raw tail is empty")
	}
}

func TestActiveContextTokensAfterTurnUsesProviderTotal(t *testing.T) {
	head := &domain.ContextHead{ActiveContextTokens: 125}
	turn := &domain.Turn{}
	summary := domain.TurnRunSummary{InputTokens: 800, OutputTokens: 100, TotalTokens: 900}

	if got := activeContextTokensAfterTurn(head, turn, summary, 25, 0, 0); got != 900 {
		t.Fatalf("active tokens = %d, want provider total 900", got)
	}
}

func TestActiveContextTokensAfterTurnFallsBackToLocalEstimate(t *testing.T) {
	head := &domain.ContextHead{ActiveContextTokens: 125}

	if got := activeContextTokensAfterTurn(head, &domain.Turn{}, domain.TurnRunSummary{}, 25, 0, 0); got != 150 {
		t.Fatalf("active tokens = %d, want local fallback 150", got)
	}
}

func TestEstimateUserMessageTokensIncludesAttachments(t *testing.T) {
	metadata := json.RawMessage(`{"attachments":[{"category":"image"},{"category":"document"}]}`)
	want := domain.EstimateTokens("hello") + 2_000 + 64
	if got := estimateUserMessageTokens("hello", metadata); got != want {
		t.Fatalf("message tokens = %d, want %d", got, want)
	}
}
