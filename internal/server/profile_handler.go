package server

import (
	"errors"
	"net/http"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/profile"
	"github.com/gin-gonic/gin"
)

func (a *API) handleGetPersonalization(c *gin.Context) {
	preferences, err := a.useCases.Profile.GetPersonalization(c.Request.Context(), currentUser(c).ID)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"personalization": preferences})
}

func (a *API) handleUpdatePersonalization(c *gin.Context) {
	var request struct {
		PreferencesText         *string `json:"preferences_text"`
		LocationEnabledForModel *bool   `json:"location_enabled_for_model"`
		ExpectedVersion         *int64  `json:"expected_version"`
	}
	if err := bindStrictJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	if request.PreferencesText == nil || request.LocationEnabledForModel == nil || request.ExpectedVersion == nil {
		writeAPIError(c, domain.NewValidationError("preferences_text, location_enabled_for_model, and expected_version are required"))
		return
	}

	preferences, err := a.useCases.Profile.UpdatePersonalization(c.Request.Context(), currentUser(c).ID, profile.UpdatePreferencesInput{
		PreferencesText:         *request.PreferencesText,
		LocationEnabledForModel: *request.LocationEnabledForModel,
		ExpectedVersion:         *request.ExpectedVersion,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"personalization": preferences})
}

func (a *API) handleGetLocation(c *gin.Context) {
	location, err := a.useCases.Profile.GetLocation(c.Request.Context(), currentUser(c).ID)
	if errors.Is(err, domain.ErrNotFound) {
		c.Status(http.StatusNoContent)
		return
	}
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"location": location})
}

func (a *API) handleUpdateLocation(c *gin.Context) {
	var request struct {
		Latitude         *float64 `json:"latitude"`
		Longitude        *float64 `json:"longitude"`
		CoordinateSystem *string  `json:"coordinate_system"`
		FormattedAddress *string  `json:"formatted_address"`
		Province         *string  `json:"province"`
		City             *string  `json:"city"`
		District         *string  `json:"district"`
		Adcode           *string  `json:"adcode"`
		POIID            *string  `json:"poi_id"`
		POIName          *string  `json:"poi_name"`
		Source           *string  `json:"source"`
	}
	if err := bindStrictJSON(c, &request); err != nil {
		writeAPIError(c, err)
		return
	}
	if request.Latitude == nil || request.Longitude == nil || request.CoordinateSystem == nil ||
		request.FormattedAddress == nil || request.Province == nil || request.City == nil ||
		request.District == nil || request.Adcode == nil || request.POIID == nil ||
		request.POIName == nil || request.Source == nil {
		writeAPIError(c, domain.NewValidationError("all location fields are required"))
		return
	}

	location, err := a.useCases.Profile.UpdateLocation(c.Request.Context(), currentUser(c).ID, profile.UpdateLocationInput{
		Latitude:         *request.Latitude,
		Longitude:        *request.Longitude,
		CoordinateSystem: *request.CoordinateSystem,
		FormattedAddress: *request.FormattedAddress,
		Province:         *request.Province,
		City:             *request.City,
		District:         *request.District,
		Adcode:           *request.Adcode,
		POIID:            *request.POIID,
		POIName:          *request.POIName,
		Source:           *request.Source,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"location": location})
}

func (a *API) handleDeleteLocation(c *gin.Context) {
	if err := a.useCases.Profile.DeleteLocation(c.Request.Context(), currentUser(c).ID); err != nil {
		writeAPIError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
