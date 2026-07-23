package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type UserStore interface {
	CreateUser(ctx context.Context, params CreateUserParams) (*domain.User, error)
	GetUserByID(ctx context.Context, userID string) (*domain.User, error)
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	ListUsers(ctx context.Context, params ListUsersParams) ([]domain.User, string, error)
	UpdateUser(ctx context.Context, params UpdateUserParams) (*domain.User, error)
	DeleteManagedUser(ctx context.Context, userID string, allowedCurrentRoles []string) error
	UpdateUserPassword(ctx context.Context, userID string, passwordHash string, allowedCurrentRoles []string) (*domain.User, error)
	TouchUserLogin(ctx context.Context, userID string) (*domain.User, error)
	EnsureSystemUser(ctx context.Context, params EnsureSystemUserParams) (*domain.User, error)
}

type ActionTokenStore interface {
	CreateActionToken(ctx context.Context, params CreateActionTokenParams) (string, error)
	MarkActionTokenSent(ctx context.Context, tokenID string, sentAt time.Time) error
	LastActionTokenSentAt(ctx context.Context, userID string, purpose string) (*time.Time, error)
	VerifyEmailWithToken(ctx context.Context, tokenHash []byte, now time.Time) (*domain.User, error)
	ResetPasswordWithToken(ctx context.Context, tokenHash []byte, passwordHash string, now time.Time) (*domain.User, error)
}

type ActionMailer interface {
	SendVerification(ctx context.Context, recipient string, token string) error
	SendPasswordReset(ctx context.Context, recipient string, token string) error
}

type Service struct {
	Users        UserStore
	Tokens       *TokenService
	ActionTokens ActionTokenStore
	Mailer       ActionMailer
	SystemUser   SystemUserConfig
}

type SystemUserConfig struct {
	Email        string
	Username     string
	PasswordHash string
}

type CreateUserParams struct {
	Email           string
	Username        string
	PasswordHash    string
	Role            string
	Status          string
	EmailVerifiedAt *time.Time
}

type CreateActionTokenParams struct {
	UserID    string
	Purpose   string
	TokenHash []byte
	ExpiresAt time.Time
}

type ListUsersParams struct {
	Roles         []string
	ExcludeUserID string
	Limit         int
	Cursor        string
}

type UpdateUserParams struct {
	UserID              string
	Email               *string
	Username            *string
	Role                *string
	Status              *string
	StorageQuotaBytes   *int64
	SandboxQuota        *int
	AllowedCurrentRoles []string
}

type EnsureSystemUserParams struct {
	Email        string
	Username     string
	PasswordHash string
}

type Session struct {
	AccessToken string       `json:"access_token"`
	TokenType   string       `json:"token_type"`
	ExpiresAt   time.Time    `json:"expires_at"`
	User        *domain.User `json:"user"`
}

type RegisterInput struct {
	Email    string
	Username string
	Password string
}

type RegistrationResult struct {
	VerificationRequired bool `json:"verification_required"`
	EmailSent            bool `json:"email_sent"`
}

type LoginInput struct {
	Email    string
	Password string
}

type VerifyEmailInput struct {
	Token string
}

type ResendVerificationInput struct {
	Email string
}

type ForgotPasswordInput struct {
	Email string
}

type ResetPasswordInput struct {
	Token       string
	NewPassword string
}

type CreateManagedUserInput struct {
	Email    string
	Username string
	Password string
	Role     string
	Status   string
}

type UpdateManagedUserInput struct {
	UserID            string
	Email             *string
	Username          *string
	Role              *string
	Status            *string
	StorageQuotaBytes *int64
	SandboxQuota      *int
}

type ResetManagedPasswordInput struct {
	UserID      string
	NewPassword string
}

type ChangePasswordInput struct {
	UserID          string
	CurrentPassword string
	NewPassword     string
}

