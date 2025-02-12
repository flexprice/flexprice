package entitlement

import (
	"fmt"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// Entitlement represents the benefits a customer gets from a subscription plan
type Entitlement struct {
	ID               string              `json:"id"`
	PlanID           string              `json:"plan_id"`
	FeatureID        string              `json:"feature_id"`
	FeatureType      types.FeatureType   `json:"feature_type"`
	IsEnabled        bool                `json:"is_enabled"`
	UsageLimit       *int64              `json:"usage_limit"`
	UsageResetPeriod types.BillingPeriod `json:"usage_reset_period"`
	IsSoftLimit      bool                `json:"is_soft_limit"`
	StaticValue      string              `json:"static_value"`
	types.BaseModel
}

// Validate performs validation on the entitlement
func (e *Entitlement) Validate() error {
	if e.PlanID == "" {
		return fmt.Errorf("plan_id is required")
	}
	if e.FeatureID == "" {
		return fmt.Errorf("feature_id is required")
	}
	if e.FeatureType == "" {
		return fmt.Errorf("feature_type is required")
	}

	// Validate based on feature type
	switch e.FeatureType {
	case types.FeatureTypeMetered:
		if e.UsageResetPeriod != "" {
			if err := e.UsageResetPeriod.Validate(); err != nil {
				return fmt.Errorf("invalid usage_reset_period: %w", err)
			}
		}
	case types.FeatureTypeStatic:
		if e.StaticValue == "" {
			return fmt.Errorf("static_value is required for static features")
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
		PlanID:           e.PlanID,
		FeatureID:        e.FeatureID,
		FeatureType:      types.FeatureType(e.FeatureType),
		IsEnabled:        e.IsEnabled,
		UsageLimit:       e.UsageLimit,
		UsageResetPeriod: types.BillingPeriod(e.UsageResetPeriod),
		IsSoftLimit:      e.IsSoftLimit,
		StaticValue:      e.StaticValue,
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
