package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/feature"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type CreateFeatureRequest struct {
	Name         string            `json:"name" binding:"required"`
	Description  string            `json:"description"`
	LookupKey    string            `json:"lookup_key"`
	Type         types.FeatureType `json:"type" binding:"required"`
	MeterID      string            `json:"meter_id,omitempty"`
	Metadata     types.Metadata    `json:"metadata,omitempty"`
	UnitSingular string            `json:"unit_singular,omitempty"`
	UnitPlural   string            `json:"unit_plural,omitempty"`
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
		if r.MeterID == "" {
			return ierr.NewError("meter_id is required for metered features").
				WithHint("Please select a valid metered feature").
				Mark(ierr.ErrValidation)
		}
	}

	if (r.UnitSingular == "" && r.UnitPlural != "") || (r.UnitPlural == "" && r.UnitSingular != "") {
		return ierr.NewError("unit_singular and unit_plural must be set together").
			WithHint("Please provide both unit singular and unit plural").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (r *CreateFeatureRequest) ToFeature(ctx context.Context) (*feature.Feature, error) {
	return &feature.Feature{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_FEATURE),
		Name:          r.Name,
		Description:   r.Description,
		LookupKey:     r.LookupKey,
		Metadata:      r.Metadata,
		Type:          r.Type,
		MeterID:       r.MeterID,
		UnitSingular:  r.UnitSingular,
		UnitPlural:    r.UnitPlural,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}, nil
}

type UpdateFeatureRequest struct {
	Name         *string         `json:"name,omitempty"`
	Description  *string         `json:"description,omitempty"`
	Metadata     *types.Metadata `json:"metadata,omitempty"`
	UnitSingular *string         `json:"unit_singular,omitempty"`
	UnitPlural   *string         `json:"unit_plural,omitempty"`
}

type FeatureResponse struct {
	*feature.Feature
	Meter *MeterResponse `json:"meter,omitempty"`
}

// ListFeaturesResponse represents a paginated list of features
type ListFeaturesResponse = types.ListResponse[*FeatureResponse]
