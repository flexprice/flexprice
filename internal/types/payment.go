package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// PaymentStatus represents the status of a payment
type PaymentStatus string

const (
	PaymentStatusInitiated         PaymentStatus = "INITIATED"
	PaymentStatusPending           PaymentStatus = "PENDING"
	PaymentStatusProcessing        PaymentStatus = "PROCESSING"
	PaymentStatusSucceeded         PaymentStatus = "SUCCEEDED"
	PaymentStatusOverpaid          PaymentStatus = "OVERPAID"
	PaymentStatusFailed            PaymentStatus = "FAILED"
	PaymentStatusRefunded          PaymentStatus = "REFUNDED"
	PaymentStatusPartiallyRefunded PaymentStatus = "PARTIALLY_REFUNDED"
)

func (s PaymentStatus) String() string {
	return string(s)
}

func (s PaymentStatus) Validate() error {
	allowed := []PaymentStatus{
		PaymentStatusInitiated,
		PaymentStatusPending,
		PaymentStatusProcessing,
		PaymentStatusSucceeded,
		PaymentStatusOverpaid,
		PaymentStatusFailed,
		PaymentStatusRefunded,
		PaymentStatusPartiallyRefunded,
	}
	if !lo.Contains(allowed, s) {
		return ierr.NewError("invalid payment status").
			WithHint("Please provide a valid payment status").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// PaymentMethodType represents the type of payment method
type PaymentMethodType string

const (
	PaymentMethodTypeCard        PaymentMethodType = "CARD"
	PaymentMethodTypeACH         PaymentMethodType = "ACH"
	PaymentMethodTypeOffline     PaymentMethodType = "OFFLINE"
	PaymentMethodTypeCredits     PaymentMethodType = "CREDITS"
	PaymentMethodTypePaymentLink PaymentMethodType = "PAYMENT_LINK"
)

func (s PaymentMethodType) String() string {
	return string(s)
}

func (s PaymentMethodType) Validate() error {
	allowed := []PaymentMethodType{
		PaymentMethodTypeCard,
		PaymentMethodTypeACH,
		PaymentMethodTypeOffline,
		PaymentMethodTypeCredits,
		PaymentMethodTypePaymentLink,
	}
	if !lo.Contains(allowed, s) {
		return ierr.NewError("invalid payment method type").
			WithHint("Please provide a valid payment method type").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// PaymentDestinationType represents the type of payment destination
type PaymentDestinationType string

const (
	PaymentDestinationTypeInvoice PaymentDestinationType = "INVOICE"
)

func (s PaymentDestinationType) String() string {
	return string(s)
}

func (s PaymentDestinationType) Validate() error {
	allowed := []PaymentDestinationType{
		PaymentDestinationTypeInvoice,
	}
	if !lo.Contains(allowed, s) {
		return ierr.NewError("invalid payment destination type").
			WithHint("Please provide a valid payment destination type").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// PaymentFilter represents the filter for listing payments
type PaymentFilter struct {
	*QueryFilter
	*TimeRangeFilter

	PaymentIDs        []string `form:"payment_ids"`
	DestinationType   *string  `form:"destination_type"`
	DestinationID     *string  `form:"destination_id"`
	PaymentMethodType *string  `form:"payment_method_type"`
	PaymentStatus     *string  `form:"payment_status"`
	PaymentGateway    *string  `form:"payment_gateway"`
	Currency          *string  `form:"currency"`
}

// NewNoLimitPaymentFilter creates a new payment filter with no limit
func NewNoLimitPaymentFilter() *PaymentFilter {
	return &PaymentFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the payment filter
func (f *PaymentFilter) Validate() error {
	if f == nil {
		return nil
	}

	if err := f.QueryFilter.Validate(); err != nil {
		return err
	}

	if err := f.TimeRangeFilter.Validate(); err != nil {
		return err
	}

	return nil
}

// GetLimit implements BaseFilter interface
func (f *PaymentFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *PaymentFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *PaymentFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

func (f *PaymentFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

func (f *PaymentFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

func (f *PaymentFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns true if the filter has no limit
func (f *PaymentFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
