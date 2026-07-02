package mail

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/credential"
	"github.com/EurekaMXZ/assistant/internal/domain"
)

type stubSettingsStore struct {
	stored StoredSettings
}

func (s *stubSettingsStore) GetSMTPSettings(context.Context) (*StoredSettings, error) {
	clone := s.stored
	clone.EncryptedPassword = append([]byte(nil), s.stored.EncryptedPassword...)
	clone.PasswordNonce = append([]byte(nil), s.stored.PasswordNonce...)
	return &clone, nil
}

func (s *stubSettingsStore) UpdateSMTPSettings(_ context.Context, params UpdateSettingsParams) (*StoredSettings, error) {
	s.stored = StoredSettings{
		Settings: params.Settings, EncryptedPassword: append([]byte(nil), params.EncryptedPassword...),
		PasswordNonce: append([]byte(nil), params.PasswordNonce...), KeyVersion: params.KeyVersion,
	}
	s.stored.UpdatedByUserID = params.UpdatedByUserID
	s.stored.UpdatedAt = time.Now().UTC()
	s.stored.PasswordConfigured = len(s.stored.EncryptedPassword) > 0
	return s.GetSMTPSettings(context.Background())
}

type stubSender struct {
	config  SMTPConfig
	message Message
}

func (s *stubSender) Send(_ context.Context, config SMTPConfig, message Message) error {
	s.config = config
	s.message = message
	return nil
}

func TestValidateSettingsRejectsHeaderInjectionAndInvalidSecurity(t *testing.T) {
	valid := Settings{Enabled: true, Host: "smtp.example.com", Port: 587, Security: SecurityStartTLS, FromEmail: "noreply@example.com"}
	if err := ValidateSettings(valid); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	injected := valid
	injected.FromName = "Assistant\r\nBcc: attacker@example.com"
	if err := ValidateSettings(injected); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("header injection error = %v", err)
	}
	invalidSecurity := valid
	invalidSecurity.Security = "ssl"
	if err := ValidateSettings(invalidSecurity); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("security error = %v", err)
	}
}

func TestValidateRecipientRejectsHeaderInjection(t *testing.T) {
	if err := ValidateRecipient("recipient@example.com"); err != nil {
		t.Fatalf("valid recipient rejected: %v", err)
	}
	if err := ValidateRecipient("recipient@example.com\r\nBcc: attacker@example.com"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("recipient injection error = %v", err)
	}
}

func TestUpdateSettingsRetainsOmittedPasswordAndSenderDecryptsIt(t *testing.T) {
	masterKey := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	credentialCipher, err := credential.NewCipher(masterKey)
	if err != nil {
		t.Fatal(err)
	}
	passwordCipher := NewPasswordCipher(credentialCipher)
	encrypted, nonce, err := passwordCipher.Encrypt("smtp-secret")
	if err != nil {
		t.Fatal(err)
	}
	store := &stubSettingsStore{stored: StoredSettings{
		Settings:          Settings{Enabled: true, Host: "smtp.example.com", Port: 587, Security: SecurityStartTLS, Username: "mailer", FromEmail: "noreply@example.com", PasswordConfigured: true},
		EncryptedPassword: encrypted, PasswordNonce: nonce, KeyVersion: 1,
	}}
	sender := &stubSender{}
	service := &Service{Settings: store, Cipher: passwordCipher, Sender: sender, PublicURL: "https://app.example.com"}
	actor := &domain.User{ID: "system-1", Role: domain.UserRoleSystem}
	name := "Assistant"
	updated, err := service.UpdateSettings(context.Background(), actor, UpdateSettingsInput{FromName: &name})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if !updated.PasswordConfigured || len(store.stored.EncryptedPassword) == 0 {
		t.Fatal("omitted password cleared the stored secret")
	}
	if err := service.TestSettings(context.Background(), actor, "recipient@example.com"); err != nil {
		t.Fatalf("test settings: %v", err)
	}
	if sender.config.Password != "smtp-secret" || sender.message.To != "recipient@example.com" {
		t.Fatalf("unexpected send request: %+v %+v", sender.config, sender.message)
	}
}

func TestMailSettingsRequireExactSystemRole(t *testing.T) {
	service := &Service{Settings: &stubSettingsStore{stored: StoredSettings{Settings: Settings{Port: 587, Security: SecurityStartTLS}}}}
	_, err := service.GetSettings(context.Background(), &domain.User{ID: "admin-1", Role: domain.UserRoleAdmin})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("admin settings error = %v, want forbidden", err)
	}
}
