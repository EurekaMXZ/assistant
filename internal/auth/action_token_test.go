package auth

import (
	"bytes"
	"testing"
)

func TestGenerateActionTokenStoresOnlyStableSHA256Hash(t *testing.T) {
	token, hash, err := GenerateActionToken()
	if err != nil {
		t.Fatalf("generate action token: %v", err)
	}
	if token == "" || len(hash) != 32 {
		t.Fatalf("unexpected token/hash lengths: %d/%d", len(token), len(hash))
	}
	computed, err := HashActionToken(token)
	if err != nil {
		t.Fatalf("hash action token: %v", err)
	}
	if !bytes.Equal(hash, computed) {
		t.Fatal("generated and computed token hashes differ")
	}
	if bytes.Contains(hash, []byte(token)) {
		t.Fatal("stored hash contains plaintext token")
	}
}

func TestHashActionTokenRejectsMalformedTokens(t *testing.T) {
	for _, token := range []string{"", "not-base64", "c2hvcnQ"} {
		if _, err := HashActionToken(token); err == nil {
			t.Fatalf("HashActionToken(%q) succeeded", token)
		}
	}
}
