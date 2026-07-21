package profile

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

const (
	MaxPreferencesTextLength  = 8000
	maxFormattedAddressLength = 500
	maxRegionNameLength       = 100
	maxPOIIDLength            = 128
	maxPOINameLength          = 200
)

var adcodePattern = regexp.MustCompile(`^[0-9]{6}$`)

type UpdatePreferencesInput struct {
	PreferencesText         string
	LocationEnabledForModel bool
	ExpectedVersion         int64
}

type UpdateLocationInput struct {
	Latitude         float64
	Longitude        float64
	CoordinateSystem string
	FormattedAddress string
	Province         string
	City             string
	District         string
	Adcode           string
	POIID            string
	POIName          string
	Source           string
}

type Service struct {
	Repository Repository
}

func (s *Service) GetPreferences(ctx context.Context, userID string) (*domain.UserPreferences, error) {
	preferences, err := s.Repository.GetUserPreferences(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return &domain.UserPreferences{UserID: userID}, nil
	}
	return preferences, err
}

func (s *Service) UpdatePreferences(ctx context.Context, userID string, input UpdatePreferencesInput) (*domain.UserPreferences, error) {
	if err := validateText("preferences_text", input.PreferencesText, MaxPreferencesTextLength); err != nil {
		return nil, err
	}
	if input.ExpectedVersion < 0 {
		return nil, domain.NewValidationError("expected_version must be non-negative")
	}
	return s.Repository.UpsertUserPreferences(ctx, domain.UserPreferences{
		UserID:                  userID,
		PreferencesText:         input.PreferencesText,
		LocationEnabledForModel: input.LocationEnabledForModel,
	}, input.ExpectedVersion)
}

func (s *Service) GetLocation(ctx context.Context, userID string) (*domain.UserLocation, error) {
	return s.Repository.GetUserLocation(ctx, userID)
}

func (s *Service) UpdateLocation(ctx context.Context, userID string, input UpdateLocationInput) (*domain.UserLocation, error) {
	location, err := normalizeLocation(userID, input)
	if err != nil {
		return nil, err
	}
	return s.Repository.UpsertUserLocation(ctx, location)
}

func (s *Service) DeleteLocation(ctx context.Context, userID string) error {
	return s.Repository.DeleteUserLocation(ctx, userID)
}

func normalizeLocation(userID string, input UpdateLocationInput) (domain.UserLocation, error) {
	if math.IsNaN(input.Latitude) || math.IsInf(input.Latitude, 0) || input.Latitude < -90 || input.Latitude > 90 {
		return domain.UserLocation{}, domain.NewValidationError("latitude must be finite and between -90 and 90")
	}
	if math.IsNaN(input.Longitude) || math.IsInf(input.Longitude, 0) || input.Longitude < -180 || input.Longitude > 180 {
		return domain.UserLocation{}, domain.NewValidationError("longitude must be finite and between -180 and 180")
	}
	coordinateSystem := input.CoordinateSystem
	if coordinateSystem != domain.CoordinateSystemGCJ02 {
		return domain.UserLocation{}, domain.NewValidationError("coordinate_system must be gcj02")
	}
	source := input.Source
	switch source {
	case domain.LocationSourceMap, domain.LocationSourceSearch, domain.LocationSourceGeolocation:
	default:
		return domain.UserLocation{}, domain.NewValidationError("source must be map, search, or geolocation")
	}

	formattedAddress := strings.TrimSpace(input.FormattedAddress)
	province := strings.TrimSpace(input.Province)
	city := strings.TrimSpace(input.City)
	district := strings.TrimSpace(input.District)
	adcode := input.Adcode
	poiID := strings.TrimSpace(input.POIID)
	poiName := strings.TrimSpace(input.POIName)
	for _, field := range []struct {
		name  string
		value string
		max   int
	}{
		{name: "formatted_address", value: formattedAddress, max: maxFormattedAddressLength},
		{name: "province", value: province, max: maxRegionNameLength},
		{name: "city", value: city, max: maxRegionNameLength},
		{name: "district", value: district, max: maxRegionNameLength},
		{name: "poi_id", value: poiID, max: maxPOIIDLength},
		{name: "poi_name", value: poiName, max: maxPOINameLength},
	} {
		if err := validateText(field.name, field.value, field.max); err != nil {
			return domain.UserLocation{}, err
		}
	}
	if adcode != "" && !adcodePattern.MatchString(adcode) {
		return domain.UserLocation{}, domain.NewValidationError("adcode must be empty or exactly 6 digits")
	}

	return domain.UserLocation{
		UserID:           userID,
		Latitude:         input.Latitude,
		Longitude:        input.Longitude,
		CoordinateSystem: coordinateSystem,
		FormattedAddress: formattedAddress,
		Province:         province,
		City:             city,
		District:         district,
		Adcode:           adcode,
		POIID:            poiID,
		POIName:          poiName,
		Source:           source,
	}, nil
}

func validateText(field string, value string, maximum int) error {
	if !utf8.ValidString(value) {
		return domain.NewValidationError(field + " must be valid UTF-8")
	}
	if utf8.RuneCountInString(value) > maximum {
		return domain.NewValidationError(field + " is too long")
	}
	return nil
}
