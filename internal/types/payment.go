package types

import (
	"fmt"

	"github.com/samber/lo"
)

// PaymentStatus represents the status of a payment
type PaymentStatus string

const (
	PaymentStatusPending           PaymentStatus = "PENDING"
	PaymentStatusProcessing        PaymentStatus = "PROCESSING"
	PaymentStatusSucceeded         PaymentStatus = "SUCCEEDED"
	PaymentStatusFailed            PaymentStatus = "FAILED"
	PaymentStatusRefunded          PaymentStatus = "REFUNDED"
	PaymentStatusPartiallyRefunded PaymentStatus = "PARTIALLY_REFUNDED"
)

func (s PaymentStatus) String() string {
	return string(s)
}

func (s PaymentStatus) Validate() error {
	allowed := []PaymentStatus{
		PaymentStatusPending,
		PaymentStatusProcessing,
		PaymentStatusSucceeded,
		PaymentStatusFailed,
		PaymentStatusRefunded,
		PaymentStatusPartiallyRefunded,
	}
	if !lo.Contains(allowed, s) {
		return fmt.Errorf("invalid payment status: %s", s)
	}
	return nil
}

// PaymentMethodType represents the type of payment method
type PaymentMethodType string

const (
	PaymentMethodTypeCard    PaymentMethodType = "CARD"
	PaymentMethodTypeACH     PaymentMethodType = "ACH"
	PaymentMethodTypeOffline PaymentMethodType = "OFFLINE"
	PaymentMethodTypeCredits PaymentMethodType = "CREDITS"
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
	}
	if !lo.Contains(allowed, s) {
		return fmt.Errorf("invalid payment method type: %s", s)
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
		return fmt.Errorf("invalid payment destination type: %s", s)
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
