package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// CouponApplicationFilter represents filters for coupon application queries
type CouponApplicationFilter struct {
	*QueryFilter

	Filters []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	// InvoiceIDs filters by invoice IDs (can be a single ID in array)
	InvoiceIDs []string `json:"invoice_ids,omitempty" form:"invoice_ids"`
	// SubscriptionIDs filters by subscription IDs (can be a single ID in array)
	SubscriptionIDs []string `json:"subscription_ids,omitempty" form:"subscription_ids"`
	// CouponIDs filters by coupon IDs (can be a single ID in array)
	CouponIDs []string `json:"coupon_ids,omitempty" form:"coupon_ids"`
	// InvoiceLineItemIDs filters by invoice line item IDs (can be a single ID in array)
	InvoiceLineItemIDs []string `json:"invoice_line_item_ids,omitempty" form:"invoice_line_item_ids"`
	// CouponAssociationIDs filters by coupon association IDs (can be a single ID in array)
	CouponAssociationIDs []string `json:"coupon_association_ids,omitempty" form:"coupon_association_ids"`
}

// NewCouponApplicationFilter creates a new CouponApplicationFilter with default values
func NewCouponApplicationFilter() *CouponApplicationFilter {
	return &CouponApplicationFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitCouponApplicationFilter creates a new CouponApplicationFilter with no pagination limits
func NewNoLimitCouponApplicationFilter() *CouponApplicationFilter {
	return &CouponApplicationFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the coupon application filter
func (f *CouponApplicationFilter) Validate() error {
	if f.QueryFilter == nil {
		return ierr.NewError("query_filter is required").
			WithHint("QueryFilter must be initialized").
			Mark(ierr.ErrValidation)
	}

	return f.QueryFilter.Validate()
}

// WithExpand sets the expand on the filter
func (f *CouponApplicationFilter) WithExpand(expand string) *CouponApplicationFilter {
	f.Expand = &expand
	return f
}

// GetLimit implements BaseFilter interface
func (f *CouponApplicationFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *CouponApplicationFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *CouponApplicationFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *CouponApplicationFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *CouponApplicationFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *CouponApplicationFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns true if this is an unlimited query
func (f *CouponApplicationFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
