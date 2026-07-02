package credential

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCipherRoundTripAndAssociatedData(t *testing.T) {
	masterKey := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	cipher, err := NewCipher(masterKey)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}

	ciphertext, nonce, err := cipher.Encrypt("credential-1", "openai", "sk-sensitive")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if strings.Contains(string(ciphertext), "sk-sensitive") {
		t.Fatal("ciphertext contains plaintext key")
	}
	plaintext, err := cipher.Decrypt("credential-1", "openai", ciphertext, nonce)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plaintext != "sk-sensitive" {
		t.Fatalf("plaintext = %q", plaintext)
	}
	if _, err := cipher.Decrypt("credential-2", "openai", ciphertext, nonce); err == nil {
		t.Fatal("decrypt with a different credential id succeeded")
	}
	if _, err := cipher.Decrypt("credential-1", "other", ciphertext, nonce); err == nil {
		t.Fatal("decrypt with a different provider succeeded")
	}
}

func TestNewCipherValidatesMasterKey(t *testing.T) {
	for _, key := range []string{"", "not-base64", base64.StdEncoding.EncodeToString([]byte("too-short"))} {
		if _, err := NewCipher(key); err == nil {
			t.Fatalf("NewCipher(%q) succeeded", key)
		}
	}
}

func TestKeyHint(t *testing.T) {
	if got := KeyHint("sk-123456"); got != "...3456" {
		t.Fatalf("KeyHint = %q", got)
	}
	if got := KeyHint("abcd"); got != "****" {
		t.Fatalf("short KeyHint = %q", got)
	}
}
