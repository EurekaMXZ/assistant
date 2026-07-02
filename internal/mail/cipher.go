package mail

import (
	"errors"

	"github.com/EurekaMXZ/assistant/internal/credential"
)

const (
	smtpCredentialID = "smtp-settings"
	smtpPurpose      = "smtp-password"
)

type PasswordCipher struct {
	cipher *credential.Cipher
}

func NewPasswordCipher(cipher *credential.Cipher) *PasswordCipher {
	return &PasswordCipher{cipher: cipher}
}

func (c *PasswordCipher) Encrypt(password string) ([]byte, []byte, error) {
	if c == nil || c.cipher == nil {
		return nil, nil, errors.New("SMTP password cipher is not configured")
	}
	return c.cipher.Encrypt(smtpCredentialID, smtpPurpose, password)
}

func (c *PasswordCipher) Decrypt(ciphertext []byte, nonce []byte) (string, error) {
	if c == nil || c.cipher == nil {
		return "", errors.New("SMTP password cipher is not configured")
	}
	return c.cipher.Decrypt(smtpCredentialID, smtpPurpose, ciphertext, nonce)
}
