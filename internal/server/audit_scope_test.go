package server

import (
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestAuditRequiredRole(t *testing.T) {
	tests := map[string]string{
		"/api/v1/admin/provider-credentials/:credentialID": domain.UserRoleSystem,
		"/api/v1/admin/models/:modelID":                    domain.UserRoleSystem,
		"/api/v1/admin/mail-settings":                      domain.UserRoleSystem,
		"/api/v1/users/:userID":                            domain.UserRoleAdmin,
		"/api/v1/admin/billing/accounts/:userID":           domain.UserRoleAdmin,
		"/api/v1/conversations":                            domain.UserRoleUser,
	}
	for path, expected := range tests {
		if actual := auditRequiredRole(path); actual != expected {
			t.Errorf("auditRequiredRole(%q) = %q, want %q", path, actual, expected)
		}
	}
}
