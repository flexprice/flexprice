package types

import (
	"fmt"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// CustomerFilter represents filters for customer queries
type CustomerFilter struct {
	*QueryFilter
	*TimeRangeFilter
	// filters allows complex filtering based on multiple fields

	Filters     []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort        []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	CustomerIDs []string           `json:"customer_ids,omitempty" form:"customer_ids" validate:"omitempty"`
	ExternalIDs []string           `json:"external_ids,omitempty" form:"external_ids" validate:"omitempty"`
	ExternalID  string             `json:"external_id,omitempty" form:"external_id" validate:"omitempty"`
	Email       string             `json:"email,omitempty" form:"email" validate:"omitempty,email"`
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
		return ierr.NewError("invalid email").
			WithHint("Please provide a valid email").
			Mark(ierr.ErrValidation)
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

// Common validation rules for IDs
func validateID(id string, idType string) error {

	// Sample Constraints for customer id and external customer id
	// 1. Cannot contain invalid characters % and space

	invalidChars := []string{"%", " "}
	for _, char := range invalidChars {
		if strings.Contains(id, char) {
			return ierr.NewError(fmt.Sprintf("invalid %s", idType)).
				WithHint(fmt.Sprintf("Please provide a valid %s - cannot contain: %s", idType, char)).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// ValidateCustomerID validates the customer id
func ValidateCustomerID(id string) error {

	if strings.HasPrefix(id, "_") || strings.HasSuffix(id, "_") {
		return ierr.NewError("invalid customer id").
			WithHint("Please provide a valid customer id").
			Mark(ierr.ErrValidation)
	}

	return validateID(id, "customer id")
}

// ValidateExternalCustomerID validates the external customer id
func ValidateExternalCustomerID(id string) error {
	return validateID(id, "external customer id")
}
