package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// PriceSyncWorkflowInput represents input for the price sync workflow.
// Supports both plan and addon price sync via EntityType + EntityID.
// PlanID is kept for backward compatibility — if EntityID is empty and PlanID is set,
// EntityID is backfilled from PlanID and EntityType is set to PRICE_ENTITY_TYPE_PLAN.
type PriceSyncWorkflowInput struct {
	EntityType    types.PriceEntityType `json:"entity_type"`    // PRICE_ENTITY_TYPE_PLAN or PRICE_ENTITY_TYPE_ADDON
	EntityID      string                `json:"entity_id"`       // plan ID or addon ID
	PlanID        string                `json:"plan_id"`         // deprecated: use EntityID+EntityType
	TenantID      string                `json:"tenant_id"`
	EnvironmentID string                `json:"environment_id"`
	UserID        string                `json:"user_id"`
}

func (p *PriceSyncWorkflowInput) Validate() error {
	// Backward compat: backfill EntityID/EntityType from PlanID
	if p.EntityID == "" && p.PlanID != "" {
		p.EntityID = p.PlanID
		p.EntityType = types.PRICE_ENTITY_TYPE_PLAN
	}

	if p.EntityID == "" {
		return ierr.NewError("entity ID is required").
			WithHint("Provide entity_id (plan ID or addon ID)").
			Mark(ierr.ErrValidation)
	}

	if p.EntityType != types.PRICE_ENTITY_TYPE_PLAN && p.EntityType != types.PRICE_ENTITY_TYPE_ADDON {
		return ierr.NewError("invalid entity type for price sync").
			WithHintf("entity_type must be %s or %s", types.PRICE_ENTITY_TYPE_PLAN, types.PRICE_ENTITY_TYPE_ADDON).
			Mark(ierr.ErrValidation)
	}

	if p.TenantID == "" || p.EnvironmentID == "" || p.UserID == "" {
		return ierr.NewError("tenant ID, environment ID and user ID are required").
			WithHint("Tenant ID, environment ID and user ID are required").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// QuickBooksPriceSyncWorkflowInput represents input for the QuickBooks price sync workflow
type QuickBooksPriceSyncWorkflowInput struct {
	PriceID       string `json:"price_id"`
	PlanID        string `json:"plan_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

func (q *QuickBooksPriceSyncWorkflowInput) Validate() error {
	if q.PriceID == "" || q.PlanID == "" {
		return ierr.NewError("price ID and plan ID are required").
			WithHint("Price ID and Plan ID are required").
			Mark(ierr.ErrValidation)
	}

	if q.TenantID == "" || q.EnvironmentID == "" || q.UserID == "" {
		return ierr.NewError("tenant ID, environment ID and user ID are required").
			WithHint("Tenant ID, environment ID and user ID are required").
			Mark(ierr.ErrValidation)
	}

	return nil
}
