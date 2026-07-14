package billing

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenerateAndHashRedemptionCode(t *testing.T) {
	code, generatedHash, hint, err := GenerateRedemptionCode()
	if err != nil {
		t.Fatalf("generate redemption code: %v", err)
	}
	if len(code) != 48 || code != strings.ToLower(code) || strings.Contains(hint, code) {
		t.Fatalf("unexpected code=%q hint=%q", code, hint)
	}
	hash, err := HashRedemptionCode("  " + code + "  ")
	if err != nil || hash != generatedHash {
		t.Fatalf("hash generated code: hash=%x generated=%x err=%v", hash, generatedHash, err)
	}
}

func TestHashRedemptionCodeRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"", "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789", "1234", "other"} {
		if _, err := HashRedemptionCode(value); err == nil {
			t.Fatalf("HashRedemptionCode(%q) succeeded", value)
		}
	}
}

func TestHashRedemptionCodeAcceptsLegacyIssuedCode(t *testing.T) {
	legacy := "ASST-" + base64.RawURLEncoding.EncodeToString(make([]byte, redemptionCodeBytes))
	if _, err := HashRedemptionCode(legacy); err != nil {
		t.Fatalf("hash legacy redemption code: %v", err)
	}
}
