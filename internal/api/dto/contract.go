package dto

import (
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type ContractResponse struct {
	ID               string                         `json:"id"`
	TenantID         string                         `json:"tenant_id"`
	EnvironmentID    string                         `json:"environment_id"`
	CustomerID       string                         `json:"customer_id"`
	Currency         string                         `json:"currency"`
	TermStart        *time.Time                     `json:"term_start,omitempty"`
	TermEnd          *time.Time                     `json:"term_end,omitempty"`
	EndedAt          *time.Time                     `json:"ended_at,omitempty"`
	ContractStatus   types.ContractStatus           `json:"contract_status"`
	Status           types.Status                   `json:"status"`
	CreatedAt        time.Time                      `json:"created_at"`
	UpdatedAt        time.Time                      `json:"updated_at"`
	Associations     []*ContractAssociationResponse `json:"associations,omitempty"`
	Document         *types.ContractDocument        `json:"document,omitempty"`
	ExtractionStatus types.ContractExtractionStatus `json:"extraction_status,omitempty"`
	Extraction       json.RawMessage                `json:"extraction,omitempty" swaggertype:"object"`
	ExtractionError  string                         `json:"extraction_error,omitempty"`
}

type ContractAssociationResponse struct {
	ID         string                 `json:"id"`
	EntityType types.RevRecEntityType `json:"entity_type"`
	EntityID   string                 `json:"entity_id"`
	CreatedAt  time.Time              `json:"created_at"`
}

func NewContractResponse(c *contract.Contract) *ContractResponse {
	if c == nil {
		return nil
	}
	return &ContractResponse{
		ID:               c.ID(),
		TenantID:         c.TenantID(),
		EnvironmentID:    c.EnvironmentID(),
		CustomerID:       c.CustomerID(),
		Currency:         c.Currency(),
		TermStart:        c.TermStart(),
		TermEnd:          c.TermEnd(),
		EndedAt:          c.EndedAt(),
		ContractStatus:   c.ContractStatus(),
		Status:           c.Status(),
		CreatedAt:        c.CreatedAt(),
		UpdatedAt:        c.UpdatedAt(),
		Document:         c.Document(),
		ExtractionStatus: c.ExtractionStatus(),
		Extraction:       c.Extraction(),
		ExtractionError:  c.ExtractionError(),
	}
}

// WithAssociations attaches the contract's associations to the response.
func (r *ContractResponse) WithAssociations(associations []*contract.Association) *ContractResponse {
	r.Associations = lo.Map(associations, func(a *contract.Association, _ int) *ContractAssociationResponse {
		return &ContractAssociationResponse{
			ID:         a.ID(),
			EntityType: a.EntityType(),
			EntityID:   a.EntityID(),
			CreatedAt:  a.CreatedAt(),
		}
	})
	return r
}

// WithoutExtraction drops the extraction payload, keeping extraction_status, for list responses.
func (r *ContractResponse) WithoutExtraction() *ContractResponse {
	r.Extraction = nil
	return r
}

// ListContractsResponse represents the response for listing contracts
type ListContractsResponse = types.ListResponse[*ContractResponse] // @name ListContractsResponse

// DeriveContractTermsRequest holds a reviewer's edits to the inputs of the derived contract values.
// An omitted field keeps the extracted value; charges line up with extraction.result.charges by position.
type DeriveContractTermsRequest struct {
	Term    *DeriveTermInputs    `json:"term,omitempty"`
	Charges []DeriveChargeInputs `json:"charges,omitempty"`
}

type DeriveTermInputs struct {
	EffectiveDate *string `json:"effective_date,omitempty"`
	TermMonths    *string `json:"term_months,omitempty"`
	NoticeDays    *string `json:"notice_days,omitempty"`
}

type DeriveChargeInputs struct {
	Amount        *string `json:"amount,omitempty"`
	AmountPeriod  *string `json:"amount_period,omitempty"`
	BillingPeriod *string `json:"billing_period,omitempty"`
	BillingTiming *string `json:"billing_timing,omitempty"`
}

type DeriveContractTermsResponse struct {
	Derived *extraction.Derived `json:"derived"`
}

// CompleteDraftRequest completes a draft contract: it can create the customer and products, then creates the
// subscriptions and POBs as drafts.
type CompleteDraftRequest struct {
	Contract CreateContractRequest `json:"contract"`
	// Customer creates the contract's customer; set either it or contract.customer_id.
	Customer *CreateCustomerRequest `json:"customer,omitempty"`
	// Products creates products; each of their prices is added to the subscription as a line item.
	Products []CreateProductCatalogRequest `json:"products,omitempty" validate:"omitempty,dive"`
	// Subscriptions run over the contract's term in its customer and currency; one for now.
	Subscriptions          []CreateSubscriptionRequest `json:"subscriptions" validate:"len=1"`
	PerformanceObligations []CreatePOBRequest          `json:"performance_obligations,omitempty" validate:"omitempty,dive"`
	// Activate also activates the subscriptions from the contract's term start.
	Activate bool `json:"activate"`
}

type CreateContractRequest struct {
	CustomerID string `json:"customer_id,omitempty"`
	Currency   string `json:"currency" validate:"required,len=3"`
	// TermStart is required to activate.
	TermStart *time.Time `json:"term_start,omitempty"`
	TermEnd   time.Time  `json:"term_end" validate:"required"`
}

// CreateProductCatalogRequest is a product with the prices it adds to the contract's subscription.
type CreateProductCatalogRequest struct {
	Product CreateProductRequest             `json:"product"`
	Prices  []SubscriptionPriceCreateRequest `json:"prices,omitempty" validate:"omitempty,dive"`
}

// CreatePOBRequest names its product by product_id, or by product_lookup_key for a product created in the same request.
type CreatePOBRequest struct {
	Name              string                  `json:"name" validate:"required"`
	ProductID         string                  `json:"product_id,omitempty"`
	ProductLookupKey  string                  `json:"product_lookup_key,omitempty"`
	RecognitionMethod types.RecognitionMethod `json:"recognition_method" validate:"required"`
	ServiceStart      time.Time               `json:"service_start" validate:"required"`
	ServiceEnd        time.Time               `json:"service_end" validate:"required"`
	AllocatedAmount   decimal.Decimal         `json:"allocated_amount" swaggertype:"string"`
}

func (r *CompleteDraftRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	if (r.Customer == nil) == (r.Contract.CustomerID == "") {
		return ierr.NewError("set either customer or contract.customer_id").
			WithHint("Name an existing customer or create one, not both").
			Mark(ierr.ErrValidation)
	}
	if r.Contract.TermStart != nil && !r.Contract.TermEnd.After(*r.Contract.TermStart) {
		return ierr.NewError("contract term_end must be after term_start").
			WithHint("A contract's term must end after it starts").
			Mark(ierr.ErrValidation)
	}
	if r.Activate && r.Contract.TermStart == nil {
		return ierr.NewError("contract term_start is required to activate").
			WithHint("Set the term start the subscription begins on").
			Mark(ierr.ErrValidation)
	}
	for _, p := range r.PerformanceObligations {
		if err := p.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (r *CreatePOBRequest) Validate() error {
	if (r.ProductID == "") == (r.ProductLookupKey == "") {
		return ierr.NewError("set either product_id or product_lookup_key").
			WithHint("Name the performance obligation's product once").
			WithReportableDetails(map[string]any{"name": r.Name}).
			Mark(ierr.ErrValidation)
	}
	if err := r.RecognitionMethod.Validate(); err != nil {
		return err
	}
	if r.AllocatedAmount.IsNegative() {
		return ierr.NewError("allocated_amount must not be negative").
			WithHint("Allocate zero or more to each performance obligation").
			WithReportableDetails(map[string]any{"name": r.Name}).
			Mark(ierr.ErrValidation)
	}
	if r.ServiceEnd.Before(r.ServiceStart) {
		return ierr.NewError("service_end is before service_start").
			WithHint("A performance obligation's service must end after it starts").
			WithReportableDetails(map[string]any{"name": r.Name}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// SaveContractReviewRequest saves one review step. Version is the review_version the edits were made on.
type SaveContractReviewRequest struct {
	Version int `json:"version" validate:"min=0"`
	// Values sets terms of this step by path, e.g. "charges[0].amount": "184000". "" clears a term.
	Values map[string]string `json:"values,omitempty"`
	// Confirmed replaces the step's confirmed term paths; omit it to keep them.
	Confirmed []string `json:"confirmed,omitempty"`
	// Customer is the customer decision, saved on the customer step only.
	Customer *ContractReviewCustomer `json:"customer,omitempty"`
	// Products replaces the product decisions by charge path, e.g. "charges[0]"; charges step only.
	// Omit it to keep them.
	Products map[string]ContractReviewProduct `json:"products,omitempty" validate:"omitempty,dive"`
}

type ContractReviewCustomer struct {
	Choice     string `json:"choice" validate:"required,oneof=existing new"`
	CustomerID string `json:"customer_id,omitempty"`
}

type ContractReviewProduct struct {
	Choice    string `json:"choice" validate:"required,oneof=existing new"`
	ProductID string `json:"product_id,omitempty"`
}

// ContractProductMatch suggests existing products for one charge, best first.
type ContractProductMatch struct {
	// Status is matched, possible, new or unavailable.
	Status     string                     `json:"status"`
	ProductID  string                     `json:"product_id,omitempty"`
	Candidates []ContractProductCandidate `json:"candidates"`
}

type ContractProductCandidate struct {
	ProductID   string `json:"product_id"`
	Name        string `json:"name"`
	LookupKey   string `json:"lookup_key"`
	Description string `json:"description,omitempty"`
	// Reason is exact_name, lookup_key or similar_name.
	Reason string `json:"reason"`
}

// ContractReviewResponse is the reviewer's working copy of the contract terms. Before the first save it
// is the extraction itself, at version 0.
type ContractReviewResponse struct {
	ContractID string                  `json:"contract_id"`
	Version    int                     `json:"version"`
	Result     *extraction.Result      `json:"result"`
	Confirmed  []string                `json:"confirmed"`
	Edited     []string                `json:"edited"`
	Customer   *ContractReviewCustomer `json:"customer,omitempty"`
	// Products holds the saved product decisions by charge path.
	Products map[string]ContractReviewProduct `json:"products"`
	// ProductMatches suggests existing products for every charge, by charge path.
	ProductMatches map[string]*ContractProductMatch `json:"product_matches"`
}

func (r *SaveContractReviewRequest) Validate() error {
	return validator.ValidateRequest(r)
}
