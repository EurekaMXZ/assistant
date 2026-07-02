package billing

import (
	"encoding/json"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/llm"
)

func TestMaximumSnapshotChargeUsesHighestApplicableRates(t *testing.T) {
	currency, amount, err := MaximumSnapshotCharge(json.RawMessage(`{
		"currency":"USD",
		"input_per_million_nanos":1000000000,
		"cache_read_input_per_million_nanos":2000000000,
		"cache_creation_input_per_million_nanos":5000000000,
		"output_per_million_nanos":3000000000
	}`), 100_000, 10_000)
	if err != nil {
		t.Fatalf("maximum charge: %v", err)
	}
	if currency != "USD" || amount != 530_000_000 {
		t.Fatalf("maximum charge = currency %q amount %d", currency, amount)
	}
}

func TestQuoteSnapshotPricesFourTokenBuckets(t *testing.T) {
	charge, err := QuoteSnapshot(json.RawMessage(`{
		"currency":"USD",
		"input_per_million_nanos":1000000000,
		"cache_read_input_per_million_nanos":200000000,
		"cache_creation_input_per_million_nanos":1500000000,
		"output_per_million_nanos":8000000000
	}`), llm.ModelUsage{
		InputTokens: 1000, CacheReadInputTokens: 200, CacheCreationInputTokens: 100,
		OutputTokens: 400, ReasoningOutputTokens: 150, TotalTokens: 1400,
	})
	if err != nil {
		t.Fatalf("quote snapshot: %v", err)
	}
	if charge.AmountNanos != 4_090_000 {
		t.Fatalf("amount_nanos = %d, want %d", charge.AmountNanos, 4_090_000)
	}
}

func TestQuoteSnapshotRejectsNestedPricing(t *testing.T) {
	_, err := QuoteSnapshot(json.RawMessage(`{
		"currency":"USD",
		"rates":{"input_per_million_nanos":1000,"output_per_million_nanos":2000}
	}`), llm.ModelUsage{})
	if err == nil {
		t.Fatal("nested snapshot was accepted")
	}
}
