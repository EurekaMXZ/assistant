package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type stubUserStore struct {
	users       map[string]*domain.User
	emailToID   map[string]string
	touchedUser string
	listParams  ListUsersParams
}

func newStubUserStore(users ...*domain.User) *stubUserStore {
	store := &stubUserStore{
		users:     make(map[string]*domain.User, len(users)),
		emailToID: make(map[string]string, len(users)),
	}
	for _, user := range users {
		clone := *user
		store.users[user.ID] = &clone
		store.emailToID[user.Email] = user.ID
	}
	return store
}

func (s *stubUserStore) CreateUser(_ context.Context, params CreateUserParams) (*domain.User, error) {
	for _, user := range s.users {
		if user.Email == params.Email {
			return nil, domain.ErrConflict
		}
	}
	user := &domain.User{
		ID:              "user-created",
		Email:           params.Email,
		Username:        params.Username,
		PasswordHash:    params.PasswordHash,
		Role:            params.Role,
		Status:          params.Status,
		EmailVerifiedAt: params.EmailVerifiedAt,
		AuthVersion:     1,
	}
	s.users[user.ID] = user
	s.emailToID[user.Email] = user.ID
	return user, nil
}

func (s *stubUserStore) GetUserByID(_ context.Context, userID string) (*domain.User, error) {
	user, ok := s.users[userID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	clone := *user
	return &clone, nil
}

func (s *stubUserStore) GetUserByEmail(_ context.Context, email string) (*domain.User, error) {
	userID, ok := s.emailToID[email]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return s.GetUserByID(context.Background(), userID)
}

func (s *stubUserStore) ListUsers(_ context.Context, params ListUsersParams) ([]domain.User, string, error) {
	s.listParams = params
	results := make([]domain.User, 0, len(s.users))
	for _, user := range s.users {
		if user.ID == params.ExcludeUserID {
			continue
		}
		matchedRole := len(params.Roles) == 0
		for _, role := range params.Roles {
			if user.Role == role {
				matchedRole = true
				break
			}
		}
		if matchedRole {
			results = append(results, *user)
		}
	}
	return results, "next-users", nil
}

func (s *stubUserStore) UpdateUser(_ context.Context, params UpdateUserParams) (*domain.User, error) {
	user, ok := s.users[params.UserID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if len(params.AllowedCurrentRoles) > 0 && !containsRole(params.AllowedCurrentRoles, user.Role) {
		return nil, domain.ErrNotFound
	}
	if params.Email != nil {
		user.Email = *params.Email
	}
	if params.Username != nil {
		user.Username = *params.Username
	}
	if params.Role != nil {
		user.Role = *params.Role
	}
	if params.Status != nil {
		user.Status = *params.Status
	}
	clone := *user
	return &clone, nil
}

func (s *stubUserStore) UpdateUserPassword(_ context.Context, userID string, passwordHash string, allowedCurrentRoles []string) (*domain.User, error) {
	user, ok := s.users[userID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if len(allowedCurrentRoles) > 0 && !containsRole(allowedCurrentRoles, user.Role) {
		return nil, domain.ErrNotFound
	}
	user.PasswordHash = passwordHash
	user.AuthVersion++
	clone := *user
	return &clone, nil
}

func (s *stubUserStore) DeleteManagedUser(_ context.Context, userID string, allowedCurrentRoles []string) error {
	user, ok := s.users[userID]
	if !ok || (len(allowedCurrentRoles) > 0 && !containsRole(allowedCurrentRoles, user.Role)) {
		return domain.ErrNotFound
	}
	user.Status = domain.UserStatusDisabled
	user.AuthVersion++
	now := time.Now().UTC()
	user.DeletedAt = &now
	return nil
}

func containsRole(roles []string, role string) bool {
	for _, candidate := range roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func (s *stubUserStore) TouchUserLogin(_ context.Context, userID string) (*domain.User, error) {
	user, ok := s.users[userID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	s.touchedUser = userID
	clone := *user
	return &clone, nil
}

func (s *stubUserStore) EnsureSystemUser(_ context.Context, params EnsureSystemUserParams) (*domain.User, error) {
	if user, ok := s.users["system"]; ok {
		user.Email = params.Email
		user.Username = params.Username
		if user.PasswordHash != params.PasswordHash {
			user.AuthVersion++
		}
		user.PasswordHash = params.PasswordHash
		if user.EmailVerifiedAt == nil {
			now := time.Now().UTC()
			user.EmailVerifiedAt = &now
		}
		clone := *user
		return &clone, nil
	}
	verifiedAt := time.Now().UTC()
	user := &domain.User{
		ID:              "system",
		Email:           params.Email,
		Username:        params.Username,
		PasswordHash:    params.PasswordHash,
		Role:            domain.UserRoleSystem,
		Status:          domain.UserStatusActive,
		EmailVerifiedAt: &verifiedAt,
		AuthVersion:     1,
	}
	s.users[user.ID] = user
	s.emailToID[user.Email] = user.ID
	clone := *user
	return &clone, nil
}

func TestServiceCreateManagedUserRejectsSameLevelRole(t *testing.T) {
	service := &Service{
		Users: newStubUserStore(),
	}

	actor := &domain.User{ID: "admin-1", Role: domain.UserRoleAdmin, Status: domain.UserStatusActive}
	_, err := service.CreateManagedUser(context.Background(), actor, CreateManagedUserInput{
		Email:    "admin2@example.com",
		Username: "admin2",
		Password: "secret123",
		Role:     domain.UserRoleAdmin,
		Status:   domain.UserStatusActive,
	})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden error, got %v", err)
	}
}

func TestServiceGetManagedUserAllowsSelfRead(t *testing.T) {
	service := &Service{
		Users: newStubUserStore(&domain.User{
			ID:       "admin-1",
			Email:    "admin@example.com",
			Username: "admin",
			Role:     domain.UserRoleAdmin,
			Status:   domain.UserStatusActive,
		}),
	}

	actor := &domain.User{ID: "admin-1", Role: domain.UserRoleAdmin, Status: domain.UserStatusActive}
	user, err := service.GetManagedUser(context.Background(), actor, actor.ID)
	if err != nil || user.ID != actor.ID {
		t.Fatalf("managed self read user=%#v err=%v", user, err)
	}
}

func TestServiceListManagedUsersIncludesEveryRole(t *testing.T) {
	store := newStubUserStore(
		&domain.User{ID: "system-1", Role: domain.UserRoleSystem},
		&domain.User{ID: "admin-1", Role: domain.UserRoleAdmin},
		&domain.User{ID: "user-1", Role: domain.UserRoleUser},
	)
	service := &Service{Users: store}

	users, next, err := service.ListManagedUsers(context.Background(), store.users["admin-1"], 50, "cursor-users")
	if err != nil || len(users) != 3 || next != "next-users" {
		t.Fatalf("managed users=%#v next=%q err=%v", users, next, err)
	}
	if store.listParams.Limit != 50 || store.listParams.Cursor != "cursor-users" {
		t.Fatalf("list params=%#v", store.listParams)
	}
}

func TestServiceAdminCannotUpdateAdministrator(t *testing.T) {
	store := newStubUserStore(
		&domain.User{ID: "admin-1", Role: domain.UserRoleAdmin},
		&domain.User{ID: "admin-2", Role: domain.UserRoleAdmin},
	)
	service := &Service{Users: store}
	username := "changed"

	_, err := service.UpdateManagedUser(context.Background(), store.users["admin-1"], UpdateManagedUserInput{
		UserID: "admin-2", Username: &username,
	})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden error, got %v", err)
	}
}

func TestServiceLoginIssuesSessionAndTouchesUser(t *testing.T) {
	passwordHash, err := HashPassword("secret123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	verifiedAt := time.Now().UTC()
	store := newStubUserStore(&domain.User{
		ID:              "user-1",
		Email:           "user@example.com",
		Username:        "user",
		PasswordHash:    passwordHash,
		Role:            domain.UserRoleUser,
		Status:          domain.UserStatusActive,
		EmailVerifiedAt: &verifiedAt,
		AuthVersion:     1,
	})
	tokens, err := NewTokenService(TokenSettings{
		Secret:         "secret",
		Issuer:         "assistant-test",
		AccessTokenTTL: 2 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	service := &Service{
		Users:  store,
		Tokens: tokens,
	}

	session, err := service.Login(context.Background(), LoginInput{
		Email:    "user@example.com",
		Password: "secret123",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if session.AccessToken == "" {
		t.Fatal("expected access token")
	}
	if store.touchedUser != "user-1" {
		t.Fatalf("expected touched user to be user-1, got %q", store.touchedUser)
	}
}

func TestServiceLoginRejectsUnverifiedUserAfterValidPassword(t *testing.T) {
	passwordHash, err := HashPassword("secret123")
	if err != nil {
		t.Fatal(err)
	}
	store := newStubUserStore(&domain.User{
		ID: "user-1", Email: "user@example.com", Username: "user", PasswordHash: passwordHash,
		Role: domain.UserRoleUser, Status: domain.UserStatusActive, AuthVersion: 1,
	})
	tokens, err := NewTokenService(TokenSettings{Secret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Users: store, Tokens: tokens}
	_, err = service.Login(context.Background(), LoginInput{Email: "user@example.com", Password: "secret123"})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("login error = %v, want forbidden", err)
	}
	if store.touchedUser != "" {
		t.Fatal("unverified login updated last_login_at")
	}
}
