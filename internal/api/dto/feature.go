package dto

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/types"
)

type CreateFeatureRequest struct {
	Name         string            `json:"name" binding:"required"`
	Description  string            `json:"description"`
	LookupKey    string            `json:"lookup_key" binding:"required"`
	Type         types.FeatureType `json:"type" binding:"required"`
	MeterID      string            `json:"meter_id,omitempty"`
	Metadata     types.Metadata    `json:"metadata,omitempty"`
	UnitSingular string            `json:"unit_singular,omitempty"`
	UnitPlural   string            `json:"unit_plural,omitempty"`
}

func (r *CreateFeatureRequest) Validate() error {
	if r.Name == "" {
		return errors.New("name is required")
	}

	if r.LookupKey == "" {
		return errors.New("lookup_key is required")
	}

	if err := r.Type.Validate(); err != nil {
		return err
	}

	if r.Type == types.FeatureTypeMetered {
		if r.MeterID == "" {
			return errors.New("meter_id is required for meter features")
		}
	}

	if (r.UnitSingular == "" && r.UnitPlural != "") || (r.UnitPlural == "" && r.UnitSingular != "") {
		return errors.New("unit_singular and unit_plural must be set together")
	}

	return nil
}

func (r *CreateFeatureRequest) ToFeature(ctx context.Context) (*feature.Feature, error) {
	return &feature.Feature{
		ID:           types.GenerateUUIDWithPrefix(types.UUID_PREFIX_FEATURE),
		Name:         r.Name,
		Description:  r.Description,
		LookupKey:    r.LookupKey,
		Metadata:     r.Metadata,
		Type:         r.Type,
		MeterID:      r.MeterID,
		UnitSingular: r.UnitSingular,
		UnitPlural:   r.UnitPlural,
		BaseModel:    types.GetDefaultBaseModel(ctx),
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
