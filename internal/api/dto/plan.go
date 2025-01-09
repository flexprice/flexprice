package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
)

type CreatePlanRequest struct {
	Name           string                   `json:"name" validate:"required"`
	LookupKey      string                   `json:"lookup_key"`
	Description    string                   `json:"description"`
	InvoiceCadence types.InvoiceCadence     `json:"invoice_cadence"`
	TrialPeriod    int                      `json:"trial_period"`
	Prices         []CreatePlanPriceRequest `json:"prices"`
}

type CreatePlanPriceRequest struct {
	*CreatePriceRequest
}

func (r *CreatePlanRequest) Validate() error {
	err := validator.New().Struct(r)
	if err != nil {
		return err
	}

	for _, price := range r.Prices {
		if err := price.Validate(); err != nil {
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

type CreatePlanResponse struct {
	*plan.Plan
}

type PlanResponse struct {
	*plan.Plan
	Prices []PriceResponse `json:"prices"`
}

type UpdatePlanRequest struct {
	Name           string                   `json:"name" validate:"required"`
	LookupKey      string                   `json:"lookup_key"`
	Description    string                   `json:"description"`
	InvoiceCadence types.InvoiceCadence     `json:"invoice_cadence"`
	TrialPeriod    int                      `json:"trial_period"`
	Prices         []UpdatePlanPriceRequest `json:"prices"`
}

type UpdatePlanPriceRequest struct {
	// The ID of the price to update (present if the price is being updated)
	ID string `json:"id,omitempty"`
	// The price request to update existing price or create new price
	*CreatePriceRequest
}

type ListPlansResponse struct {
	Plans  []*PlanResponse `json:"plans"`
	Total  int             `json:"total"`
	Offset int             `json:"offset"`
	Limit  int             `json:"limit"`
}
