package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProfileRepositoryUserPreferencesVersionCASIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)

	userID := insertIntegrationUser(t, pool, domain.UserRoleUser)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, cleanupErr := pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID); cleanupErr != nil {
			t.Errorf("delete integration user: %v", cleanupErr)
		}
	})
	repository := NewProfileRepository(pool)

	created, err := repository.UpsertUserPreferences(t.Context(), domain.UserPreferences{
		UserID: userID, PreferencesText: "first",
	}, 0)
	if err != nil || created.Version != 1 {
		t.Fatalf("create preferences: version=%v err=%v", created, err)
	}
	updated, err := repository.UpsertUserPreferences(t.Context(), domain.UserPreferences{
		UserID: userID, PreferencesText: "second", LocationEnabledForModel: true,
	}, created.Version)
	if err != nil || updated.Version != 2 || updated.PreferencesText != "second" {
		t.Fatalf("update preferences: preferences=%#v err=%v", updated, err)
	}
	if _, err := repository.UpsertUserPreferences(t.Context(), domain.UserPreferences{
		UserID: userID, PreferencesText: "stale",
	}, created.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale update error=%v, want conflict", err)
	}
}
