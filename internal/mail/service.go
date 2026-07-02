package mail

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type SettingsStore interface {
	GetSMTPSettings(ctx context.Context) (*StoredSettings, error)
	UpdateSMTPSettings(ctx context.Context, params UpdateSettingsParams) (*StoredSettings, error)
}

type Service struct {
	Settings  SettingsStore
	Cipher    *PasswordCipher
	Sender    Sender
	PublicURL string
}

func (s *Service) GetSettings(ctx context.Context, actor *domain.User) (*Settings, error) {
	if err := requireSystemActor(actor); err != nil {
		return nil, err
	}
	stored, err := s.getStored(ctx)
	if err != nil {
		return nil, err
	}
	settings := stored.Settings
	return &settings, nil
}

func (s *Service) UpdateSettings(ctx context.Context, actor *domain.User, input UpdateSettingsInput) (*Settings, error) {
	if err := requireSystemActor(actor); err != nil {
		return nil, err
	}
	stored, err := s.getStored(ctx)
	if err != nil {
		return nil, err
	}
	updated := stored.Settings
	if input.Enabled != nil {
		updated.Enabled = *input.Enabled
	}
	if input.Host != nil {
		updated.Host = strings.TrimSpace(*input.Host)
	}
	if input.Port != nil {
		updated.Port = *input.Port
	}
	if input.Security != nil {
		updated.Security = strings.ToLower(strings.TrimSpace(*input.Security))
	}
	if input.Username != nil {
		updated.Username = strings.TrimSpace(*input.Username)
	}
	if input.FromEmail != nil {
		updated.FromEmail = strings.TrimSpace(*input.FromEmail)
	}
	if input.FromName != nil {
		updated.FromName = strings.TrimSpace(*input.FromName)
	}
	if err := ValidateSettings(updated); err != nil {
		return nil, err
	}
	encrypted := stored.EncryptedPassword
	nonce := stored.PasswordNonce
	keyVersion := stored.KeyVersion
	if input.Password != nil {
		if *input.Password == "" {
			encrypted = nil
			nonce = nil
			updated.PasswordConfigured = false
		} else {
			if s.Cipher == nil {
				return nil, errors.New("SMTP password cipher is not configured")
			}
			encrypted, nonce, err = s.Cipher.Encrypt(*input.Password)
			if err != nil {
				return nil, err
			}
			if stored.PasswordConfigured {
				keyVersion++
			} else {
				keyVersion = 1
			}
			updated.PasswordConfigured = true
		}
	}
	result, err := s.Settings.UpdateSMTPSettings(ctx, UpdateSettingsParams{
		Settings: updated, EncryptedPassword: encrypted, PasswordNonce: nonce,
		KeyVersion: keyVersion, UpdatedByUserID: actor.ID,
	})
	if err != nil {
		return nil, err
	}
	response := result.Settings
	return &response, nil
}

func (s *Service) TestSettings(ctx context.Context, actor *domain.User, recipient string) error {
	if err := requireSystemActor(actor); err != nil {
		return err
	}
	return s.send(ctx, recipient, "Assistant SMTP test", "Your Assistant SMTP settings are working.")
}

func (s *Service) SendVerification(ctx context.Context, recipient string, token string) error {
	link, err := s.actionLink("auth/verify-email", token)
	if err != nil {
		return err
	}
	body := fmt.Sprintf("Verify your email address by opening this link:\n\n%s\n\nThis link expires in 24 hours.", link)
	return s.send(ctx, recipient, "Verify your email address", body)
}

func (s *Service) SendPasswordReset(ctx context.Context, recipient string, token string) error {
	link, err := s.actionLink("auth/reset-password", token)
	if err != nil {
		return err
	}
	body := fmt.Sprintf("Reset your password by opening this link:\n\n%s\n\nThis link expires in 1 hour.", link)
	return s.send(ctx, recipient, "Reset your password", body)
}

func (s *Service) send(ctx context.Context, recipient string, subject string, body string) error {
	if err := ValidateRecipient(recipient); err != nil {
		return err
	}
	stored, err := s.getStored(ctx)
	if err != nil {
		return err
	}
	if !stored.Enabled {
		return errors.New("SMTP mail is disabled")
	}
	if err := ValidateSettings(stored.Settings); err != nil {
		return err
	}
	password := ""
	if stored.PasswordConfigured {
		if s.Cipher == nil {
			return errors.New("SMTP password cipher is not configured")
		}
		password, err = s.Cipher.Decrypt(stored.EncryptedPassword, stored.PasswordNonce)
		if err != nil {
			return err
		}
	}
	if s.Sender == nil {
		return errors.New("SMTP sender is not configured")
	}
	return s.Sender.Send(ctx, SMTPConfig{
		Host: stored.Host, Port: stored.Port, Security: stored.Security,
		Username: stored.Username, Password: password, FromEmail: stored.FromEmail, FromName: stored.FromName,
	}, Message{To: strings.TrimSpace(recipient), Subject: subject, Body: body})
}

func (s *Service) getStored(ctx context.Context) (*StoredSettings, error) {
	if s == nil || s.Settings == nil {
		return nil, errors.New("SMTP settings are not configured")
	}
	return s.Settings.GetSMTPSettings(ctx)
}

func (s *Service) actionLink(path string, token string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(s.PublicURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", errors.New("public web origin is invalid")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + path
	query := base.Query()
	query.Set("token", token)
	base.RawQuery = query.Encode()
	return base.String(), nil
}
