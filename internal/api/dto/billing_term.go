package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/billingterm"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// BillingTermInput is one billing term as a request states it. The rules are checked when the terms are expanded.
type BillingTermInput struct {
	Kind types.BillingTermKind `json:"kind"`

	// ValueType is fixed or remainder; percentage is rejected as not supported yet.
	ValueType types.BillingTermValueType `json:"value_type"`
	Value     *decimal.Decimal           `json:"value,omitempty" swaggertype:"string"`

	BillTrigger *types.InstalmentBillTrigger `json:"bill_trigger,omitempty"`
	BillOn      *time.Time                   `json:"bill_on,omitempty"`

	StartDate      *time.Time              `json:"start_date,omitempty"`
	EndDate        *time.Time              `json:"end_date,omitempty"`
	PeriodCount    *int                    `json:"period_count,omitempty"`
	InvoiceCadence *types.InvoiceCadence   `json:"invoice_cadence,omitempty"`
	Split          *types.BillingTermSplit `json:"split,omitempty"`

	InvoiceLabel *string        `json:"invoice_label,omitempty" validate:"omitempty,max=255"`
	Metadata     types.Metadata `json:"metadata,omitempty"`
}

// ApplyDefaults aligns the term's dates to whole seconds, like the dates of the commitment they sit in.
func (in *BillingTermInput) ApplyDefaults() {
	for _, date := range []**time.Time{&in.BillOn, &in.StartDate, &in.EndDate} {
		if *date != nil {
			*date = lo.ToPtr((*date).Truncate(time.Second))
		}
	}
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
	Value decimal.Decimal           `json:"value" swaggertype:"string"`
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
	BillingTerms []BillingTermInput `json:"billing_terms" validate:"required,min=1,dive"`
}

// ApplyDefaults aligns the dates of every term to whole seconds.
func (r *ReplaceBillingTermsRequest) ApplyDefaults() {
	for i := range r.BillingTerms {
		r.BillingTerms[i].ApplyDefaults()
	}
}

// Validate checks the rules that need only the request. Call ApplyDefaults first.
func (r ReplaceBillingTermsRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// BillingTermsResponse is a commitment's terms with the instalments they produce. InvoicesToReview lists the
// invoices that carry an instalment an edit replaced.
type BillingTermsResponse struct {
	BillingTerms     []*billingterm.Term       `json:"billing_terms"`
	Instalments      []*billingterm.Instalment `json:"instalments"`
	InvoicesToReview []string                  `json:"invoices_to_review,omitempty"`
}

// ReplanInstalmentInput is one instalment of a re-plan. A date instalment needs a bill_on; a manual one takes none and
// waits at the end of the commitment term until it is triggered.
type ReplanInstalmentInput struct {
	Amount decimal.Decimal `json:"amount" swaggertype:"string"`

	BillOn      *time.Time                   `json:"bill_on,omitempty"`
	BillTrigger *types.InstalmentBillTrigger `json:"bill_trigger,omitempty"`

	InvoiceLabel *string `json:"invoice_label,omitempty" validate:"omitempty,max=255"`
}

// Trigger returns how the instalment becomes billable, date unless the request says otherwise.
func (in ReplanInstalmentInput) Trigger() types.InstalmentBillTrigger {
	return lo.CoalesceOrEmpty(lo.FromPtr(in.BillTrigger), types.InstalmentBillTriggerDate)
}

// Validate checks the rules that need only the instalment. index is its position in the request.
func (in ReplanInstalmentInput) Validate(index int) error {
	if !in.Amount.IsPositive() {
		return ierr.Validationf("instalments[%d].amount: must be greater than 0", index)
	}

	switch in.Trigger() {
	case types.InstalmentBillTriggerDate:
		if in.BillOn == nil {
			return ierr.Validationf("instalments[%d].bill_on: a date instalment needs a bill_on", index)
		}
	case types.InstalmentBillTriggerManual:
		if in.BillOn != nil {
			return ierr.Validationf("instalments[%d].bill_on: is set by triggering a manual instalment, not in the plan", index)
		}
	default:
		return ierr.Validationf("instalments[%d].bill_trigger: must be date or manual", index)
	}

	return nil
}

// ReplanInstalmentsRequest is the new list of instalments of a commitment, which replaces its scheduled instalments
// as a whole.
type ReplanInstalmentsRequest struct {
	Instalments []ReplanInstalmentInput `json:"instalments" validate:"required,min=1,dive"`
}

// ApplyDefaults aligns the bill_on of every instalment to whole seconds.
func (r *ReplanInstalmentsRequest) ApplyDefaults() {
	for n := range r.Instalments {
		if billOn := r.Instalments[n].BillOn; billOn != nil {
			r.Instalments[n].BillOn = lo.ToPtr(billOn.Truncate(time.Second))
		}
	}
}

// Validate checks the rules that need only the request. Call ApplyDefaults first.
func (r ReplanInstalmentsRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if len(r.Instalments) > billingterm.MaxInstalments {
		return ierr.Validationf("instalments: must hold 1 to %d instalments", billingterm.MaxInstalments)
	}

	for n, instalment := range r.Instalments {
		if err := instalment.Validate(n); err != nil {
			return err
		}
	}

	return nil
}

// BringForwardInstalmentsRequest moves Amount of the commitment's scheduled instalments into one new instalment
// billed on BillOn, now by default.
type BringForwardInstalmentsRequest struct {
	Amount decimal.Decimal `json:"amount" swaggertype:"string"`
	BillOn *time.Time      `json:"bill_on,omitempty"`
}

// ApplyDefaults aligns bill_on to whole seconds.
func (r *BringForwardInstalmentsRequest) ApplyDefaults() {
	if r.BillOn != nil {
		r.BillOn = lo.ToPtr(r.BillOn.Truncate(time.Second))
	}
}

// Validate checks the rules that need only the request. Call ApplyDefaults first.
func (r BringForwardInstalmentsRequest) Validate() error {
	if !r.Amount.IsPositive() {
		return ierr.Validation("amount: must be greater than 0")
	}

	return nil
}

// TriggerInstalmentRequest triggers a manual instalment to be billed on BillOn, now by default.
type TriggerInstalmentRequest struct {
	BillOn *time.Time `json:"bill_on,omitempty"`
}

// ApplyDefaults aligns bill_on to whole seconds.
func (r *TriggerInstalmentRequest) ApplyDefaults() {
	if r.BillOn != nil {
		r.BillOn = lo.ToPtr(r.BillOn.Truncate(time.Second))
	}
}

// ListInstalmentsResponse is the instalments of a commitment in billing order.
type ListInstalmentsResponse struct {
	Items []*billingterm.Instalment `json:"items"`
}
