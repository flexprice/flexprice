package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// InvoiceCadence is the cadence of the invoice ex ARREAR, ADVANCE, etc
type InvoiceCadence string

const (
	// InvoiceCadenceArrear raises an invoice at the end of each billing period (in arrears)
	InvoiceCadenceArrear InvoiceCadence = "ARREAR"
	// InvoiceCadenceAdvance raises an invoice at the beginning of each billing period (in advance)
	InvoiceCadenceAdvance InvoiceCadence = "ADVANCE"
)

func (c InvoiceCadence) String() string {
	return string(c)
}

func (c InvoiceCadence) Validate() error {
	allowed := []InvoiceCadence{
		InvoiceCadenceArrear,
		InvoiceCadenceAdvance,
	}
	if !lo.Contains(allowed, c) {
		return ierr.NewError("invalid invoice cadence").
			WithHint("Please provide a valid invoice cadence").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

type InvoiceType string

const (
	// InvoiceTypeSubscription indicates invoice is for subscription charges
	InvoiceTypeSubscription InvoiceType = "SUBSCRIPTION"
	// InvoiceTypeOneOff indicates invoice is for one-time charges
	InvoiceTypeOneOff InvoiceType = "ONE_OFF"
	// InvoiceTypeCredit indicates invoice is for credit adjustments
	InvoiceTypeCredit InvoiceType = "CREDIT"
)

func (t InvoiceType) String() string {
	return string(t)
}

func (t InvoiceType) Validate() error {
	allowed := []InvoiceType{
		InvoiceTypeSubscription,
		InvoiceTypeOneOff,
		InvoiceTypeCredit,
	}
	if !lo.Contains(allowed, t) {
		return ierr.NewError("invalid invoice type").
			WithHint("Please provide a valid invoice type").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

type InvoiceStatus string

const (
	// InvoiceStatusDraft indicates invoice is in draft state and can be modified
	InvoiceStatusDraft InvoiceStatus = "DRAFT"
	// InvoiceStatusFinalized indicates invoice is finalized and ready for payment
	InvoiceStatusFinalized InvoiceStatus = "FINALIZED"
	// InvoiceStatusVoided indicates invoice has been voided
	InvoiceStatusVoided InvoiceStatus = "VOIDED"
)

func (s InvoiceStatus) String() string {
	return string(s)
}

func (s InvoiceStatus) Validate() error {
	allowed := []InvoiceStatus{
		InvoiceStatusDraft,
		InvoiceStatusFinalized,
		InvoiceStatusVoided,
	}
	if !lo.Contains(allowed, s) {
		return ierr.NewError("invalid invoice status").
			WithHint("Please provide a valid invoice status").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

type InvoiceBillingReason string

const (
	// InvoiceBillingReasonSubscriptionCreate indicates invoice is for subscription creation
	InvoiceBillingReasonSubscriptionCreate InvoiceBillingReason = "SUBSCRIPTION_CREATE"
	// InvoiceBillingReasonSubscriptionCycle indicates invoice is for subscription renewal
	InvoiceBillingReasonSubscriptionCycle InvoiceBillingReason = "SUBSCRIPTION_CYCLE"
	// InvoiceBillingReasonSubscriptionUpdate indicates invoice is for subscription update
	InvoiceBillingReasonSubscriptionUpdate InvoiceBillingReason = "SUBSCRIPTION_UPDATE"
	// InvoiceBillingReasonManual indicates invoice is created manually
	InvoiceBillingReasonManual InvoiceBillingReason = "MANUAL"
)

func (r InvoiceBillingReason) String() string {
	return string(r)
}

func (r InvoiceBillingReason) Validate() error {
	allowed := []InvoiceBillingReason{
		InvoiceBillingReasonSubscriptionCreate,
		InvoiceBillingReasonSubscriptionCycle,
		InvoiceBillingReasonSubscriptionUpdate,
		InvoiceBillingReasonManual,
	}
	if !lo.Contains(allowed, r) {
		return ierr.NewError("invalid invoice billing reason").
			WithHint("Please provide a valid invoice billing reason").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

const (
	InvoiceDefaultDueDays = 1
)

// InvoiceFilter represents the filter options for listing invoices
type InvoiceFilter struct {
	*QueryFilter
	*TimeRangeFilter
	InvoiceIDs         []string         `json:"invoice_ids,omitempty" form:"invoice_ids"`
	CustomerID         string           `json:"customer_id,omitempty" form:"customer_id"`
	ExternalCustomerID string           `json:"external_customer_id,omitempty" form:"external_customer_id"`
	SubscriptionID     string           `json:"subscription_id,omitempty" form:"subscription_id"`
	InvoiceType        InvoiceType      `json:"invoice_type,omitempty" form:"invoice_type"`
	InvoiceStatus      []InvoiceStatus  `json:"invoice_status,omitempty" form:"invoice_status"`
	PaymentStatus      []PaymentStatus  `json:"payment_status,omitempty" form:"payment_status"`
	AmountDueGt        *decimal.Decimal `json:"amount_due_gt,omitempty" form:"amount_due_gt"`
	AmountRemainingGt  *decimal.Decimal `json:"amount_remaining_gt,omitempty" form:"amount_remaining_gt"`
}

// NewInvoiceFilter creates a new invoice filter with default options
func NewInvoiceFilter() *InvoiceFilter {
	return &InvoiceFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitInvoiceFilter creates a new invoice filter without pagination
func NewNoLimitInvoiceFilter() *InvoiceFilter {
	return &InvoiceFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the invoice filter
func (f *InvoiceFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return ierr.WithError(err).WithHint("invalid query filter").Mark(ierr.ErrValidation)
		}
	}
	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return ierr.WithError(err).WithHint("invalid time range").Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// GetLimit implements BaseFilter interface
func (f *InvoiceFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *InvoiceFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *InvoiceFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *InvoiceFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *InvoiceFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *InvoiceFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *InvoiceFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
