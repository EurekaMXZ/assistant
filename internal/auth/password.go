package auth

import (
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

const minPasswordLength = 8

func ValidatePassword(password string) error {
	if len(strings.TrimSpace(password)) < minPasswordLength {
		return domain.NewValidationError("password must be at least 8 characters")
	}
	return nil
}

func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

func ComparePasswordHash(hash string, password string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return domain.NewAuthenticationError("invalid email or password")
	}
	return nil
}

func ValidatePasswordHash(hash string) error {
	if _, err := bcrypt.Cost([]byte(hash)); err != nil {
		return domain.NewValidationError("system user password hash is invalid")
	}
	return nil
}
