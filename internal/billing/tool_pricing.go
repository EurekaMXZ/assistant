package billing

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
)

type ToolRate struct {
	PricePerCallNanos int64 `json:"price_per_call_nanos"`
	Enabled           bool  `json:"enabled"`
	Version           int64 `json:"version"`
}

type ToolCharge struct {
	AmountNanos int64
	PricingJSON json.RawMessage
	UsageJSON   json.RawMessage
}

type toolPricingSnapshot struct {
	Currency string              `json:"currency"`
	Tools    map[string]ToolRate `json:"tools"`
}

func QuoteToolUsage(currency string, rates map[string]ToolRate, usage map[string]int) (*ToolCharge, error) {
	currency = strings.TrimSpace(currency)
	if len(currency) != 3 {
		return nil, errors.New("tool pricing currency is required")
	}

	usedRates := make(map[string]ToolRate)
	normalizedUsage := make(map[string]int)
	var amount int64
	for key, count := range usage {
		key = strings.TrimSpace(key)
		if key == "" || count < 0 {
			return nil, errors.New("invalid tool usage")
		}
		if count == 0 {
			continue
		}
		rate := rates[key]
		if rate.PricePerCallNanos < 0 || rate.Version < 0 {
			return nil, errors.New("invalid tool price")
		}
		if rate.Enabled && rate.PricePerCallNanos == 0 {
			return nil, errors.New("enabled tool price must be positive")
		}
		usedRates[key] = rate
		normalizedUsage[key] = count
		if !rate.Enabled || rate.PricePerCallNanos == 0 {
			continue
		}
		if int64(count) > math.MaxInt64/rate.PricePerCallNanos {
			return nil, errors.New("tool charge overflow")
		}
		lineAmount := int64(count) * rate.PricePerCallNanos
		if amount > math.MaxInt64-lineAmount {
			return nil, errors.New("tool charge overflow")
		}
		amount += lineAmount
	}

	pricingJSON, err := json.Marshal(toolPricingSnapshot{Currency: currency, Tools: usedRates})
	if err != nil {
		return nil, err
	}
	usageJSON, err := json.Marshal(normalizedUsage)
	if err != nil {
		return nil, err
	}
	return &ToolCharge{AmountNanos: amount, PricingJSON: pricingJSON, UsageJSON: usageJSON}, nil
}

func AddToolCharge(modelCharge *Charge, toolCharge *ToolCharge) (*Charge, error) {
	if modelCharge == nil || toolCharge == nil {
		return nil, errors.New("model and tool charges are required")
	}
	if modelCharge.AmountNanos < 0 || toolCharge.AmountNanos < 0 || modelCharge.AmountNanos > math.MaxInt64-toolCharge.AmountNanos {
		return nil, errors.New("usage charge overflow")
	}
	return &Charge{
		Currency: modelCharge.Currency, AmountNanos: modelCharge.AmountNanos + toolCharge.AmountNanos,
		PricingJSON: append(json.RawMessage(nil), modelCharge.PricingJSON...),
	}, nil
}
