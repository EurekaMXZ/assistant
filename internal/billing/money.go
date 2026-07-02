package billing

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const nanosPerUnit int64 = 1_000_000_000

func ParseAmount(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") {
		return 0, errors.New("amount must be a positive decimal")
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, errors.New("amount must be a positive decimal")
	}
	units, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, errors.New("amount must be a positive decimal")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if fraction == "" || len(fraction) > 9 {
			return 0, errors.New("amount supports at most 9 decimal places")
		}
	}
	for len(fraction) < 9 {
		fraction += "0"
	}
	fractionNanos := int64(0)
	if fraction != "" {
		fractionNanos, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, errors.New("amount must be a positive decimal")
		}
	}
	if units > (math.MaxInt64-fractionNanos)/nanosPerUnit {
		return 0, errors.New("amount is too large")
	}
	nanos := units*nanosPerUnit + fractionNanos
	if nanos <= 0 {
		return 0, errors.New("amount must be greater than zero")
	}
	return nanos, nil
}

func FormatAmount(nanos int64) string {
	negative := nanos < 0
	magnitude := uint64(nanos)
	if negative {
		magnitude = uint64(-(nanos + 1)) + 1
	}
	value := fmt.Sprintf("%d.%09d", magnitude/uint64(nanosPerUnit), magnitude%uint64(nanosPerUnit))
	value = strings.TrimRight(value, "0")
	value = strings.TrimRight(value, ".")
	if !strings.Contains(value, ".") {
		value += ".00"
	} else if len(value)-strings.IndexByte(value, '.')-1 == 1 {
		value += "0"
	}
	if negative {
		return "-" + value
	}
	return value
}
