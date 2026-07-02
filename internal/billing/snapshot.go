package billing

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/EurekaMXZ/assistant/internal/llm"
)

type snapshotPricing struct {
	Currency                          string `json:"currency"`
	InputPerMillionNanos              int64  `json:"input_per_million_nanos"`
	CacheReadInputPerMillionNanos     int64  `json:"cache_read_input_per_million_nanos"`
	CacheCreationInputPerMillionNanos int64  `json:"cache_creation_input_per_million_nanos"`
	OutputPerMillionNanos             int64  `json:"output_per_million_nanos"`
	ImageInputPerMillionNanos         *int64 `json:"image_input_per_million_nanos,omitempty"`
	ImageOutputPerImageNanos          *int64 `json:"image_output_per_image_nanos,omitempty"`
}

func QuoteSnapshot(raw json.RawMessage, usage llm.ModelUsage) (*Charge, error) {
	if len(raw) == 0 || string(raw) == "{}" {
		return nil, errors.New("model pricing snapshot is required")
	}
	pricing, err := decodeSnapshotPricing(raw)
	if err != nil {
		return nil, err
	}
	billableInput := usage.InputTokens - usage.CacheReadInputTokens - usage.CacheCreationInputTokens
	if billableInput < 0 || usage.InputTokens < 0 || usage.CacheReadInputTokens < 0 || usage.CacheCreationInputTokens < 0 || usage.OutputTokens < 0 || usage.ReasoningOutputTokens < 0 {
		return nil, errors.New("invalid provider token usage")
	}
	input, err := priceTokensChecked(billableInput, pricing.InputPerMillionNanos)
	if err != nil {
		return nil, err
	}
	cacheRead, err := priceTokensChecked(usage.CacheReadInputTokens, pricing.CacheReadInputPerMillionNanos)
	if err != nil {
		return nil, err
	}
	cacheCreation, err := priceTokensChecked(usage.CacheCreationInputTokens, pricing.CacheCreationInputPerMillionNanos)
	if err != nil {
		return nil, err
	}
	output, err := priceTokensChecked(usage.OutputTokens, pricing.OutputPerMillionNanos)
	if err != nil {
		return nil, err
	}
	if input > math.MaxInt64-cacheRead || input+cacheRead > math.MaxInt64-cacheCreation || input+cacheRead+cacheCreation > math.MaxInt64-output {
		return nil, errors.New("model charge overflow")
	}
	return &Charge{
		Currency: pricing.Currency, AmountNanos: input + cacheRead + cacheCreation + output,
		PricingJSON: raw,
	}, nil
}

func MaximumSnapshotCharge(raw json.RawMessage, contextWindowTokens int, maxOutputTokens int) (string, int64, error) {
	if len(raw) == 0 || string(raw) == "{}" {
		return "", 0, errors.New("model pricing snapshot is required")
	}
	pricing, err := decodeSnapshotPricing(raw)
	if err != nil {
		return "", 0, err
	}
	if contextWindowTokens <= 0 || maxOutputTokens <= 0 || maxOutputTokens > contextWindowTokens {
		return "", 0, errors.New("invalid model token limits")
	}
	inputRate := max(pricing.InputPerMillionNanos, pricing.CacheReadInputPerMillionNanos, pricing.CacheCreationInputPerMillionNanos)
	outputRate := pricing.OutputPerMillionNanos
	input, err := priceTokensChecked(contextWindowTokens, inputRate)
	if err != nil {
		return "", 0, err
	}
	output, err := priceTokensChecked(maxOutputTokens, outputRate)
	if err != nil {
		return "", 0, err
	}
	if input > math.MaxInt64-output {
		return "", 0, errors.New("model charge overflow")
	}
	return pricing.Currency, input + output, nil
}

func decodeSnapshotPricing(raw json.RawMessage) (snapshotPricing, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return snapshotPricing{}, errors.New("invalid model pricing snapshot")
	}
	allowed := map[string]bool{
		"currency": true, "input_per_million_nanos": true,
		"cache_read_input_per_million_nanos": true, "cache_creation_input_per_million_nanos": true,
		"output_per_million_nanos": true, "image_input_per_million_nanos": true,
		"image_output_per_image_nanos": true,
	}
	for field := range fields {
		if !allowed[field] {
			return snapshotPricing{}, fmt.Errorf("invalid model pricing snapshot field %q", field)
		}
	}
	for _, field := range []string{"currency", "input_per_million_nanos", "cache_read_input_per_million_nanos", "cache_creation_input_per_million_nanos", "output_per_million_nanos"} {
		value, ok := fields[field]
		if !ok {
			return snapshotPricing{}, fmt.Errorf("model pricing snapshot is missing %q", field)
		}
		if string(value) == "null" {
			return snapshotPricing{}, fmt.Errorf("model pricing snapshot field %q cannot be null", field)
		}
	}
	var pricing snapshotPricing
	if err := json.Unmarshal(raw, &pricing); err != nil || pricing.Currency == "" {
		return snapshotPricing{}, errors.New("invalid model pricing snapshot")
	}
	rates := []int64{pricing.InputPerMillionNanos, pricing.CacheReadInputPerMillionNanos, pricing.CacheCreationInputPerMillionNanos, pricing.OutputPerMillionNanos}
	if pricing.ImageInputPerMillionNanos != nil {
		rates = append(rates, *pricing.ImageInputPerMillionNanos)
	}
	if pricing.ImageOutputPerImageNanos != nil {
		rates = append(rates, *pricing.ImageOutputPerImageNanos)
	}
	for _, rate := range rates {
		if rate < 0 {
			return snapshotPricing{}, errors.New("model pricing snapshot rates must be non-negative")
		}
	}
	return pricing, nil
}

func priceTokensChecked(tokens int, rate int64) (int64, error) {
	if tokens < 0 || rate < 0 {
		return 0, errors.New("negative tokens or rate")
	}
	if tokens == 0 || rate == 0 {
		return 0, nil
	}
	if int64(tokens) > math.MaxInt64/rate {
		return 0, errors.New("model charge overflow")
	}
	return int64(tokens) * rate / tokensPerMillion, nil
}
