package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// InvoiceLineItemEntityType is the type of the source of a invoice line item
// It is optional and can be used to differentiate between plan and addon line items
type InvoiceLineItemEntityType string

const (
	InvoiceLineItemEntityTypePlan  InvoiceLineItemEntityType = "plan"
	InvoiceLineItemEntityTypeAddon InvoiceLineItemEntityType = "addon"
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

	if c != "" && !lo.Contains(allowed, c) {
		return ierr.NewError("invalid invoice cadence").
			WithHint("Please provide a valid invoice cadence").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// InvoiceFlowType represents the type of invoice flow for payment behavior handling
type InvoiceFlowType string

const (
	// InvoiceFlowSubscriptionCreation represents subscription creation flow - applies full payment behavior logic
	InvoiceFlowSubscriptionCreation InvoiceFlowType = "subscription_creation"
	// InvoiceFlowRenewal represents renewal/periodic billing flow - always creates invoices, marks as pending on failure
	InvoiceFlowRenewal InvoiceFlowType = "renewal"
	// InvoiceFlowManual represents manual invoice creation flow - default behavior
	InvoiceFlowManual InvoiceFlowType = "manual"
	// InvoiceFlowCancel represents subscription cancellation flow - uses subscription's payment method and behavior but doesn't error on manual+renewal
	InvoiceFlowCancel InvoiceFlowType = "cancel"
)

func (f InvoiceFlowType) String() string {
	return string(f)
}

func (f InvoiceFlowType) Validate() error {
	allowed := []InvoiceFlowType{
		InvoiceFlowSubscriptionCreation,
		InvoiceFlowRenewal,
		InvoiceFlowManual,
		InvoiceFlowCancel,
	}

	if f != "" && !lo.Contains(allowed, f) {
		return ierr.NewError("invalid invoice flow type").
			WithHint("Please provide a valid invoice flow type").
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

	if t != "" && !lo.Contains(allowed, t) {
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

	if s != "" && !lo.Contains(allowed, s) {
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
	// InvoiceBillingReasonProration indicates invoice is for proration credits/charges (cancellations, plan changes)
	InvoiceBillingReasonProration InvoiceBillingReason = "PRORATION"
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
		InvoiceBillingReasonProration,
		InvoiceBillingReasonManual,
	}

	if r != "" && !lo.Contains(allowed, r) {
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

type InvoiceNumberFormat string

const (
	InvoiceNumberFormatYYYYMM   InvoiceNumberFormat = "YYYYMM"
	InvoiceNumberFormatYYYYMMDD InvoiceNumberFormat = "YYYYMMDD"
	InvoiceNumberFormatYYMMDD   InvoiceNumberFormat = "YYMMDD"
	InvoiceNumberFormatYY       InvoiceNumberFormat = "YY"
	InvoiceNumberFormatYYYY     InvoiceNumberFormat = "YYYY"
)

// InvoiceConfig represents the configuration for automatic invoice number generation.
// It defines the format and sequencing rules used to create unique, human-readable invoice numbers.
//
// Fields:
//   - InvoiceNumberPrefix: A string prefix for all invoice numbers (e.g., "INV", "BILL")
//   - InvoiceNumberFormat: Date format template using Go time format (e.g., "YYYYMM" for year-month)
//   - InvoiceNumberStartSequence: Starting number for the sequence counter (typically 1)
//   - InvoiceNumberTimezone: Timezone for date formatting (e.g., "UTC", "America/New_York")
//   - InvoiceNumberSeparator: Character(s) used to separate parts of the invoice number (e.g., "-", "_", or "" for no separator)
//   - InvoiceNumberSuffixLength: Number of digits for the sequence number suffix (e.g., 5 for "00001")
//
// Generated invoice numbers follow the pattern: {prefix}{separator}{formatted_date}{separator}{padded_sequence}
//
// Example configuration with separator:
//
//	InvoiceNumberPrefix: "INV"
//	InvoiceNumberFormat: "YYYYMM"
//	InvoiceNumberStartSequence: 1
//	InvoiceNumberTimezone: "UTC"
//	InvoiceNumberSeparator: "-"
//	InvoiceNumberSuffixLength: 5
//
// Generated invoice numbers for January 2025:
//
//	"INV-202501-00001"
//	"INV-202501-00002"
//	"INV-202501-00003"
//
// Example configuration without separator:
//
//	InvoiceNumberPrefix: "INV"
//	InvoiceNumberFormat: "YYYYMM"
//	InvoiceNumberStartSequence: 1
//	InvoiceNumberTimezone: "UTC"
//	InvoiceNumberSeparator: ""
//	InvoiceNumberSuffixLength: 5
//
// Generated invoice numbers for January 2025:
//
//	"INV20250100001"
//	"INV20250100002"
//	"INV20250100003"
//
// Note: Sequences reset monthly and are tenant-environment-scoped for isolation.
type InvoiceConfig struct {
	InvoiceNumberPrefix                    string              `json:"prefix,omitempty" validate:"required,min=1"`
	InvoiceNumberFormat                    InvoiceNumberFormat `json:"format,omitempty" validate:"required"`
	InvoiceNumberStartSequence             int                 `json:"start_sequence,omitempty" validate:"required,min=0"`
	InvoiceNumberTimezone                  string              `json:"timezone,omitempty" validate:"required,min=1"`
	InvoiceNumberSeparator                 string              `json:"separator,omitempty"` // Empty string ("") is valid for no separator
	InvoiceNumberSuffixLength              int                 `json:"suffix_length,omitempty" validate:"required,min=1,max=10"`
	DueDateDays                            *int                `json:"due_date_days,omitempty" validate:"omitempty,min=0"` // Number of days after period end when payment is due
	AutoCompletePurchasedCreditTransaction bool                `json:"auto_complete_purchased_credit_transaction,omitempty"`
}

// Validate implements SettingConfig interface
func (c InvoiceConfig) Validate() error {
	// Validate struct using validation tags
	if err := validator.ValidateRequest(c); err != nil {
		return err
	}

	// Additional custom validation for InvoiceNumberFormat enum
	validFormats := []InvoiceNumberFormat{
		InvoiceNumberFormatYYYYMM,
		InvoiceNumberFormatYYYYMMDD,
		InvoiceNumberFormatYYMMDD,
		InvoiceNumberFormatYY,
		InvoiceNumberFormatYYYY,
	}
	valid := false
	for _, validFormat := range validFormats {
		if c.InvoiceNumberFormat == validFormat {
			valid = true
			break
		}
	}
	if !valid {
		return ierr.NewErrorf("invoice_config: 'format' must be one of %v, got %s", validFormats, c.InvoiceNumberFormat).
			WithHintf("Invoice config format must be one of %v", validFormats).
			Mark(ierr.ErrValidation)
	}

	// Validate timezone directly on the struct
	if err := ValidateTimezone(c.InvoiceNumberTimezone); err != nil {
		return ierr.WithError(err).
			WithHintf("Invalid timezone: %s", c.InvoiceNumberTimezone).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// InvoiceFilter represents the filter options for listing invoices
type InvoiceFilter struct {
	*QueryFilter
	*TimeRangeFilter

	Filters []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`
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

	// SkipLineItems if true, will not include line items in the response
	SkipLineItems bool `json:"skip_line_items,omitempty" form:"skip_line_items"`
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
