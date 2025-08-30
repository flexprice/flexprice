package entitlement

import (
	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Entitlement represents the benefits a customer gets from a subscription plan
type Entitlement struct {
	ID               string                      `json:"id"`
	EntityType       types.EntitlementEntityType `json:"entity_type"`
	EntityID         string                      `json:"entity_id"`
	FeatureID        string                      `json:"feature_id"`
	FeatureType      types.FeatureType           `json:"feature_type"`
	IsEnabled        bool                        `json:"is_enabled"`
	UsageLimit       *int64                      `json:"usage_limit"`
	UsageResetPeriod types.BillingPeriod         `json:"usage_reset_period"`
	IsSoftLimit      bool                        `json:"is_soft_limit"`
	StaticValue      string                      `json:"static_value"`
	EnvironmentID    string                      `json:"environment_id"`
	DisplayOrder     int                         `json:"display_order"`
	types.BaseModel
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
		ID:               e.ID,
		EntityType:       types.EntitlementEntityType(e.EntityType),
		EntityID:         e.EntityID,
		FeatureID:        e.FeatureID,
		FeatureType:      types.FeatureType(e.FeatureType),
		IsEnabled:        e.IsEnabled,
		UsageLimit:       e.UsageLimit,
		UsageResetPeriod: types.BillingPeriod(e.UsageResetPeriod),
		IsSoftLimit:      e.IsSoftLimit,
		StaticValue:      e.StaticValue,
		EnvironmentID:    e.EnvironmentID,
		DisplayOrder:     e.DisplayOrder,
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
