package types

import (
	"fmt"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// CouponType represents the type of coupon discount (fixed or percentage)
type CouponType string

const (
	// CouponTypeFixed represents a fixed amount coupon discount
	CouponTypeFixed CouponType = "fixed"
	// CouponTypePercentage represents a percentage-based coupon discount
	CouponTypePercentage CouponType = "percentage"
)

// CouponCadence represents the duration type of coupon discount
type CouponCadence string

const (
	// CouponCadenceOnce represents a one-time coupon discount
	CouponCadenceOnce CouponCadence = "once"
	// CouponCadenceRepeated represents a coupon discount that repeats for a specific period
	CouponCadenceRepeated CouponCadence = "repeated"
	// CouponCadenceForever represents a coupon discount that applies forever
	CouponCadenceForever CouponCadence = "forever"
)

// CouponRule represents a rule for applying coupon discounts
type CouponRule struct {
	Field    string      `json:"field"`    // Field to check (e.g., "customer_id", "plan_id", "amount")
	Operator string      `json:"operator"` // Operator (e.g., "equals", "greater_than", "in")
	Value    interface{} `json:"value"`    // Value to compare against
}

// CouponRules represents a collection of coupon discount rules
type CouponRules struct {
	Inclusions []CouponRule `json:"inclusions"` // All conditions must be met (AND logic)
	Exclusions []CouponRule `json:"exclusions"` // Any exclusion prevents application (OR logic)
}

type CouponFilter struct {
	*QueryFilter

	Filters   []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort      []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	CouponIDs []string           `json:"coupon_ids,omitempty" form:"coupon_ids" validate:"omitempty"`
}

// NewCouponFilter creates a new CouponFilter with default values
func NewCouponFilter() *CouponFilter {
	return &CouponFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitCouponFilter creates a new CouponFilter with no pagination limits
func NewNoLimitCouponFilter() *CouponFilter {
	return &CouponFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the coupon filter
func (f CouponFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	for _, filter := range f.Filters {
		if err := filter.Validate(); err != nil {
			return err
		}
	}

	for _, sort := range f.Sort {
		if err := sort.Validate(); err != nil {
			return err
		}
	}

	for _, couponID := range f.CouponIDs {
		if err := ValidateCouponID(couponID); err != nil {
			return err
		}
	}

	return nil
}

// GetLimit implements BaseFilter interface
func (f *CouponFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *CouponFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *CouponFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *CouponFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *CouponFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *CouponFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *CouponFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}

// Common validation rules for IDs
func validateCouponID(id string, idType string) error {

	// Sample Constraints for coupon id
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

// ValidateCouponID validates the coupon id
func ValidateCouponID(id string) error {

	if strings.HasPrefix(id, "_") || strings.HasSuffix(id, "_") {
		return ierr.NewError("invalid coupon id").
			WithHint("Please provide a valid coupon id").
			Mark(ierr.ErrValidation)
	}

	return validateCouponID(id, "coupon id")
}
