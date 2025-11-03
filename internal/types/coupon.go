package types

import (
	"fmt"
	"strings"
	"time"

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

// CouponAssociationFilter represents filters for coupon association queries
type CouponAssociationFilter struct {
	*QueryFilter

	Filters []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	// SubscriptionIDs filters by subscription IDs (can be a single ID in array)
	SubscriptionIDs []string `json:"subscription_ids,omitempty" form:"subscription_ids"`
	// CouponIDs filters by coupon IDs (can be a single ID in array)
	CouponIDs []string `json:"coupon_ids,omitempty" form:"coupon_ids"`
	// SubscriptionLineItemIDs filters by subscription line item IDs (can be a single ID in array)
	SubscriptionLineItemIDs []string `json:"subscription_line_item_ids,omitempty" form:"subscription_line_item_ids"`
	// SubscriptionLineItemIDIsNil filters for subscription-level associations (no line items)
	// When true, returns associations with no line item. When false, returns associations with line items.
	SubscriptionLineItemIDIsNil *bool `json:"subscription_line_item_id_is_nil,omitempty" form:"subscription_line_item_id_is_nil"`
	// SubscriptionPhaseIDs filters by subscription phase IDs (can be a single ID in array)
	SubscriptionPhaseIDs []string `json:"subscription_phase_ids,omitempty" form:"subscription_phase_ids"`
	// WithCoupon includes coupon relation in the response
	WithCoupon bool `json:"with_coupon,omitempty" form:"with_coupon"`
	// ActiveOnly filters to only return active associations based on start_date and end_date
	// When ActiveOnly is true, the association must overlap with the period specified by ActivePeriodStart and ActivePeriodEnd
	// If ActivePeriodStart/ActivePeriodEnd are not provided, uses current time (now())
	// An association is active during a period if:
	// - start_date <= active_period_end (association started before or during the period)
	// - AND (end_date IS NULL OR end_date >= active_period_start) (association hasn't ended before the period or is indefinite)
	ActiveOnly bool `json:"active_only,omitempty" form:"active_only"`
	// ActivePeriodStart is the start of the period to check if associations are active (used with ActiveOnly)
	// If not provided and ActiveOnly is true, uses current time
	ActivePeriodStart *time.Time `json:"active_period_start,omitempty" form:"active_period_start"`
	// ActivePeriodEnd is the end of the period to check if associations are active (used with ActiveOnly)
	// If not provided and ActiveOnly is true, uses current time
	ActivePeriodEnd *time.Time `json:"active_period_end,omitempty" form:"active_period_end"`
}

// NewCouponAssociationFilter creates a new CouponAssociationFilter with default values
func NewCouponAssociationFilter() *CouponAssociationFilter {
	return &CouponAssociationFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitCouponAssociationFilter creates a new CouponAssociationFilter with no pagination limits
func NewNoLimitCouponAssociationFilter() *CouponAssociationFilter {
	return &CouponAssociationFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the coupon association filter
func (f *CouponAssociationFilter) Validate() error {
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

	return nil
}

// GetLimit implements BaseFilter interface for CouponAssociationFilter
func (f *CouponAssociationFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface for CouponAssociationFilter
func (f *CouponAssociationFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface for CouponAssociationFilter
func (f *CouponAssociationFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface for CouponAssociationFilter
func (f *CouponAssociationFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface for CouponAssociationFilter
func (f *CouponAssociationFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface for CouponAssociationFilter
func (f *CouponAssociationFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *CouponAssociationFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}

// Validate validates the coupon filter
func (f *CouponFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	for _, filter := range f.Filters {
		if filter != nil {
			if err := filter.Validate(); err != nil {
				return err
			}
		}
	}

	for _, sort := range f.Sort {
		if sort != nil {
			if err := sort.Validate(); err != nil {
				return err
			}
		}
	}

	// Validate coupon IDs if provided
	for _, id := range f.CouponIDs {
		if err := ValidateCouponID(id); err != nil {
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