func (s *Service) BootstrapSystemUser(ctx context.Context) (*domain.User, error) {
	if s == nil {
		return nil, errors.New("auth service is nil")
	}
	if s.Users == nil {
		return nil, errors.New("auth service requires user store")
	}

	email, err := domain.NormalizeEmail(s.SystemUser.Email)
	if err != nil {
		return nil, err
	}
	username, err := domain.NormalizeUsername(s.SystemUser.Username)
	if err != nil {
		return nil, err
	}
	passwordHash := strings.TrimSpace(s.SystemUser.PasswordHash)
	if passwordHash == "" {
		return nil, domain.NewValidationError("system user password hash is required")
	}
	if err := ValidatePasswordHash(passwordHash); err != nil {
		return nil, err
	}

	user, err := s.Users.EnsureSystemUser(ctx, EnsureSystemUserParams{
		Email:        email,
		Username:     username,
		PasswordHash: passwordHash,
	})
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *Service) AuthenticateAccessToken(ctx context.Context, accessToken string) (*domain.User, error) {
	if s == nil || s.Users == nil || s.Tokens == nil {
		return nil, errors.New("auth service is not configured")
	}

	claims, err := s.Tokens.Parse(strings.TrimSpace(accessToken))
	if err != nil {
		return nil, err
	}

	user, err := s.Users.GetUserByID(ctx, claims.Subject)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.NewUnauthorizedError("invalid access token")
		}
		return nil, err
	}
	if user.Status != domain.UserStatusActive {
		return nil, domain.NewForbiddenError("user is disabled")
	}
	if claims.AuthVersion != user.AuthVersion {
		return nil, domain.NewUnauthorizedError("invalid access token")
	}

	return user, nil
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (*RegistrationResult, error) {
	user, err := s.createUser(ctx, input.Email, input.Username, input.Password, domain.UserRoleUser, domain.UserStatusActive, false)
	if err != nil {
		return nil, err
	}
	emailSent, err := s.issueActionToken(ctx, user, ActionPurposeEmailVerification, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	return &RegistrationResult{VerificationRequired: true, EmailSent: emailSent}, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput) (*Session, error) {
	if s == nil || s.Users == nil || s.Tokens == nil {
		return nil, errors.New("auth service is not configured")
	}

	email, err := domain.NormalizeEmail(input.Email)
	if err != nil {
		return nil, err
	}

	user, err := s.Users.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.NewAuthenticationError("invalid email or password")
		}
		return nil, err
	}
	if user.Status != domain.UserStatusActive {
		return nil, domain.NewForbiddenError("user is disabled")
	}
	if err := ComparePasswordHash(user.PasswordHash, input.Password); err != nil {
		return nil, err
	}
	if user.EmailVerifiedAt == nil {
		return nil, domain.NewForbiddenError("email verification required")
	}

	user, err = s.Users.TouchUserLogin(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return s.issueSession(user)
}

func (s *Service) VerifyEmail(ctx context.Context, input VerifyEmailInput) (*domain.User, error) {
	if s == nil || s.ActionTokens == nil {
		return nil, errors.New("email verification is not configured")
	}
	hash, err := HashActionToken(input.Token)
	if err != nil {
		return nil, domain.NewValidationError("verification token is invalid or expired")
	}
	user, err := s.ActionTokens.VerifyEmailWithToken(ctx, hash, s.now())
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.NewValidationError("verification token is invalid or expired")
		}
		return nil, err
	}
	return user, nil
}

func (s *Service) ResendVerification(ctx context.Context, input ResendVerificationInput) error {
	if s == nil || s.Users == nil || s.ActionTokens == nil {
		return errors.New("email verification is not configured")
	}
	email, err := domain.NormalizeEmail(input.Email)
	if err != nil {
		return err
	}
	user, err := s.Users.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	if user.EmailVerifiedAt != nil || user.Status != domain.UserStatusActive {
		return nil
	}
	if coolingDown, err := s.actionTokenCoolingDown(ctx, user.ID, ActionPurposeEmailVerification); err != nil {
		return err
	} else if coolingDown {
		return nil
	}
	_, err = s.issueActionToken(ctx, user, ActionPurposeEmailVerification, 24*time.Hour)
	return err
}

func (s *Service) ForgotPassword(ctx context.Context, input ForgotPasswordInput) error {
	if s == nil || s.Users == nil || s.ActionTokens == nil {
		return errors.New("password recovery is not configured")
	}
	email, err := domain.NormalizeEmail(input.Email)
	if err != nil {
		return err
	}
	user, err := s.Users.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	if user.EmailVerifiedAt == nil || user.Status != domain.UserStatusActive {
		return nil
	}
	if coolingDown, err := s.actionTokenCoolingDown(ctx, user.ID, ActionPurposePasswordReset); err != nil {
		return err
	} else if coolingDown {
		return nil
	}
	_, err = s.issueActionToken(ctx, user, ActionPurposePasswordReset, time.Hour)
	return err
}

func (s *Service) ResetPassword(ctx context.Context, input ResetPasswordInput) (*domain.User, error) {
	if s == nil || s.ActionTokens == nil {
		return nil, errors.New("password recovery is not configured")
	}
	hash, err := HashActionToken(input.Token)
	if err != nil {
		return nil, domain.NewValidationError("password reset token is invalid or expired")
	}
	passwordHash, err := HashPassword(input.NewPassword)
	if err != nil {
		return nil, err
	}
	user, err := s.ActionTokens.ResetPasswordWithToken(ctx, hash, passwordHash, s.now())
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.NewValidationError("password reset token is invalid or expired")
		}
		return nil, err
	}
	return user, nil
}

