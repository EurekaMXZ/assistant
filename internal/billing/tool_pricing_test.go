package billing

import (
	"encoding/json"
	"math"
	"testing"
)

func TestQuoteToolUsage(t *testing.T) {
	quote, err := QuoteToolUsage("USD", map[string]ToolRate{
		"sandbox.create": {PricePerCallNanos: 250_000_000, Enabled: true, Version: 2},
		"tavily.search":  {PricePerCallNanos: 10_000_000, Enabled: false, Version: 1},
	}, map[string]int{"sandbox.create": 2, "tavily.search": 3})
	if err != nil {
		t.Fatalf("quote tool usage: %v", err)
	}
	if quote.AmountNanos != 500_000_000 {
		t.Fatalf("amount = %d, want 500000000", quote.AmountNanos)
	}
	var usage map[string]int
	if err := json.Unmarshal(quote.UsageJSON, &usage); err != nil || usage["tavily.search"] != 3 {
		t.Fatalf("usage = %#v, err=%v", usage, err)
	}
	var pricing struct {
		Currency string              `json:"currency"`
		Tools    map[string]ToolRate `json:"tools"`
	}
	if err := json.Unmarshal(quote.PricingJSON, &pricing); err != nil || pricing.Currency != "USD" || pricing.Tools["sandbox.create"].Version != 2 {
		t.Fatalf("pricing = %#v, err=%v", pricing, err)
	}
}

func TestQuoteToolUsageRejectsOverflow(t *testing.T) {
	_, err := QuoteToolUsage("USD", map[string]ToolRate{
		"image_generation": {PricePerCallNanos: math.MaxInt64, Enabled: true, Version: 1},
	}, map[string]int{"image_generation": 2})
	if err == nil {
		t.Fatal("expected tool charge overflow")
	}
}

func TestQuoteToolUsageRejectsEnabledZeroPrice(t *testing.T) {
	_, err := QuoteToolUsage("USD", map[string]ToolRate{
		"sandbox.create": {Enabled: true, Version: 1},
	}, map[string]int{"sandbox.create": 1})
	if err == nil {
		t.Fatal("expected enabled zero price to fail")
	}
}

func TestAddToolCharge(t *testing.T) {
	combined, err := AddToolCharge(
		&Charge{Currency: "USD", AmountNanos: 100, PricingJSON: json.RawMessage(`{"currency":"USD"}`)},
		&ToolCharge{AmountNanos: 25},
	)
	if err != nil || combined.AmountNanos != 125 || combined.Currency != "USD" {
		t.Fatalf("combined = %#v, err=%v", combined, err)
	}
}
