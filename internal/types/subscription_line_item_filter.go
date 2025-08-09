package types

// SubscriptionLineItemFilter defines filters for querying subscription line items
type SubscriptionLineItemFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// Specific filters
	SubscriptionIDs []string                         `json:"subscription_ids,omitempty" form:"subscription_ids"`
	CustomerIDs     []string                         `json:"customer_ids,omitempty" form:"customer_ids"`
	PriceIDs        []string                         `json:"price_ids,omitempty" form:"price_ids"`
	MeterIDs        []string                         `json:"meter_ids,omitempty" form:"meter_ids"`
	Currencies      []string                         `json:"currencies,omitempty" form:"currencies"`
	BillingPeriods  []string                         `json:"billing_periods,omitempty" form:"billing_periods"`
	EntityIDs       []string                         `json:"entity_ids,omitempty" form:"entity_ids"`
	EntityType      *SubscriptionLineItemEntitiyType `json:"entity_type,omitempty" form:"entity_type"`

	// TODO: !REMOVE after migration
	PlanIDs []string `json:"plan_ids,omitempty" form:"plan_ids"`
}

// NewSubscriptionLineItemFilter creates a new subscription line item filter with default options
func NewSubscriptionLineItemFilter() *SubscriptionLineItemFilter {
	return &SubscriptionLineItemFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitSubscriptionLineItemFilter creates a new subscription line item filter without pagination
func NewNoLimitSubscriptionLineItemFilter() *SubscriptionLineItemFilter {
	return &SubscriptionLineItemFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the subscription line item filter
func (f *SubscriptionLineItemFilter) Validate() error {
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

	return nil
}

// GetLimit returns the limit value for the filter
func (f *SubscriptionLineItemFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset returns the offset value for the filter
func (f *SubscriptionLineItemFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort returns the sort value for the filter
func (f *SubscriptionLineItemFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder returns the order value for the filter
func (f *SubscriptionLineItemFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus returns the status value for the filter
func (f *SubscriptionLineItemFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand returns the expand value for the filter
func (f *SubscriptionLineItemFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns whether the filter has unlimited pagination
func (f *SubscriptionLineItemFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return false
	}
	return f.QueryFilter.IsUnlimited()
}