func (s *Service) ListManagedUsers(ctx context.Context, actor *domain.User, limit int, cursor string) ([]domain.User, string, error) {
	if s == nil || s.Users == nil {
		return nil, "", errors.New("auth service is not configured")
	}
	if actor == nil {
		return nil, "", domain.NewUnauthorizedError("authentication required")
	}

	if !domain.UserRoleSatisfies(actor.Role, domain.UserRoleAdmin) {
		return nil, "", domain.NewForbiddenError("insufficient privileges")
	}

	return s.Users.ListUsers(ctx, ListUsersParams{Limit: limit, Cursor: cursor})
}

func (s *Service) GetManagedUser(ctx context.Context, actor *domain.User, userID string) (*domain.User, error) {
	if s == nil || s.Users == nil {
		return nil, errors.New("auth service is not configured")
	}
	if actor == nil {
		return nil, domain.NewUnauthorizedError("authentication required")
	}

	if !domain.UserRoleSatisfies(actor.Role, domain.UserRoleAdmin) {
		return nil, domain.NewForbiddenError("insufficient privileges")
	}
	return s.Users.GetUserByID(ctx, userID)
}

func (s *Service) CreateManagedUser(ctx context.Context, actor *domain.User, input CreateManagedUserInput) (*domain.User, error) {
	return s.createManagedUser(ctx, actor, input)
}

func (s *Service) UpdateManagedUser(ctx context.Context, actor *domain.User, input UpdateManagedUserInput) (*domain.User, error) {
	if s == nil || s.Users == nil {
		return nil, errors.New("auth service is not configured")
	}
	if actor == nil {
		return nil, domain.NewUnauthorizedError("authentication required")
	}

	target, err := s.Users.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, err
	}
	if target.DeletedAt != nil {
		return nil, domain.ErrNotFound
	}
	if err := ensureManageableTarget(actor, target); err != nil {
		return nil, err
	}

	params := UpdateUserParams{UserID: input.UserID, AllowedCurrentRoles: domain.ManageableUserRoles(actor.Role)}
	if input.Email != nil {
		normalized, err := domain.NormalizeEmail(*input.Email)
		if err != nil {
			return nil, err
		}
		params.Email = &normalized
	}
	if input.Username != nil {
		normalized, err := domain.NormalizeUsername(*input.Username)
		if err != nil {
			return nil, err
		}
		params.Username = &normalized
	}
	if input.Role != nil {
		normalized, err := domain.NormalizeUserRole(*input.Role)
		if err != nil {
			return nil, err
		}
		if !domain.UserRoleCanManage(actor.Role, normalized) {
			return nil, domain.NewForbiddenError("cannot assign target role")
		}
		params.Role = &normalized
	}
	if input.Status != nil {
		normalized, err := domain.NormalizeUserStatus(*input.Status)
		if err != nil {
			return nil, err
		}
		params.Status = &normalized
	}
	if input.StorageQuotaBytes != nil {
		if *input.StorageQuotaBytes < 0 {
			return nil, domain.NewValidationError("storage quota must be non-negative")
		}
		params.StorageQuotaBytes = input.StorageQuotaBytes
	}
	if input.SandboxQuota != nil {
		if *input.SandboxQuota < 0 {
			return nil, domain.NewValidationError("sandbox quota must be non-negative")
		}
		params.SandboxQuota = input.SandboxQuota
	}

	return s.Users.UpdateUser(ctx, params)
}

func (s *Service) DeleteManagedUser(ctx context.Context, actor *domain.User, userID string) error {
	if s == nil || s.Users == nil {
		return errors.New("auth service is not configured")
	}
	if actor == nil {
		return domain.NewUnauthorizedError("authentication required")
	}
	target, err := s.Users.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if target.DeletedAt != nil {
		return domain.ErrNotFound
	}
	if err := ensureManageableTarget(actor, target); err != nil {
		return err
	}
	return s.Users.DeleteManagedUser(ctx, userID, domain.ManageableUserRoles(actor.Role))
}

func (s *Service) ResetManagedPassword(ctx context.Context, actor *domain.User, input ResetManagedPasswordInput) (*domain.User, error) {
	if s == nil || s.Users == nil {
		return nil, errors.New("auth service is not configured")
	}
	if actor == nil {
		return nil, domain.NewUnauthorizedError("authentication required")
	}

	target, err := s.Users.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, err
	}
	if err := ensureManageableTarget(actor, target); err != nil {
		return nil, err
	}

	passwordHash, err := HashPassword(input.NewPassword)
	if err != nil {
		return nil, err
	}

	return s.Users.UpdateUserPassword(ctx, input.UserID, passwordHash, domain.ManageableUserRoles(actor.Role))
}

