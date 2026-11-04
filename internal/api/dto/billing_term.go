package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/billingterm"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// BillingTermInput is one billing term as a request states it. The request checks that kind and value_type are set;
// the other rules are checked when the terms are expanded.
type BillingTermInput struct {
	Kind types.BillingTermKind `json:"kind" validate:"required"`

	// ValueType is fixed or remainder; percentage is rejected as not supported yet.
	ValueType types.BillingTermValueType `json:"value_type" validate:"required"`
	Value     *decimal.Decimal           `json:"value,omitempty" swaggertype:"string"`

	BillTrigger *types.InstalmentBillTrigger `json:"bill_trigger,omitempty"`

	// BillOn defaults to the commitment start, or on an edit to the start of the billing period holding now (the
	// commitment start once its term has ended). A date under a second past a period edge means the edge.
	BillOn *time.Time `json:"bill_on,omitempty"`

	// StartDate defaults like BillOn; EndDate defaults to the commitment end.
	StartDate      *time.Time              `json:"start_date,omitempty"`
	EndDate        *time.Time              `json:"end_date,omitempty"`
	PeriodCount    *int                    `json:"period_count,omitempty"`
	InvoiceCadence *types.InvoiceCadence   `json:"invoice_cadence,omitempty"`
	Split          *types.BillingTermSplit `json:"split,omitempty"`

	// InstalmentInvoicing delivers this term's instalments, the commitment's instalment_invoicing by default.
	InstalmentInvoicing *types.InstalmentDelivery `json:"instalment_invoicing,omitempty"`

	// InvoiceLabel names the term's instalments, "Instalment {n} of {of}" by default. {n} and {of} count only the
	// commitment's instalments whose labels use them.
	InvoiceLabel *string        `json:"invoice_label,omitempty" validate:"omitempty,max=255"`
	Metadata     types.Metadata `json:"metadata,omitempty"`
}

// ToTerm builds the term of the commitment ownerID with a new ID.
func (in BillingTermInput) ToTerm(ctx context.Context, ownerID string) *billingterm.Term {
	return &billingterm.Term{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_TERM),

		OwnerType: types.BillingTermOwnerCommitment,
		OwnerID:   ownerID,

		Kind:      in.Kind,
		ValueType: in.ValueType,
		Value:     in.Value,

		BillTrigger: in.BillTrigger,
		BillOn:      in.BillOn,

		StartDate:      in.StartDate,
		EndDate:        in.EndDate,
		PeriodCount:    in.PeriodCount,
		InvoiceCadence: in.InvoiceCadence,
		Split:          in.Split,

		InstalmentInvoicing: in.InstalmentInvoicing,

		InvoiceLabel: in.InvoiceLabel,
		Metadata:     in.Metadata,

		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
}

// CancellationFeeInput is the fee billed when a commitment is terminated early. Type says how to read Value: an
// amount for fixed, a percentage for percentage.
type CancellationFeeInput struct {
	// Type is fixed or percentage; a percentage fee is rejected as not supported yet.
	Type  types.CancellationFeeType `json:"type" validate:"required"`
	Value decimal.Decimal           `json:"value" validate:"required" swaggertype:"string"`
}

// Validate checks the fee against its type. A percentage fee is rejected as not supported yet.
func (in CancellationFeeInput) Validate() error {
	switch in.Type {
	case types.CancellationFeeFixed:
		if !in.Value.IsPositive() {
			return ierr.Validation("cancellation_fee.value: must be greater than 0")
		}
	case types.CancellationFeePercentage:
		return ierr.Validation("percentage cancellation fees are not supported yet")
	default:
		return ierr.Validation("cancellation_fee.type: must be fixed or percentage")
	}

	return nil
}

// ReplaceBillingTermsRequest is the new set of terms of a commitment, which replaces the current set as a whole.
type ReplaceBillingTermsRequest struct {
	BillingTerms []BillingTermInput `json:"billing_terms" validate:"required,min=1,max=100,dive"`
}

// Validate checks the rules that need only the request.
func (r ReplaceBillingTermsRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// BillingTermsResponse is a commitment's terms with its scheduled and invoiced instalments in billing order.
type BillingTermsResponse struct {
	BillingTerms []*billingterm.Term       `json:"billing_terms"`
	Instalments  []*billingterm.Instalment `json:"instalments"`
}

// TriggerInstalmentRequest triggers a manual instalment to be billed on BillOn, now by default.
type TriggerInstalmentRequest struct {
	// BillOn within a second after a billing period edge is taken as that edge.
	BillOn *time.Time `json:"bill_on,omitempty"`
}

// ListInstalmentsResponse is the instalments of a commitment in billing order.
type ListInstalmentsResponse struct {
	Items []*billingterm.Instalment `json:"items"`
}
