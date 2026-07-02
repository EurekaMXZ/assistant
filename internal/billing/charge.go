package billing

import "encoding/json"

const tokensPerMillion = int64(1_000_000)

type Charge struct {
	Currency    string
	AmountNanos int64
	PricingJSON json.RawMessage
}