func (s *Service) ChangeOwnPassword(ctx context.Context, input ChangePasswordInput) (*domain.User, error) {
	if s == nil || s.Users == nil {
		return nil, errors.New("auth service is not configured")
	}

	user, err := s.Users.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, err
	}
	if user.Status != domain.UserStatusActive {
		return nil, domain.NewForbiddenError("user is disabled")
	}
	if err := ComparePasswordHash(user.PasswordHash, input.CurrentPassword); err != nil {
		return nil, err
	}

	passwordHash, err := HashPassword(input.NewPassword)
	if err != nil {
		return nil, err
	}

	return s.Users.UpdateUserPassword(ctx, user.ID, passwordHash, []string{user.Role})
}

func (s *Service) createManagedUser(ctx context.Context, actor *domain.User, input CreateManagedUserInput) (*domain.User, error) {
	if s == nil || s.Users == nil {
		return nil, errors.New("auth service is not configured")
	}
	if actor == nil {
		return nil, domain.NewUnauthorizedError("authentication required")
	}

	role, err := domain.NormalizeUserRole(input.Role)
	if err != nil {
		return nil, err
	}
	if !domain.UserRoleCanManage(actor.Role, role) {
		return nil, domain.NewForbiddenError("cannot create target role")
	}

	return s.createUser(ctx, input.Email, input.Username, input.Password, role, input.Status, true)
}

func (s *Service) createUser(ctx context.Context, email string, username string, password string, role string, status string, verified bool) (*domain.User, error) {
	if s == nil || s.Users == nil {
		return nil, errors.New("auth service is not configured")
	}

	normalizedEmail, err := domain.NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	normalizedUsername, err := domain.NormalizeUsername(username)
	if err != nil {
		return nil, err
	}
	normalizedRole, err := domain.NormalizeUserRole(role)
	if err != nil {
		return nil, err
	}
	if normalizedRole == domain.UserRoleSystem {
		return nil, domain.NewForbiddenError("system user can only be configured at startup")
	}
	normalizedStatus, err := domain.NormalizeUserStatus(status)
	if err != nil {
		return nil, err
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	var verifiedAt *time.Time
	if verified {
		now := s.now()
		verifiedAt = &now
	}
	return s.Users.CreateUser(ctx, CreateUserParams{
		Email:           normalizedEmail,
		Username:        normalizedUsername,
		PasswordHash:    passwordHash,
		Role:            normalizedRole,
		Status:          normalizedStatus,
		EmailVerifiedAt: verifiedAt,
	})
}

func (s *Service) actionTokenCoolingDown(ctx context.Context, userID string, purpose string) (bool, error) {
	sentAt, err := s.ActionTokens.LastActionTokenSentAt(ctx, userID, purpose)
	if err != nil {
		return false, err
	}
	return sentAt != nil && sentAt.After(s.now().Add(-time.Minute)), nil
}

func (s *Service) issueActionToken(ctx context.Context, user *domain.User, purpose string, ttl time.Duration) (bool, error) {
	if s.ActionTokens == nil {
		return false, errors.New("account action tokens are not configured")
	}
	token, hash, err := GenerateActionToken()
	if err != nil {
		return false, err
	}
	tokenID, err := s.ActionTokens.CreateActionToken(ctx, CreateActionTokenParams{
		UserID: user.ID, Purpose: purpose, TokenHash: hash, ExpiresAt: s.now().Add(ttl),
	})
	if err != nil {
		return false, err
	}
	if s.Mailer == nil {
		return false, nil
	}
	if purpose == ActionPurposeEmailVerification {
		err = s.Mailer.SendVerification(ctx, user.Email, token)
	} else {
		err = s.Mailer.SendPasswordReset(ctx, user.Email, token)
	}
	if err != nil {
		return false, nil
	}
	if err := s.ActionTokens.MarkActionTokenSent(ctx, tokenID, s.now()); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) now() time.Time {
	return time.Now().UTC()
}

func (s *Service) issueSession(user *domain.User) (*Session, error) {
	if s == nil || s.Tokens == nil {
		return nil, errors.New("auth service requires token service")
	}
	accessToken, expiresAt, err := s.Tokens.Issue(user)
	if err != nil {
		return nil, err
	}
	return &Session{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt,
		User:        user,
	}, nil
}

func ensureManageableTarget(actor *domain.User, target *domain.User) error {
	if actor == nil {
		return domain.NewUnauthorizedError("authentication required")
	}
	if target == nil {
		return domain.ErrNotFound
	}
	if target.DeletedAt != nil {
		return domain.ErrNotFound
	}
	if actor.ID == target.ID {
		return domain.NewForbiddenError("cannot manage yourself")
	}
	if !domain.UserRoleCanManage(actor.Role, target.Role) {
		return domain.NewForbiddenError("cannot manage target user")
	}
	return nil
}
