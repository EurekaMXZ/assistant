package domain

import (
	"fmt"
	"strings"
	"time"
)

const (
	UserRoleSystem = "system"
	UserRoleAdmin  = "admin"
	UserRoleUser   = "user"

	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
)

type User struct {
	ID              string     `json:"id"`
	Email           string     `json:"email"`
	Username        string     `json:"username"`
	Role            string     `json:"role"`
	Status          string     `json:"status"`
	PasswordHash    string     `json:"-"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	AuthVersion     int64      `json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func NormalizeEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	switch {
	case normalized == "":
		return "", NewValidationError("email is required")
	case !strings.Contains(normalized, "@"):
		return "", NewValidationError("email is invalid")
	default:
		return normalized, nil
	}
}

func NormalizeUsername(username string) (string, error) {
	normalized := strings.TrimSpace(username)
	if normalized == "" {
		return "", NewValidationError("username is required")
	}
	return normalized, nil
}

func NormalizeUserRole(role string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case UserRoleSystem:
		return UserRoleSystem, nil
	case UserRoleAdmin:
		return UserRoleAdmin, nil
	case UserRoleUser:
		return UserRoleUser, nil
	default:
		return "", NewValidationError(fmt.Sprintf("unsupported user role %q", role))
	}
}

func NormalizeUserStatus(status string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", UserStatusActive:
		return UserStatusActive, nil
	case UserStatusDisabled:
		return UserStatusDisabled, nil
	default:
		return "", NewValidationError(fmt.Sprintf("unsupported user status %q", status))
	}
}

func UserRoleRank(role string) int {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case UserRoleSystem:
		return 3
	case UserRoleAdmin:
		return 2
	case UserRoleUser:
		return 1
	default:
		return 0
	}
}

func UserRoleSatisfies(actual string, minimum string) bool {
	return UserRoleRank(actual) >= UserRoleRank(minimum)
}

func UserRoleCanManage(actorRole string, targetRole string) bool {
	return UserRoleRank(actorRole) > UserRoleRank(targetRole)
}

func ManageableUserRoles(actorRole string) []string {
	switch actorRole {
	case UserRoleSystem:
		return []string{UserRoleAdmin, UserRoleUser}
	case UserRoleAdmin:
		return []string{UserRoleUser}
	default:
		return nil
	}
}
