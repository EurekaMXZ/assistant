package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const userPreferencesColumns = `
	user_id::text, preferences_text, location_enabled_for_model, version, created_at, updated_at`

const userLocationColumns = `
	user_id::text, latitude, longitude, coordinate_system, formatted_address,
	province, city, district, adcode, poi_id, poi_name, source, created_at, updated_at`

type ProfileRepository struct {
	pool *pgxpool.Pool
}

func NewProfileRepository(pool *pgxpool.Pool) *ProfileRepository {
	return &ProfileRepository{pool: pool}
}

func (r *ProfileRepository) GetUserPreferences(ctx context.Context, userID string) (*domain.UserPreferences, error) {
	preferences, err := scanUserPreferences(r.pool.QueryRow(ctx, `
		SELECT `+userPreferencesColumns+`
		FROM user_preferences
		WHERE user_id = $1::uuid
	`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user preferences: %w", err)
	}
	return preferences, nil
}

func (r *ProfileRepository) UpsertUserPreferences(ctx context.Context, preferences domain.UserPreferences, expectedVersion int64) (*domain.UserPreferences, error) {
	stored, err := scanUserPreferences(r.pool.QueryRow(ctx, `
		INSERT INTO user_preferences (user_id, preferences_text, location_enabled_for_model)
		SELECT $1::uuid, $2, $3
		WHERE $4 = 0 OR EXISTS (
			SELECT 1
			FROM user_preferences
			WHERE user_id = $1::uuid AND version = $4
		)
		ON CONFLICT (user_id) DO UPDATE SET
			preferences_text = EXCLUDED.preferences_text,
			location_enabled_for_model = EXCLUDED.location_enabled_for_model,
			version = user_preferences.version + 1
		WHERE user_preferences.version = $4
		RETURNING `+userPreferencesColumns,
		preferences.UserID, preferences.PreferencesText, preferences.LocationEnabledForModel, expectedVersion))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("upsert user preferences: %w", err)
	}
	return stored, nil
}

func (r *ProfileRepository) GetUserLocation(ctx context.Context, userID string) (*domain.UserLocation, error) {
	location, err := scanUserLocation(r.pool.QueryRow(ctx, `
		SELECT `+userLocationColumns+`
		FROM user_locations
		WHERE user_id = $1::uuid
	`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user location: %w", err)
	}
	return location, nil
}

func (r *ProfileRepository) UpsertUserLocation(ctx context.Context, location domain.UserLocation) (*domain.UserLocation, error) {
	stored, err := scanUserLocation(r.pool.QueryRow(ctx, `
		INSERT INTO user_locations (
			user_id, latitude, longitude, coordinate_system, formatted_address,
			province, city, district, adcode, poi_id, poi_name, source
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (user_id) DO UPDATE SET
			latitude = EXCLUDED.latitude,
			longitude = EXCLUDED.longitude,
			coordinate_system = EXCLUDED.coordinate_system,
			formatted_address = EXCLUDED.formatted_address,
			province = EXCLUDED.province,
			city = EXCLUDED.city,
			district = EXCLUDED.district,
			adcode = EXCLUDED.adcode,
			poi_id = EXCLUDED.poi_id,
			poi_name = EXCLUDED.poi_name,
			source = EXCLUDED.source
		RETURNING `+userLocationColumns,
		location.UserID, location.Latitude, location.Longitude, location.CoordinateSystem,
		location.FormattedAddress, location.Province, location.City, location.District,
		location.Adcode, location.POIID, location.POIName, location.Source))
	if err != nil {
		return nil, fmt.Errorf("upsert user location: %w", err)
	}
	return stored, nil
}

func (r *ProfileRepository) DeleteUserLocation(ctx context.Context, userID string) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM user_locations WHERE user_id = $1::uuid`, userID); err != nil {
		return fmt.Errorf("delete user location: %w", err)
	}
	return nil
}

func scanUserPreferences(row scanRow) (*domain.UserPreferences, error) {
	var preferences domain.UserPreferences
	if err := row.Scan(
		&preferences.UserID,
		&preferences.PreferencesText,
		&preferences.LocationEnabledForModel,
		&preferences.Version,
		&preferences.CreatedAt,
		&preferences.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &preferences, nil
}

func scanUserLocation(row scanRow) (*domain.UserLocation, error) {
	var location domain.UserLocation
	if err := row.Scan(
		&location.UserID,
		&location.Latitude,
		&location.Longitude,
		&location.CoordinateSystem,
		&location.FormattedAddress,
		&location.Province,
		&location.City,
		&location.District,
		&location.Adcode,
		&location.POIID,
		&location.POIName,
		&location.Source,
		&location.CreatedAt,
		&location.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &location, nil
}
