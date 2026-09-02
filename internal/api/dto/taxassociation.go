package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	taxassociation "github.com/flexprice/flexprice/internal/domain/taxassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
)

type CreateTaxAssociationRequest struct {
	TaxRateCode        string                  `json:"tax_rate_code" binding:"required"`
	EntityType         types.TaxRateEntityType `json:"entity_type" binding:"omitempty"`
	EntityID           string                  `json:"entity_id" binding:"omitempty"`
	ExternalCustomerID string                  `json:"external_customer_id" binding:"omitempty"`
	Priority           int                     `json:"priority" binding:"omitempty"`
	Currency           string                  `json:"currency" binding:"omitempty"`
	AutoApply          bool                    `json:"auto_apply" binding:"omitempty"`
	Metadata           map[string]string       `json:"metadata" binding:"omitempty"`
	// StartDate sets when this association becomes active. Defaults to now if omitted.
	StartDate *time.Time `json:"start_date,omitempty"`
	// EndDate sets when this association expires. Must be after StartDate when both are provided.
	EndDate *time.Time `json:"end_date,omitempty"`
	// TaxBehavior is inclusive or exclusive. Settable at any level. If left empty on a
	// subscription-level association, it resolves from the currency default at creation
	// time (internal/types.DefaultTaxBehaviorForCurrency) — tenant/customer-level templates
	// only need this set explicitly if the tenant wants one; otherwise it stays null and is
	// resolved when the template is copied down to a subscription.
	TaxBehavior *types.TaxBehavior `json:"tax_behavior,omitempty"`
}

