package entitlement

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// Entitlement represents the benefits a customer gets from a subscription plan
type Entitlement struct {
	ID                  string                            `json:"id"`
	EntityType          types.EntitlementEntityType       `json:"entity_type"`
	EntityID            string                            `json:"entity_id"`
	FeatureID           string                            `json:"feature_id"`
	FeatureType         types.FeatureType                 `json:"feature_type"`
	IsEnabled           bool                              `json:"is_enabled"`
	UsageLimit          *int64                            `json:"usage_limit"`
	UsageResetPeriod    types.EntitlementUsageResetPeriod `json:"usage_reset_period"`
	IsSoftLimit         bool                              `json:"is_soft_limit"`
	StaticValue         string                            `json:"static_value"`
	EnvironmentID       string                            `json:"environment_id"`
	DisplayOrder        int                               `json:"display_order"`
	ParentEntitlementID *string                           `json:"parent_entitlement_id,omitempty"`
	StartDate           *time.Time                        `json:"start_date,omitempty"`
	EndDate             *time.Time                        `json:"end_date,omitempty"`
	types.BaseModel
}

// EntitlementCloneOverrides holds optional overrides for CopyWith. Nil fields mean "keep existing value".
type EntitlementCloneOverrides struct {
	ID         *string
	EntityType *types.EntitlementEntityType
	EntityID   *string
	BaseModel  *types.BaseModel
}

// CopyWith returns a shallow copy of the entitlement with optional overrides applied.
// Pointer fields on the original (UsageLimit, ParentEntitlementID, StartDate, EndDate) are shallow-copied.
// If BaseModel is not in overrides, uses types.GetDefaultBaseModel(ctx).
func (e *Entitlement) CopyWith(ctx context.Context, overrides *EntitlementCloneOverrides) *Entitlement {
	if e == nil {
		return nil
	}
	out := lo.FromPtr(e)
	if overrides == nil {
		return lo.ToPtr(out)
	}
	if overrides.ID != nil {
		out.ID = lo.FromPtr(overrides.ID)
	}
	if overrides.EntityType != nil {
		out.EntityType = lo.FromPtr(overrides.EntityType)
	}
	if overrides.EntityID != nil {
		out.EntityID = lo.FromPtr(overrides.EntityID)
	}
	if overrides.BaseModel != nil {
		out.BaseModel = lo.FromPtr(overrides.BaseModel)
	} else {
		out.BaseModel = types.GetDefaultBaseModel(ctx)
	}
	return lo.ToPtr(out)
}

// Validate performs validation on the entitlement
func (e *Entitlement) Validate() error {
	if e.EntityType == "" {
		return ierr.NewError("entity_type is required").
			WithHint("Please provide a valid entity type").
			Mark(ierr.ErrValidation)
	}
	if err := e.EntityType.Validate(); err != nil {
		return ierr.WithError(err).
			WithHint("Invalid entity type").
			Mark(ierr.ErrValidation)
	}
	if e.FeatureID == "" {
		return ierr.NewError("feature_id is required").
			WithHint("Please provide a valid feature ID").
			Mark(ierr.ErrValidation)
	}
	if e.FeatureType == "" {
		return ierr.NewError("feature_type is required").
			WithHint("Please specify the feature type").
			Mark(ierr.ErrValidation)
	}

	// Validate based on feature type
	switch e.FeatureType {
	case types.FeatureTypeMetered:
		if e.UsageResetPeriod != "" {
			if err := e.UsageResetPeriod.Validate(); err != nil {
				return ierr.WithError(err).
					WithHint("Invalid usage reset period").
					WithReportableDetails(map[string]interface{}{
						"usage_reset_period": e.UsageResetPeriod,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	case types.FeatureTypeStatic:
		if e.StaticValue == "" {
			return ierr.NewError("static_value is required for static features").
				WithHint("Please provide a static value for this feature").
				WithReportableDetails(map[string]interface{}{
					"feature_type": e.FeatureType,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// FromEnt converts ent.Entitlement to domain Entitlement
func FromEnt(e *ent.Entitlement) *Entitlement {
	if e == nil {
		return nil
	}

	return &Entitlement{
		ID:                  e.ID,
		EntityType:          types.EntitlementEntityType(e.EntityType),
		EntityID:            e.EntityID,
		FeatureID:           e.FeatureID,
		FeatureType:         types.FeatureType(e.FeatureType),
		IsEnabled:           e.IsEnabled,
		UsageLimit:          e.UsageLimit,
		UsageResetPeriod:    types.EntitlementUsageResetPeriod(e.UsageResetPeriod),
		IsSoftLimit:         e.IsSoftLimit,
		StaticValue:         e.StaticValue,
		EnvironmentID:       e.EnvironmentID,
		DisplayOrder:        e.DisplayOrder,
		ParentEntitlementID: e.ParentEntitlementID,
		StartDate:           e.StartDate,
		EndDate:             e.EndDate,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

// FromEntList converts []*ent.Entitlement to []*Entitlement
func FromEntList(list []*ent.Entitlement) []*Entitlement {
	result := make([]*Entitlement, len(list))
	for i, e := range list {
		result[i] = FromEnt(e)
	}
	return result
}
