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

// BillingTermInput is one billing term as a request states it. A term of value_type percentage is stored as a fixed
// amount that keeps the percentage entered.
type BillingTermInput struct {
	Kind      types.BillingTermKind      `json:"kind"`
	ValueType types.BillingTermValueType `json:"value_type"`

	Amount     *decimal.Decimal `json:"amount,omitempty" swaggertype:"string"`
	Percentage *decimal.Decimal `json:"percentage,omitempty" swaggertype:"string"`

	BillTrigger *types.InstalmentBillTrigger `json:"bill_trigger,omitempty"`
	BillOn      *time.Time                   `json:"bill_on,omitempty"`
	CoversFrom  *time.Time                   `json:"covers_from,omitempty"`
	CoversTo    *time.Time                   `json:"covers_to,omitempty"`

	StartsOn       *time.Time              `json:"starts_on,omitempty"`
	EndsOn         *time.Time              `json:"ends_on,omitempty"`
	PeriodCount    *int                    `json:"period_count,omitempty"`
	InvoiceCadence *types.InvoiceCadence   `json:"invoice_cadence,omitempty"`
	Split          *types.BillingTermSplit `json:"split,omitempty"`

	Description *string        `json:"description,omitempty"`
	Metadata    types.Metadata `json:"metadata,omitempty"`
}

// ApplyDefaults aligns the term's dates to whole seconds, like the dates of the commitment they sit in.
func (in *BillingTermInput) ApplyDefaults() {
	for _, date := range []**time.Time{&in.BillOn, &in.CoversFrom, &in.CoversTo, &in.StartsOn, &in.EndsOn} {
		if *date != nil {
			*date = lo.ToPtr((*date).Truncate(time.Second))
		}
	}
}

// Validate checks that the amount fields fit the value type, which the stored form can no longer tell apart; the
// remaining rules are checked when the terms are expanded. index is the term's position in the request.
func (in BillingTermInput) Validate(index int) error {
	switch in.ValueType {
	case types.BillingTermValuePercentage:
		if in.Amount != nil {
			return ierr.Validationf("billing_terms[%d].amount: a percentage term takes no amount", index)
		}

		if in.Percentage == nil {
			return ierr.Validationf("billing_terms[%d].percentage: a percentage term needs a percentage", index)
		}
	case types.BillingTermValueFixed:
		if in.Percentage != nil {
			return ierr.Validationf("billing_terms[%d].percentage: a fixed term takes an amount; use value_type percentage for a percentage", index)
		}
	}

	return nil
}

// ToTerm builds the term of the commitment ownerID with a new ID.
func (in BillingTermInput) ToTerm(ctx context.Context, ownerID string) *billingterm.Term {
	valueType := in.ValueType
	if valueType == types.BillingTermValuePercentage {
		valueType = types.BillingTermValueFixed
	}

	return &billingterm.Term{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_TERM),

		OwnerType: types.BillingTermOwnerCommitment,
		OwnerID:   ownerID,

		Kind:      in.Kind,
		ValueType: valueType,

		Amount:     in.Amount,
		Percentage: in.Percentage,

		BillTrigger: in.BillTrigger,
		BillOn:      in.BillOn,
		CoversFrom:  in.CoversFrom,
		CoversTo:    in.CoversTo,

		StartsOn:       in.StartsOn,
		EndsOn:         in.EndsOn,
		PeriodCount:    in.PeriodCount,
		InvoiceCadence: in.InvoiceCadence,
		Split:          in.Split,

		Description: in.Description,
		Metadata:    in.Metadata,

		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
}

// ReplaceBillingTermsRequest is the new set of terms of a commitment, which replaces the current set as a whole.
type ReplaceBillingTermsRequest struct {
	BillingTerms []BillingTermInput `json:"billing_terms" validate:"required,min=1"`
}

// ApplyDefaults aligns the dates of every term to whole seconds.
func (r *ReplaceBillingTermsRequest) ApplyDefaults() {
	for i := range r.BillingTerms {
		r.BillingTerms[i].ApplyDefaults()
	}
}

// Validate checks the rules that need only the request. Call ApplyDefaults first.
func (r ReplaceBillingTermsRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	for i, term := range r.BillingTerms {
		if err := term.Validate(i); err != nil {
			return err
		}
	}

	return nil
}

// BillingTermsResponse is a commitment's terms with the instalments they produce. InvoicesToReview lists the
// invoices that carry an instalment an edit replaced.
type BillingTermsResponse struct {
	BillingTerms     []*billingterm.Term       `json:"billing_terms"`
	Instalments      []*billingterm.Instalment `json:"instalments"`
	InvoicesToReview []string                  `json:"invoices_to_review,omitempty"`
}

// ListInstalmentsResponse is the instalments of a commitment in billing order.
type ListInstalmentsResponse struct {
	Items []*billingterm.Instalment `json:"items"`
}
