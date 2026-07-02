package auth

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type stubActionTokenStore struct {
	user       *domain.User
	purpose    string
	hash       []byte
	password   string
	consumed   bool
	markedSent bool
	lastSentAt *time.Time
}

func (s *stubActionTokenStore) CreateActionToken(_ context.Context, params CreateActionTokenParams) (string, error) {
	s.purpose = params.Purpose
	s.hash = append([]byte(nil), params.TokenHash...)
	s.consumed = false
	return "token-1", nil
}

func (s *stubActionTokenStore) MarkActionTokenSent(_ context.Context, _ string, _ time.Time) error {
	s.markedSent = true
	return nil
}

func (s *stubActionTokenStore) LastActionTokenSentAt(context.Context, string, string) (*time.Time, error) {
	return s.lastSentAt, nil
}

func (s *stubActionTokenStore) VerifyEmailWithToken(_ context.Context, hash []byte, _ time.Time) (*domain.User, error) {
	if s.consumed || s.purpose != ActionPurposeEmailVerification || !bytes.Equal(hash, s.hash) {
		return nil, domain.ErrNotFound
	}
	s.consumed = true
	now := time.Now().UTC()
	s.user.EmailVerifiedAt = &now
	clone := *s.user
	return &clone, nil
}

func (s *stubActionTokenStore) ResetPasswordWithToken(_ context.Context, hash []byte, passwordHash string, _ time.Time) (*domain.User, error) {
	if s.consumed || s.purpose != ActionPurposePasswordReset || !bytes.Equal(hash, s.hash) {
		return nil, domain.ErrNotFound
	}
	s.consumed = true
	s.password = passwordHash
	s.user.PasswordHash = passwordHash
	s.user.AuthVersion++
	clone := *s.user
	return &clone, nil
}

type stubActionMailer struct {
	verificationToken string
	resetToken        string
}

func (s *stubActionMailer) SendVerification(_ context.Context, _ string, token string) error {
	s.verificationToken = token
	return nil
}

func (s *stubActionMailer) SendPasswordReset(_ context.Context, _ string, token string) error {
	s.resetToken = token
	return nil
}

func TestRegisterCreatesUnverifiedUserAndHashesVerificationToken(t *testing.T) {
	users := newStubUserStore()
	actions := &stubActionTokenStore{}
	mailer := &stubActionMailer{}
	service := &Service{Users: users, ActionTokens: actions, Mailer: mailer}

	result, err := service.Register(context.Background(), RegisterInput{Email: "new@example.com", Username: "new", Password: "secret123"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if !result.VerificationRequired || !result.EmailSent {
		t.Fatalf("unexpected registration result: %+v", result)
	}
	created := users.users["user-created"]
	if created.EmailVerifiedAt != nil {
		t.Fatal("publicly registered user is already verified")
	}
	hash, err := HashActionToken(mailer.verificationToken)
	if err != nil || !bytes.Equal(hash, actions.hash) {
		t.Fatal("verification token was not stored as its SHA256 hash")
	}
}

func TestVerifyEmailTokenIsSingleUse(t *testing.T) {
	user := &domain.User{ID: "user-1", Email: "user@example.com", AuthVersion: 1}
	actions := &stubActionTokenStore{user: user, purpose: ActionPurposeEmailVerification}
	token, hash, err := GenerateActionToken()
	if err != nil {
		t.Fatal(err)
	}
	actions.hash = hash
	service := &Service{ActionTokens: actions}
	if _, err := service.VerifyEmail(context.Background(), VerifyEmailInput{Token: token}); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	if _, err := service.VerifyEmail(context.Background(), VerifyEmailInput{Token: token}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("reused token error = %v, want invalid input", err)
	}
}

func TestResendVerificationHonorsCooldown(t *testing.T) {
	user := &domain.User{ID: "user-1", Email: "user@example.com", Role: domain.UserRoleUser, Status: domain.UserStatusActive, AuthVersion: 1}
	users := newStubUserStore(user)
	sentAt := time.Now().UTC().Add(-30 * time.Second)
	actions := &stubActionTokenStore{user: users.users[user.ID], lastSentAt: &sentAt}
	mailer := &stubActionMailer{}
	service := &Service{Users: users, ActionTokens: actions, Mailer: mailer}
	if err := service.ResendVerification(context.Background(), ResendVerificationInput{Email: user.Email}); err != nil {
		t.Fatalf("resend verification: %v", err)
	}
	if mailer.verificationToken != "" || len(actions.hash) != 0 {
		t.Fatal("verification token was issued during cooldown")
	}
}

func TestResetPasswordHashesPasswordAndInvalidatesExistingJWT(t *testing.T) {
	verifiedAt := time.Now().UTC()
	user := &domain.User{ID: "user-1", Email: "user@example.com", Role: domain.UserRoleUser, Status: domain.UserStatusActive, EmailVerifiedAt: &verifiedAt, AuthVersion: 1}
	users := newStubUserStore(user)
	tokens, err := NewTokenService(TokenSettings{Secret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	accessToken, _, err := tokens.Issue(user)
	if err != nil {
		t.Fatal(err)
	}
	actionToken, hash, err := GenerateActionToken()
	if err != nil {
		t.Fatal(err)
	}
	actions := &stubActionTokenStore{user: users.users[user.ID], purpose: ActionPurposePasswordReset, hash: hash}
	service := &Service{Users: users, Tokens: tokens, ActionTokens: actions}
	updated, err := service.ResetPassword(context.Background(), ResetPasswordInput{Token: actionToken, NewPassword: "new-secret123"})
	if err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if updated.AuthVersion != 2 || ComparePasswordHash(actions.password, "new-secret123") != nil {
		t.Fatalf("password reset did not update credentials: %+v", updated)
	}
	if _, err := service.AuthenticateAccessToken(context.Background(), accessToken); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("stale token error = %v, want unauthorized", err)
	}
}
