package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

const (
	ActionPurposeEmailVerification = "email_verification"
	ActionPurposePasswordReset     = "password_reset"
	actionTokenBytes               = 32
)

func GenerateActionToken() (string, []byte, error) {
	raw := make([]byte, actionTokenBytes)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}

func HashActionToken(token string) ([]byte, error) {
	token = strings.TrimSpace(token)
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != actionTokenBytes {
		return nil, errors.New("invalid action token")
	}
	hash := sha256.Sum256([]byte(token))
	return hash[:], nil
}
