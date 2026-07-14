package billing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	redemptionCodeBytes        = 24
	legacyRedemptionCodePrefix = "ASST-"
)

func GenerateRedemptionCode() (string, [32]byte, string, error) {
	random := make([]byte, redemptionCodeBytes)
	if _, err := rand.Read(random); err != nil {
		return "", [32]byte{}, "", err
	}
	code := hex.EncodeToString(random)
	hash := sha256.Sum256([]byte(code))
	hint := "***" + code[len(code)-6:]
	return code, hash, hint, nil
}

func HashRedemptionCode(value string) ([32]byte, error) {
	value = strings.TrimSpace(value)
	if len(value) == redemptionCodeBytes*2 && value == strings.ToLower(value) {
		raw, err := hex.DecodeString(value)
		if err == nil && len(raw) == redemptionCodeBytes {
			return sha256.Sum256([]byte(value)), nil
		}
	}
	if strings.HasPrefix(value, legacyRedemptionCodePrefix) {
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, legacyRedemptionCodePrefix))
		if err == nil && len(raw) == redemptionCodeBytes {
			return sha256.Sum256([]byte(value)), nil
		}
	}
	return [32]byte{}, errors.New("invalid redemption code")
}
