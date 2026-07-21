package domain

import "time"

const (
	CoordinateSystemGCJ02 = "gcj02"

	LocationSourceMap         = "map"
	LocationSourceSearch      = "search"
	LocationSourceGeolocation = "geolocation"
)

type UserPreferences struct {
	UserID                  string    `json:"user_id"`
	PreferencesText         string    `json:"preferences_text"`
	LocationEnabledForModel bool      `json:"location_enabled_for_model"`
	Version                 int64     `json:"version"`
	CreatedAt               time.Time `json:"created_at,omitzero"`
	UpdatedAt               time.Time `json:"updated_at,omitzero"`
}

type UserLocation struct {
	UserID           string    `json:"user_id"`
	Latitude         float64   `json:"latitude"`
	Longitude        float64   `json:"longitude"`
	CoordinateSystem string    `json:"coordinate_system"`
	FormattedAddress string    `json:"formatted_address"`
	Province         string    `json:"province"`
	City             string    `json:"city"`
	District         string    `json:"district"`
	Adcode           string    `json:"adcode"`
	POIID            string    `json:"poi_id"`
	POIName          string    `json:"poi_name"`
	Source           string    `json:"source"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
