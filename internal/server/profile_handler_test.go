package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/profile"
)

func profileTestAuth(context.Context, string) (*domain.User, error) {
	return &domain.User{ID: "authenticated-user", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
}

func TestHandleUpdatePersonalizationUsesAuthenticatedUser(t *testing.T) {
	called := false
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: profileTestAuth},
		Profile: ProfileUseCases{UpdatePersonalization: func(_ context.Context, userID string, input profile.UpdatePreferencesInput) (*domain.UserPreferences, error) {
			called = true
			if userID != "authenticated-user" {
				t.Fatalf("user ID = %q", userID)
			}
			if input.PreferencesText != "回答简洁" || !input.LocationEnabledForModel || input.ExpectedVersion != 3 {
				t.Fatalf("unexpected input: %#v", input)
			}
			return &domain.UserPreferences{UserID: userID, PreferencesText: input.PreferencesText, LocationEnabledForModel: true, Version: 1}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/profile/personalization", strings.NewReader(`{"preferences_text":"回答简洁","location_enabled_for_model":true,"expected_version":3}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !called {
		t.Fatalf("status = %d, called = %v, body = %q", rec.Code, called, rec.Body.String())
	}
}

func TestHandleUpdatePersonalizationRejectsUserIDField(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: profileTestAuth},
		Profile: ProfileUseCases{UpdatePersonalization: func(context.Context, string, profile.UpdatePreferencesInput) (*domain.UserPreferences, error) {
			t.Fatal("unexpected update call")
			return nil, nil
		}},
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/profile/personalization", strings.NewReader(`{"user_id":"other-user","preferences_text":"x","location_enabled_for_model":false,"expected_version":0}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdatePersonalizationRequiresEveryField(t *testing.T) {
	for name, body := range map[string]string{
		"empty object":              `{}`,
		"preferences text":          `{"location_enabled_for_model":false,"expected_version":0}`,
		"location model permission": `{"preferences_text":"","expected_version":0}`,
		"expected version":          `{"preferences_text":"","location_enabled_for_model":false}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := newTestServer(UseCases{
				Auth: AuthUseCases{AuthenticateAccessToken: profileTestAuth},
				Profile: ProfileUseCases{UpdatePersonalization: func(context.Context, string, profile.UpdatePreferencesInput) (*domain.UserPreferences, error) {
					t.Fatal("unexpected update call")
					return nil, nil
				}},
			})
			req := httptest.NewRequest(http.MethodPut, "/api/v1/profile/personalization", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer token")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d, body = %q", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestHandleUpdateLocationRequiresEveryField(t *testing.T) {
	valid := map[string]any{
		"latitude": 31.2304, "longitude": 121.4737, "coordinate_system": "gcj02",
		"formatted_address": "", "province": "", "city": "", "district": "", "adcode": "",
		"poi_id": "", "poi_name": "", "source": "map",
	}
	fields := []string{"latitude", "longitude", "coordinate_system", "formatted_address", "province", "city", "district", "adcode", "poi_id", "poi_name", "source"}
	for _, field := range fields {
		t.Run("missing "+field, func(t *testing.T) {
			payload := make(map[string]any, len(valid)-1)
			for key, value := range valid {
				if key != field {
					payload[key] = value
				}
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			srv := newTestServer(UseCases{
				Auth: AuthUseCases{AuthenticateAccessToken: profileTestAuth},
				Profile: ProfileUseCases{UpdateLocation: func(context.Context, string, profile.UpdateLocationInput) (*domain.UserLocation, error) {
					t.Fatal("unexpected update call")
					return nil, nil
				}},
			})
			req := httptest.NewRequest(http.MethodPut, "/api/v1/profile/location", strings.NewReader(string(encoded)))
			req.Header.Set("Authorization", "Bearer token")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d, body = %q", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestHandleUpdateLocationRejectsEmptyObject(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: profileTestAuth},
		Profile: ProfileUseCases{UpdateLocation: func(context.Context, string, profile.UpdateLocationInput) (*domain.UserLocation, error) {
			t.Fatal("unexpected update call")
			return nil, nil
		}},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/profile/location", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleUpdateLocationPreservesPresentZeroCoordinates(t *testing.T) {
	called := false
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: profileTestAuth},
		Profile: ProfileUseCases{UpdateLocation: func(_ context.Context, userID string, input profile.UpdateLocationInput) (*domain.UserLocation, error) {
			called = true
			if userID != "authenticated-user" || input.Latitude != 0 || input.Longitude != 0 || input.CoordinateSystem != domain.CoordinateSystemGCJ02 {
				t.Fatalf("unexpected location input: user=%q input=%#v", userID, input)
			}
			return &domain.UserLocation{UserID: userID, Latitude: input.Latitude, Longitude: input.Longitude, CoordinateSystem: input.CoordinateSystem, Source: input.Source}, nil
		}},
	})
	body := `{"latitude":0,"longitude":0,"coordinate_system":"gcj02","formatted_address":"","province":"","city":"","district":"","adcode":"","poi_id":"","poi_name":"","source":"map"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/profile/location", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("status = %d, called = %v, body = %q", rec.Code, called, rec.Body.String())
	}
}

func TestHandleGetMissingLocationReturnsNoContent(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: profileTestAuth},
		Profile: ProfileUseCases{GetLocation: func(_ context.Context, userID string) (*domain.UserLocation, error) {
			if userID != "authenticated-user" {
				t.Fatalf("user ID = %q", userID)
			}
			return nil, domain.ErrNotFound
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/profile/location", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
}
