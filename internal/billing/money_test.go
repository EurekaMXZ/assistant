package billing

import (
	"math"
	"testing"
)

func TestParseAndFormatAmount(t *testing.T) {
	tests := []struct {
		input string
		nanos int64
		want  string
	}{
		{input: "1", nanos: 1_000_000_000, want: "1.00"},
		{input: "0.000000001", nanos: 1, want: "0.000000001"},
		{input: "12.3400", nanos: 12_340_000_000, want: "12.34"},
	}
	for _, test := range tests {
		nanos, err := ParseAmount(test.input)
		if err != nil {
			t.Fatalf("ParseAmount(%q): %v", test.input, err)
		}
		if nanos != test.nanos || FormatAmount(nanos) != test.want {
			t.Fatalf("ParseAmount(%q) = %d, format = %q", test.input, nanos, FormatAmount(nanos))
		}
	}
}

func TestParseAmountRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "+1", ".1", "1.", "1.0000000001", "abc", "9223372037"} {
		if _, err := ParseAmount(value); err == nil {
			t.Fatalf("ParseAmount(%q) succeeded", value)
		}
	}
}

func TestFormatAmountHandlesMinimumInt64(t *testing.T) {
	if got := FormatAmount(math.MinInt64); got != "-9223372036.854775808" {
		t.Fatalf("FormatAmount(MinInt64) = %q", got)
	}
}
