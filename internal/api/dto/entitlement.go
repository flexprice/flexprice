package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/entitlement"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateEntitlementRequest represents the request to create a new entitlement
type CreateEntitlementRequest struct {
	PlanID           string                      `json:"plan_id,omitempty"`
	FeatureID        string                      `json:"feature_id" binding:"required"`
	FeatureType      types.FeatureType           `json:"feature_type" binding:"required"`
	IsEnabled        bool                        `json:"is_enabled"`
	UsageLimit       *int64                      `json:"usage_limit"`
	UsageResetPeriod types.BillingPeriod         `json:"usage_reset_period"`
	IsSoftLimit      bool                        `json:"is_soft_limit"`
	StaticValue      string                      `json:"static_value"`
	EntityType       types.EntitlementEntityType `json:"entity_type"`
	EntityID         string                      `json:"entity_id"`
}

func (r *CreateEntitlementRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.FeatureID == "" {
		return ierr.NewError("feature_id is required").
			WithHint("Feature ID is required").
			Mark(ierr.ErrValidation)
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
			return ierr.NewError("static_value is required for static features").
				WithHint("Static value is required for static features").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

func (r *CreateEntitlementRequest) ToEntitlement(ctx context.Context) *entitlement.Entitlement {
	// If the feature is static or metered, it is by default enabled
	if r.FeatureType == types.FeatureTypeStatic || r.FeatureType == types.FeatureTypeMetered {
		r.IsEnabled = true
	}

	// TODO: This is a temporary fix to maintain backward compatibility
	// We need to remove this once we have a proper entitlement entity type
	if r.PlanID != "" {
		r.EntityType = types.ENTITLEMENT_ENTITY_TYPE_PLAN
		r.EntityID = r.PlanID
	}

	return &entitlement.Entitlement{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT),
		EntityType:       r.EntityType,
		EntityID:         r.EntityID,
		FeatureID:        r.FeatureID,
		FeatureType:      r.FeatureType,
		IsEnabled:        r.IsEnabled,
		UsageLimit:       r.UsageLimit,
		UsageResetPeriod: r.UsageResetPeriod,
		IsSoftLimit:      r.IsSoftLimit,
		StaticValue:      r.StaticValue,
		EnvironmentID:    types.GetEnvironmentID(ctx),
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
