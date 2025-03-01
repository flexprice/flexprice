package dto

import (
	"context"

	"github.com/cockroachdb/errors"

	"github.com/flexprice/flexprice/internal/domain/feature"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
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
		return ierr.NewError("name is required").WithHintf("name is required").Mark(ierr.ErrValidation)
	}

	if err := r.Type.Validate(); err != nil {
		details := map[string]any{}
		var validateErrs validator.ValidationErrors
		if errors.As(err, &validateErrs) {
			for _, err := range validateErrs {
				details[err.Field()] = err.Error()
			}
		}

		return ierr.WithError(err).WithHint("struct validation failed").WithReportableDetails(details).Mark(ierr.ErrValidation)
	}

	if r.Type == types.FeatureTypeMetered {
		if r.MeterID == "" {
			return ierr.NewError("meter_id is required for meter features").WithHint("meter_id is required for meter features").Mark(ierr.ErrValidation)
		}
	}

	if (r.UnitSingular == "" && r.UnitPlural != "") || (r.UnitPlural == "" && r.UnitSingular != "") {
		return ierr.NewError("unit_singular and unit_plural must be set together").WithHint("unit_singular and unit_plural must be set together").Mark(ierr.ErrValidation)
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
