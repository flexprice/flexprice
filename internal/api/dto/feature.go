package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type CreateFeatureRequest struct {
	Name          string               `json:"name" binding:"required"`
	Description   string               `json:"description"`
	LookupKey     string               `json:"lookup_key"`
	Type          types.FeatureType    `json:"type" binding:"required"`
	MeterID       string               `json:"meter_id,omitempty"`
	Meter         *CreateMeterRequest  `json:"meter,omitempty"`
	Metadata      types.Metadata       `json:"metadata,omitempty"`
	UnitSingular  string               `json:"unit_singular,omitempty"`
	UnitPlural    string               `json:"unit_plural,omitempty"`
	ReportingUnit *types.ReportingUnit `json:"reporting_unit,omitempty"`
	AlertSettings *types.AlertSettings `json:"alert_settings,omitempty"`
	// GroupID is the id of the group to add the feature to
	GroupID string `json:"group_id,omitempty"`
}

func (r *CreateFeatureRequest) Validate() error {
	if r.Name == "" {
		return ierr.NewError("name is required").
			WithHint("Name is required").
			Mark(ierr.ErrValidation)
	}

	if err := r.Type.Validate(); err != nil {
		return err
	}

	if r.Type == types.FeatureTypeMetered {
		if r.MeterID == "" && r.Meter == nil {
			return ierr.NewError("either meter_id or meter must be provided").
				WithHint("Please provide meter details to setup a metered feature").
				Mark(ierr.ErrValidation)
		}
		if r.Meter != nil {
			if err := r.Meter.Validate(); err != nil {
				return err
			}
		}
	}

	if (r.UnitSingular == "" && r.UnitPlural != "") || (r.UnitPlural == "" && r.UnitSingular != "") {
		return ierr.NewError("unit_singular and unit_plural must be set together").
			WithHint("Please provide both unit singular and unit plural").
			Mark(ierr.ErrValidation)
	}

	// Reporting (display) unit: when provided, all three must be set; conversion: reporting_unit = unit * conversion_rate
	if r.ReportingUnit != nil {
		if err := r.ReportingUnit.Validate(); err != nil {
			return err
		}
	}

	// Validate alert settings if provided (NO mutation here)
	if r.AlertSettings != nil {
		if err := r.AlertSettings.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (r *CreateFeatureRequest) ToFeature(ctx context.Context) (*feature.Feature, error) {

	feature := &feature.Feature{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_FEATURE),
		Name:          r.Name,
		Description:   r.Description,
		LookupKey:     r.LookupKey,
		Metadata:      r.Metadata,
		Type:          r.Type,
		MeterID:       r.MeterID,
		UnitSingular:  r.UnitSingular,
		UnitPlural:    r.UnitPlural,
		ReportingUnit: r.ReportingUnit,
		GroupID:       r.GroupID,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	if r.AlertSettings != nil {
		// Default alert_enabled to false if not provided
		if r.AlertSettings.AlertEnabled == nil {
			r.AlertSettings.AlertEnabled = lo.ToPtr(false)
		}
		feature.AlertSettings = r.AlertSettings
	}
	if feature.LookupKey == "" {
		feature.LookupKey = types.GenerateLookupKey(r.Name)
	}
	return feature, nil
}

type UpdateFeatureRequest struct {
	Name          *string              `json:"name,omitempty"`
	Description   *string              `json:"description,omitempty"`
	Metadata      *types.Metadata      `json:"metadata,omitempty"`
	UnitSingular  *string              `json:"unit_singular,omitempty"`
	UnitPlural    *string              `json:"unit_plural,omitempty"`
	ReportingUnit *types.ReportingUnit `json:"reporting_unit,omitempty"`
	Filters       *[]meter.Filter      `json:"filters,omitempty"`
	AlertSettings *types.AlertSettings `json:"alert_settings,omitempty"`
	// GroupID is the id of the group to assign the feature to. Pass empty string to clear.
	GroupID *string `json:"group_id,omitempty"`
}

type FeatureResponse struct {
	*feature.Feature
	Meter *MeterResponse `json:"meter,omitempty"`
	// Group is the full group object when the feature belongs to a group (populated in response)
	Group *GroupResponse `json:"group,omitempty"`
}

// ListFeaturesResponse represents a paginated list of features
type ListFeaturesResponse = types.ListResponse[*FeatureResponse] // @name ListFeaturesResponse

// CloneFeatureRequest represents the request to clone a feature within the same environment.
type CloneFeatureRequest struct {
	// Name is required and must be different from the source feature's name
	Name string `json:"name"`
	// LookupKey is required and must be unique across published features
	LookupKey string `json:"lookup_key"`
	// Description overrides the source feature's description when provided
	Description *string `json:"description,omitempty"`
	// Metadata overrides the source feature's metadata when provided
	Metadata types.Metadata `json:"metadata,omitempty"`
}

func (r *CloneFeatureRequest) Validate() error {
	if r.Name == "" {
		return ierr.NewError("name is required for cloned feature").
			WithHint("Please provide a unique name for the cloned feature").
			Mark(ierr.ErrValidation)
	}
	if r.LookupKey == "" {
		return ierr.NewError("lookup_key is required for cloned feature").
			WithHint("Please provide a unique lookup_key for the cloned feature").
			Mark(ierr.ErrValidation)
	}
	return nil
}