func (r *CreateTaxAssociationRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.TaxRateCode == "" {
		return ierr.NewError("tax_rate_code is required").
			WithHint("Tax rate ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	if r.Priority < 0 {
		return ierr.NewError("priority cannot be less than 0").
			WithHint("Priority cannot be less than 0").
			Mark(ierr.ErrValidation)
	}

	if r.EntityType != "" {
		if err := r.EntityType.Validate(); err != nil {
			return err
		}
	}

	if r.StartDate != nil && r.EndDate != nil && !r.EndDate.After(*r.StartDate) {
		return ierr.NewError("end_date must be after start_date").
			WithHint("Provide an end_date that is strictly after start_date").
			Mark(ierr.ErrValidation)
	}

	if r.TaxBehavior != nil {
		if err := r.TaxBehavior.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (r *CreateTaxAssociationRequest) ToTaxAssociation(ctx context.Context, taxRateID string) *taxassociation.TaxAssociation {
	startDate := time.Now().UTC()
	if r.StartDate != nil {
		startDate = r.StartDate.UTC()
	}
	ta := &taxassociation.TaxAssociation{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_ASSOCIATION),
		TaxRateID:     taxRateID,
		EntityType:    r.EntityType,
		EntityID:      r.EntityID,
		Priority:      r.Priority,
		AutoApply:     r.AutoApply,
		Currency:      r.Currency,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
		Metadata:      r.Metadata,
		StartDate:     startDate,
		TaxBehavior:   r.TaxBehavior,
	}
	if r.EndDate != nil {
		ta.EndDate = lo.ToPtr(r.EndDate.UTC())
	}
	return ta
}

type TaxAssociationUpdateRequest struct {
	Priority    *int               `json:"priority" binding:"omitempty"`
	AutoApply   *bool              `json:"auto_apply" binding:"omitempty"`
	Metadata    *map[string]string `json:"metadata" binding:"omitempty"`
	TaxBehavior *types.TaxBehavior `json:"tax_behavior,omitempty"`
}

func (r *TaxAssociationUpdateRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.Priority != nil && *r.Priority < 0 {
		return ierr.NewError("priority cannot be less than 0").
			WithHint("Priority cannot be less than 0").
			Mark(ierr.ErrValidation)
	}

	if r.TaxBehavior != nil {
		if err := r.TaxBehavior.Validate(); err != nil {
			return err
		}
	}

	return nil
}

type LinkTaxRateToEntityRequest struct {
	TaxRateOverrides        []*TaxRateOverride        `json:"tax_rate_overrides" binding:"omitempty"`
	ExistingTaxAssociations []*TaxAssociationResponse `json:"existing_tax_associations" binding:"omitempty"`
	EntityType              types.TaxRateEntityType   `json:"entity_type" binding:"required" default:"tenant"`
	EntityID                string                    `json:"entity_id" binding:"required"`
}

func (r *LinkTaxRateToEntityRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	return nil
}

// TaxAssociationResponse represents the response for tax association operations
type TaxAssociationResponse struct {
	taxassociation.TaxAssociation
	TaxRate *TaxRateResponse `json:"tax_rate,omitempty"`
}

// ToTaxAssociationResponse converts a domain TaxConfig to a TaxAssociationResponse
func ToTaxAssociationResponse(tc *taxassociation.TaxAssociation) *TaxAssociationResponse {
	if tc == nil {
		return nil
	}

	return &TaxAssociationResponse{
		TaxAssociation: lo.FromPtr(tc),
	}
}

func (r *TaxAssociationResponse) WithTaxRate(taxRate *TaxRateResponse) *TaxAssociationResponse {
	r.TaxRate = taxRate
	return r
}

// ListTaxAssociationsResponse represents the response for listing tax associations
type ListTaxAssociationsResponse = types.ListResponse[*TaxAssociationResponse] // @name ListTaxAssociationsResponse

// TaxRateWithBehavior pairs a tax rate definition with the tax_behavior resolved for this
// invoice's use of it. Behavior lives on the association/override, not on TaxRate itself
// rather than on the rate, so it cannot be carried on TaxRateResponse — every consumer of a resolved rate
// set (invoice tax preparation, calculation, preview) uses this instead.
type TaxRateWithBehavior struct {
	*TaxRateResponse
	TaxBehavior types.TaxBehavior `json:"tax_behavior"`
}

// InvoiceTaxRates is everything invoice tax computation needs about a customer's tax setup:
// the rates that apply with their behavior resolved, and whether the customer is exempt.
// Both are resolved together, once, by TaxService.PrepareTaxRatesForInvoice — exemption is a
// property of the customer the rates were resolved for, so carrying it separately just
// invites the two to be paired up wrongly.
//
// Exemption never changes which rates resolve or what they compute; it only zeroes what is
// charged, at the end.
type InvoiceTaxRates struct {
	Rates  []*TaxRateWithBehavior `json:"rates,omitempty"`
	Exempt bool                   `json:"exempt"`
}

// NewInvoiceTaxRates pairs resolved rates with the customer they will be billed to, so invoice
// computation gets both together. cust must be read fresh for this invoice: tax treatment is
// never cached, so an invoice reflects the exemption status at the time it was generated.
func NewInvoiceTaxRates(rates []*TaxRateWithBehavior, cust *customer.Customer) *InvoiceTaxRates {
	return &InvoiceTaxRates{
		Rates:  rates,
		Exempt: cust != nil && cust.TaxTreatment == types.TaxTreatmentExempt,
	}
}

func (t *InvoiceTaxRates) GetRates() []*TaxRateWithBehavior {
	if t == nil {
		return nil
	}
	return t.Rates
}

func (t *InvoiceTaxRates) IsExempt() bool {
	if t == nil {
		return false
	}
	return t.Exempt
}

// TaxRateOverride represents a tax rate override for a specific entity
// This is used to override the tax rate for a specific entity i.e if you give `tax_overrides` in the create customer request it will link the tax rate to the customer else it will inherit the tenant tax rate,
// It links an existing tax rate to the entity
// The priority and auto apply fields are used to determine the order of the tax rates
type TaxRateOverride struct {
	TaxRateCode string             `json:"tax_rate_code" binding:"required"`
	Priority    int                `json:"priority" binding:"omitempty"`
	Currency    string             `json:"currency" binding:"required"`
	AutoApply   bool               `json:"auto_apply" binding:"omitempty" default:"true"`
	Metadata    map[string]string  `json:"metadata" binding:"omitempty"`
	TaxBehavior *types.TaxBehavior `json:"tax_behavior,omitempty"`
}

func (tr *TaxRateOverride) Validate() error {
	if err := validator.ValidateRequest(tr); err != nil {
		return err
	}

	if tr.Priority < 0 {
		return ierr.NewError("priority cannot be less than 0").
			WithHint("Priority cannot be less than 0").
			Mark(ierr.ErrValidation)
	}

	if tr.TaxBehavior != nil {
		if err := tr.TaxBehavior.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (tr *TaxRateOverride) ToTaxAssociationRequest(_ context.Context, entityID string, entityType types.TaxRateEntityType) *CreateTaxAssociationRequest {
	return &CreateTaxAssociationRequest{
		TaxRateCode: tr.TaxRateCode,
		EntityType:  entityType,
		EntityID:    entityID,
		Priority:    tr.Priority,
		AutoApply:   tr.AutoApply,
		Currency:    tr.Currency,
		Metadata:    tr.Metadata,
		TaxBehavior: tr.TaxBehavior,
	}
}
