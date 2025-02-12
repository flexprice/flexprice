package types

import (
	"fmt"
)

// CustomerFilter represents filters for customer queries
type CustomerFilter struct {
	*QueryFilter
	*TimeRangeFilter
	CustomerIDs []string `json:"customer_ids,omitempty" form:"customer_ids" validate:"omitempty"`
	ExternalID  string   `json:"external_id,omitempty" form:"external_id" validate:"omitempty"`
	Email       string   `json:"email,omitempty" form:"email" validate:"omitempty,email"`
}

// NewCustomerFilter creates a new CustomerFilter with default values
func NewCustomerFilter() *CustomerFilter {
	return &CustomerFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitCustomerFilter creates a new CustomerFilter with no pagination limits
func NewNoLimitCustomerFilter() *CustomerFilter {
	return &CustomerFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the customer filter
func (f CustomerFilter) Validate() error {
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

	if f.Email != "" && !IsValidEmail(f.Email) {
		return fmt.Errorf("invalid email")
	}

	return nil
}

// GetLimit implements BaseFilter interface
func (f *CustomerFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *CustomerFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *CustomerFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *CustomerFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *CustomerFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *CustomerFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *CustomerFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
