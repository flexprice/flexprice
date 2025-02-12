package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
)

type CreatePlanRequest struct {
	Name           string                         `json:"name" validate:"required"`
	LookupKey      string                         `json:"lookup_key"`
	Description    string                         `json:"description"`
	InvoiceCadence types.InvoiceCadence           `json:"invoice_cadence"`
	TrialPeriod    int                            `json:"trial_period"`
	Prices         []CreatePlanPriceRequest       `json:"prices"`
	Entitlements   []CreatePlanEntitlementRequest `json:"entitlements"`
}

type CreatePlanPriceRequest struct {
	*CreatePriceRequest
}

type CreatePlanEntitlementRequest struct {
	*CreateEntitlementRequest
}

func (r *CreatePlanRequest) Validate() error {
	validator := validator.New()
	err := validator.Struct(r)
	if err != nil {
		return err
	}

	for _, price := range r.Prices {
		if err := price.Validate(); err != nil {
			return err
		}
	}

	for _, ent := range r.Entitlements {
		if err := ent.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (r *CreatePlanRequest) ToPlan(ctx context.Context) *plan.Plan {
	plan := &plan.Plan{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		LookupKey:      r.LookupKey,
		Name:           r.Name,
		Description:    r.Description,
		InvoiceCadence: r.InvoiceCadence,
		TrialPeriod:    r.TrialPeriod,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	return plan
}

func (r *CreatePlanEntitlementRequest) ToEntitlement(ctx context.Context, planID string) *entitlement.Entitlement {
	ent := r.CreateEntitlementRequest.ToEntitlement(ctx)
	ent.PlanID = planID
	return ent
}

type CreatePlanResponse struct {
	*plan.Plan
}

type PlanResponse struct {
	*plan.Plan
	Prices       []*PriceResponse       `json:"prices,omitempty"`
	Entitlements []*EntitlementResponse `json:"entitlements,omitempty"`
}

type UpdatePlanRequest struct {
	Name           *string                        `json:"name,omitempty"`
	LookupKey      *string                        `json:"lookup_key,omitempty"`
	Description    *string                        `json:"description,omitempty"`
	InvoiceCadence *types.InvoiceCadence          `json:"invoice_cadence,omitempty"`
	TrialPeriod    *int                           `json:"trial_period,omitempty"`
	Prices         []UpdatePlanPriceRequest       `json:"prices,omitempty"`
	Entitlements   []UpdatePlanEntitlementRequest `json:"entitlements,omitempty"`
}

type UpdatePlanPriceRequest struct {
	// The ID of the price to update (present if the price is being updated)
	ID string `json:"id,omitempty"`
	// The price request to update existing price or create new price
	*CreatePriceRequest
}

type UpdatePlanEntitlementRequest struct {
	// The ID of the entitlement to update (present if the entitlement is being updated)
	ID string `json:"id,omitempty"`
	// The entitlement request to update existing entitlement or create new entitlement
	*CreatePlanEntitlementRequest
}

// ListPlansResponse represents the response for listing plans
type ListPlansResponse = types.ListResponse[*PlanResponse]
