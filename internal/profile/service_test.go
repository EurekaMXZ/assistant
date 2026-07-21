package profile

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type stubRepository struct {
	preferences *domain.UserPreferences
	location    *domain.UserLocation
}

func (r *stubRepository) GetUserPreferences(context.Context, string) (*domain.UserPreferences, error) {
	if r.preferences == nil {
		return nil, domain.ErrNotFound
	}
	return r.preferences, nil
}

func (r *stubRepository) UpsertUserPreferences(_ context.Context, preferences domain.UserPreferences, _ int64) (*domain.UserPreferences, error) {
	r.preferences = &preferences
	return r.preferences, nil
}

func (r *stubRepository) GetUserLocation(context.Context, string) (*domain.UserLocation, error) {
	if r.location == nil {
		return nil, domain.ErrNotFound
	}
	return r.location, nil
}

func (r *stubRepository) UpsertUserLocation(_ context.Context, location domain.UserLocation) (*domain.UserLocation, error) {
	r.location = &location
	return r.location, nil
}

func (r *stubRepository) DeleteUserLocation(context.Context, string) error {
	r.location = nil
	return nil
}

func TestGetPreferencesReturnsDefaultsWhenMissing(t *testing.T) {
	service := Service{Repository: &stubRepository{}}
	preferences, err := service.GetPreferences(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if preferences.UserID != "user-1" || preferences.PreferencesText != "" || preferences.LocationEnabledForModel || preferences.Version != 0 {
		t.Fatalf("unexpected defaults: %#v", preferences)
	}
}

func TestUpdatePreferencesValidatesCharacterLength(t *testing.T) {
	service := Service{Repository: &stubRepository{}}
	_, err := service.UpdatePreferences(context.Background(), "user-1", UpdatePreferencesInput{
		PreferencesText: strings.Repeat("好", MaxPreferencesTextLength+1),
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("error = %v, want validation error", err)
	}
}

func TestUpdateLocationValidation(t *testing.T) {
	valid := UpdateLocationInput{
		Latitude:         31.2304,
		Longitude:        121.4737,
		CoordinateSystem: domain.CoordinateSystemGCJ02,
		FormattedAddress: "上海市黄浦区人民大道",
		Province:         "上海市",
		City:             "上海市",
		District:         "黄浦区",
		Adcode:           "310101",
		Source:           domain.LocationSourceMap,
	}
	tests := []struct {
		name   string
		mutate func(*UpdateLocationInput)
	}{
		{name: "non-finite latitude", mutate: func(input *UpdateLocationInput) { input.Latitude = math.NaN() }},
		{name: "longitude range", mutate: func(input *UpdateLocationInput) { input.Longitude = 181 }},
		{name: "coordinate system", mutate: func(input *UpdateLocationInput) { input.CoordinateSystem = "wgs84" }},
		{name: "coordinate system whitespace", mutate: func(input *UpdateLocationInput) { input.CoordinateSystem = " gcj02" }},
		{name: "adcode", mutate: func(input *UpdateLocationInput) { input.Adcode = "31010A" }},
		{name: "adcode whitespace", mutate: func(input *UpdateLocationInput) { input.Adcode = " 310101" }},
		{name: "source", mutate: func(input *UpdateLocationInput) { input.Source = "manual" }},
		{name: "source whitespace", mutate: func(input *UpdateLocationInput) { input.Source = "map " }},
		{name: "address length", mutate: func(input *UpdateLocationInput) {
			input.FormattedAddress = strings.Repeat("a", maxFormattedAddressLength+1)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			service := Service{Repository: &stubRepository{}}
			if _, err := service.UpdateLocation(context.Background(), "user-1", input); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("error = %v, want validation error", err)
			}
		})
	}
}

func TestUpdateLocationNormalizesAddressFields(t *testing.T) {
	repository := &stubRepository{}
	service := Service{Repository: repository}
	location, err := service.UpdateLocation(context.Background(), "user-1", UpdateLocationInput{
		Latitude:         39.9042,
		Longitude:        116.4074,
		CoordinateSystem: domain.CoordinateSystemGCJ02,
		FormattedAddress: "  北京市东城区  ",
		Adcode:           "110101",
		Source:           domain.LocationSourceSearch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if location.UserID != "user-1" || location.FormattedAddress != "北京市东城区" {
		t.Fatalf("unexpected location: %#v", location)
	}
}
