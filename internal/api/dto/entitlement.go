package dto

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/types"
)

// CreateEntitlementRequest represents the request to create a new entitlement
type CreateEntitlementRequest struct {
	PlanID           string              `json:"plan_id,omitempty"`
	FeatureID        string              `json:"feature_id" binding:"required"`
	FeatureType      types.FeatureType   `json:"feature_type" binding:"required"`
	IsEnabled        bool                `json:"is_enabled"`
	UsageLimit       *int64              `json:"usage_limit"`
	UsageResetPeriod types.BillingPeriod `json:"usage_reset_period"`
	IsSoftLimit      bool                `json:"is_soft_limit"`
	StaticValue      string              `json:"static_value"`
}

func (r *CreateEntitlementRequest) Validate() error {
	if r.FeatureID == "" {
		return errors.New("feature_id is required")
	}

	if err := r.FeatureType.Validate(); err != nil {
		return err
	}

	// Validate based on feature type
	switch r.FeatureType {
	case types.FeatureTypeMetered:
		if r.UsageResetPeriod != "" {
			if err := r.UsageResetPeriod.Validate(); err != nil {
				return err
			}
		}
	case types.FeatureTypeStatic:
		if r.StaticValue == "" {
			return errors.New("static_value is required for static features")
		}
	}

	return nil
}

func (r *CreateEntitlementRequest) ToEntitlement(ctx context.Context) *entitlement.Entitlement {
	return &entitlement.Entitlement{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT),
		PlanID:           r.PlanID,
		FeatureID:        r.FeatureID,
		FeatureType:      r.FeatureType,
		IsEnabled:        r.IsEnabled,
		UsageLimit:       r.UsageLimit,
		UsageResetPeriod: r.UsageResetPeriod,
		IsSoftLimit:      r.IsSoftLimit,
		StaticValue:      r.StaticValue,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
}

// UpdateEntitlementRequest represents the request to update an existing entitlement
type UpdateEntitlementRequest struct {
	IsEnabled        *bool               `json:"is_enabled"`
	UsageLimit       *int64              `json:"usage_limit"`
	UsageResetPeriod types.BillingPeriod `json:"usage_reset_period"`
	IsSoftLimit      *bool               `json:"is_soft_limit"`
	StaticValue      string              `json:"static_value"`
}

// EntitlementResponse represents the response for an entitlement
type EntitlementResponse struct {
	*entitlement.Entitlement
	Feature *FeatureResponse `json:"feature,omitempty"`
	Plan    *PlanResponse    `json:"plan,omitempty"`
}

// ListEntitlementsResponse represents a paginated list of entitlements
type ListEntitlementsResponse = types.ListResponse[*EntitlementResponse]

// EntitlementToResponse converts an entitlement to a response
func EntitlementToResponse(e *entitlement.Entitlement) *EntitlementResponse {
	if e == nil {
		return nil
	}

	return &EntitlementResponse{
		Entitlement: e,
	}
}

// EntitlementsToResponse converts a slice of entitlements to responses
func EntitlementsToResponse(entitlements []*entitlement.Entitlement) []*EntitlementResponse {
	responses := make([]*EntitlementResponse, len(entitlements))
	for i, e := range entitlements {
		responses[i] = EntitlementToResponse(e)
	}
	return responses
}
