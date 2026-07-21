package profile

import (
	"context"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type Repository interface {
	GetUserPreferences(ctx context.Context, userID string) (*domain.UserPreferences, error)
	UpsertUserPreferences(ctx context.Context, preferences domain.UserPreferences, expectedVersion int64) (*domain.UserPreferences, error)
	GetUserLocation(ctx context.Context, userID string) (*domain.UserLocation, error)
	UpsertUserLocation(ctx context.Context, location domain.UserLocation) (*domain.UserLocation, error)
	DeleteUserLocation(ctx context.Context, userID string) error
}
