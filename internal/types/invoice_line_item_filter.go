package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// InvoiceLineItemFilter provides filtering options for invoice line item queries.
type InvoiceLineItemFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// DSL-based filters and sorts (same pattern as PlanFilter, CustomerFilter).
	Filters []*FilterCondition `json:"filters,omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty"`

	// InvoiceIDs filters by one or more invoice IDs.
	// Uses the (tenant_id, environment_id, invoice_id, status) index.
	InvoiceIDs []string `json:"invoice_ids,omitempty"`

	// CustomerIDs filters by customer.
	// Uses the (tenant_id, environment_id, customer_id, status) index.
	CustomerIDs []string `json:"customer_ids,omitempty"`

	// SubscriptionIDs filters by subscription.
	// Uses the (tenant_id, environment_id, subscription_id, status) index.
	SubscriptionIDs []string `json:"subscription_ids,omitempty"`

	// PriceIDs filters by price.
	// Uses the (tenant_id, environment_id, price_id, status) index.
	PriceIDs []string `json:"price_ids,omitempty"`

	// MeterIDs filters by meter.
	// Uses the (tenant_id, environment_id, meter_id, status) index.
	MeterIDs []string `json:"meter_ids,omitempty"`

	// EntityIDs filters by entity ID.
	EntityIDs []string `json:"entity_ids,omitempty"`

	// EntityType filters by a single entity type.
	EntityType *string `json:"entity_type,omitempty"`

	// Currencies filters by currency code (e.g. "USD", "EUR").
	Currencies []string `json:"currencies,omitempty"`

	// PeriodStart filters line items whose period_start >= this value.
	PeriodStart *time.Time `json:"period_start,omitempty"`

	// PeriodEnd filters line items whose period_end <= this value.
	PeriodEnd *time.Time `json:"period_end,omitempty"`
}

// NewDefaultInvoiceLineItemFilter returns a filter with sane defaults.
func NewDefaultInvoiceLineItemFilter() *InvoiceLineItemFilter {
	return &InvoiceLineItemFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitInvoiceLineItemFilter returns a filter that retrieves all results.
func NewNoLimitInvoiceLineItemFilter() *InvoiceLineItemFilter {
	return &InvoiceLineItemFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the invoice line item filter.
func (f *InvoiceLineItemFilter) Validate() error {
	if f == nil {
		return nil
	}
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}

	if f.PeriodStart != nil && f.PeriodEnd != nil {
		if f.PeriodStart.After(*f.PeriodEnd) {
			return ierr.NewError("period_start must be before period_end").Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// GetLimit returns the limit value for the filter.
func (f *InvoiceLineItemFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset returns the offset value for the filter.
func (f *InvoiceLineItemFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort returns the sort value for the filter.
func (f *InvoiceLineItemFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder returns the order value for the filter.
func (f *InvoiceLineItemFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus returns the status value for the filter.
func (f *InvoiceLineItemFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand returns the expand value for the filter.
func (f *InvoiceLineItemFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns whether the filter has unlimited pagination.
func (f *InvoiceLineItemFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return false
	}
	return f.QueryFilter.IsUnlimited()
}
