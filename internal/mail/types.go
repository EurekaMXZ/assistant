package mail

import (
	"net"
	stdmail "net/mail"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

const (
	SecurityStartTLS = "starttls"
	SecurityTLS      = "tls"
	SecurityNone     = "none"
)

type Settings struct {
	Enabled            bool      `json:"enabled"`
	Host               string    `json:"host"`
	Port               int       `json:"port"`
	Security           string    `json:"security"`
	Username           string    `json:"username"`
	FromEmail          string    `json:"from_email"`
	FromName           string    `json:"from_name"`
	PasswordConfigured bool      `json:"password_configured"`
	UpdatedByUserID    string    `json:"updated_by,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type StoredSettings struct {
	Settings
	EncryptedPassword []byte
	PasswordNonce     []byte
	KeyVersion        int
}

type UpdateSettingsInput struct {
	Enabled   *bool
	Host      *string
	Port      *int
	Security  *string
	Username  *string
	Password  *string
	FromEmail *string
	FromName  *string
}

type UpdateSettingsParams struct {
	Settings
	EncryptedPassword []byte
	PasswordNonce     []byte
	KeyVersion        int
	UpdatedByUserID   string
}

func ValidateSettings(settings Settings) error {
	settings.Host = strings.TrimSpace(settings.Host)
	settings.Username = strings.TrimSpace(settings.Username)
	settings.FromEmail = strings.TrimSpace(settings.FromEmail)
	settings.FromName = strings.TrimSpace(settings.FromName)
	if settings.Port < 1 || settings.Port > 65535 {
		return domain.NewValidationError("SMTP port must be between 1 and 65535")
	}
	switch settings.Security {
	case SecurityStartTLS, SecurityTLS, SecurityNone:
	default:
		return domain.NewValidationError("SMTP security must be starttls, tls, or none")
	}
	for _, value := range []string{settings.Host, settings.Username, settings.FromEmail, settings.FromName} {
		if strings.ContainsAny(value, "\r\n") {
			return domain.NewValidationError("SMTP settings contain invalid header characters")
		}
	}
	if settings.Enabled && settings.Host == "" {
		return domain.NewValidationError("SMTP host is required when mail is enabled")
	}
	if strings.ContainsAny(settings.Host, " \t") || strings.Contains(settings.Host, ":") {
		return domain.NewValidationError("SMTP host is invalid")
	}
	if settings.Enabled || settings.FromEmail != "" {
		address, err := stdmail.ParseAddress(settings.FromEmail)
		if err != nil || !strings.EqualFold(address.Address, settings.FromEmail) {
			return domain.NewValidationError("SMTP from_email is invalid")
		}
	}
	if net.ParseIP(settings.Host) == nil && len(settings.Host) > 253 {
		return domain.NewValidationError("SMTP host is invalid")
	}
	return nil
}

func ValidateRecipient(recipient string) error {
	recipient = strings.TrimSpace(recipient)
	if strings.ContainsAny(recipient, "\r\n") {
		return domain.NewValidationError("mail recipient is invalid")
	}
	address, err := stdmail.ParseAddress(recipient)
	if err != nil || !strings.EqualFold(address.Address, recipient) {
		return domain.NewValidationError("mail recipient is invalid")
	}
	return nil
}

func requireSystemActor(actor *domain.User) error {
	if actor == nil {
		return domain.NewUnauthorizedError("authentication required")
	}
	if actor.Role != domain.UserRoleSystem {
		return domain.NewForbiddenError("system privileges are required")
	}
	return nil
}
