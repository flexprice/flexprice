package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// InvoiceCadence defines when an invoice is generated relative to the billing period
// ARREAR: Invoice generated at the end of the billing period (after service delivery)
// ADVANCE: Invoice generated at the beginning of the billing period (before service delivery)
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

// InvoiceType categorizes the purpose and nature of the invoice
type InvoiceType string

const (
	// InvoiceTypeSubscription indicates invoice is for recurring subscription charges
	InvoiceTypeSubscription InvoiceType = "SUBSCRIPTION"
	// InvoiceTypeOneOff indicates invoice is for one-time charges or usage-based billing
	InvoiceTypeOneOff InvoiceType = "ONE_OFF"
	// InvoiceTypeCredit indicates invoice is for credit adjustments or refunds
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

// InvoiceStatus represents the current state of an invoice in its lifecycle
type InvoiceStatus string

const (
	// InvoiceStatusDraft indicates invoice is in draft state and can be modified or deleted
	InvoiceStatusDraft InvoiceStatus = "DRAFT"
	// InvoiceStatusFinalized indicates invoice is finalized, immutable, and ready for payment
	InvoiceStatusFinalized InvoiceStatus = "FINALIZED"
	// InvoiceStatusVoided indicates invoice has been voided and is no longer valid for payment
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

// InvoiceBillingReason indicates why an invoice was generated
type InvoiceBillingReason string

const (
	// InvoiceBillingReasonSubscriptionCreate indicates invoice is for new subscription activation
	InvoiceBillingReasonSubscriptionCreate InvoiceBillingReason = "SUBSCRIPTION_CREATE"
	// InvoiceBillingReasonSubscriptionCycle indicates invoice is for regular subscription billing cycle
	InvoiceBillingReasonSubscriptionCycle InvoiceBillingReason = "SUBSCRIPTION_CYCLE"
	// InvoiceBillingReasonSubscriptionUpdate indicates invoice is for subscription changes (upgrades, downgrades)
	InvoiceBillingReasonSubscriptionUpdate InvoiceBillingReason = "SUBSCRIPTION_UPDATE"
	// InvoiceBillingReasonManual indicates invoice was created manually by an administrator
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
	// InvoiceDefaultDueDays is the default number of days after invoice creation when payment is due
	InvoiceDefaultDueDays = 1
)

// InvoiceFilter represents the filter options for listing invoices
type InvoiceFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// invoice_ids restricts results to invoices with the specified IDs
	// Use this to retrieve specific invoices when you know their exact identifiers
	InvoiceIDs []string `json:"invoice_ids,omitempty" form:"invoice_ids"`

	// customer_id filters invoices for a specific customer using FlexPrice's internal customer ID
	// This is the ID returned by FlexPrice when creating or retrieving customers
	CustomerID string `json:"customer_id,omitempty" form:"customer_id"`

	// external_customer_id filters invoices for a customer using your system's customer identifier
	// This is the ID you provided when creating the customer in FlexPrice
	ExternalCustomerID string `json:"external_customer_id,omitempty" form:"external_customer_id"`

	// subscription_id filters invoices generated for a specific subscription
	// Only returns invoices that were created as part of the specified subscription's billing
	SubscriptionID string `json:"subscription_id,omitempty" form:"subscription_id"`

	// invoice_type filters by the nature of the invoice (SUBSCRIPTION, ONE_OFF, or CREDIT)
	// Use this to separate recurring charges from one-time fees or credit adjustments
	InvoiceType InvoiceType `json:"invoice_type,omitempty" form:"invoice_type"`

	// invoice_status filters by the current state of invoices in their lifecycle
	// Multiple statuses can be specified to include invoices in any of the listed states
	InvoiceStatus []InvoiceStatus `json:"invoice_status,omitempty" form:"invoice_status"`

	// payment_status filters by the payment state of invoices
	// Multiple statuses can be specified to include invoices with any of the listed payment states
	PaymentStatus []PaymentStatus `json:"payment_status,omitempty" form:"payment_status"`

	// amount_due_gt filters invoices with a total amount due greater than the specified value
	// Useful for finding invoices above a certain threshold or identifying high-value invoices
	AmountDueGt *decimal.Decimal `json:"amount_due_gt,omitempty" form:"amount_due_gt"`

	// amount_remaining_gt filters invoices with an outstanding balance greater than the specified value
	// Useful for finding invoices that still have significant unpaid amounts
	AmountRemainingGt *decimal.Decimal `json:"amount_remaining_gt,omitempty" form:"amount_remaining_gt"`
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
