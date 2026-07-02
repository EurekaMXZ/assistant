package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(encodedKey string) (*Cipher, error) {
	encodedKey = strings.TrimSpace(encodedKey)
	if encodedKey == "" {
		return nil, errors.New("provider credential master key is required")
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("decode provider credential master key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("provider credential master key must decode to 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create provider credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create provider credential gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(credentialID string, provider string, plaintext string) ([]byte, []byte, error) {
	if c == nil || c.aead == nil {
		return nil, nil, errors.New("provider credential cipher is not configured")
	}
	if strings.TrimSpace(plaintext) == "" {
		return nil, nil, errors.New("provider api key is required")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate provider credential nonce: %w", err)
	}
	ciphertext := c.aead.Seal(nil, nonce, []byte(plaintext), associatedData(credentialID, provider))
	return ciphertext, nonce, nil
}

func (c *Cipher) Decrypt(credentialID string, provider string, ciphertext []byte, nonce []byte) (string, error) {
	if c == nil || c.aead == nil {
		return "", errors.New("provider credential cipher is not configured")
	}
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, associatedData(credentialID, provider))
	if err != nil {
		return "", errors.New("decrypt provider credential")
	}
	return string(plaintext), nil
}

func KeyHint(apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	if len(apiKey) <= 4 {
		return strings.Repeat("*", len(apiKey))
	}
	return "..." + apiKey[len(apiKey)-4:]
}

func associatedData(credentialID string, provider string) []byte {
	return []byte(strings.TrimSpace(provider) + ":" + strings.TrimSpace(credentialID))
}
