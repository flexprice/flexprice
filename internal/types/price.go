package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// BillingModel is the billing model for the price ex FLAT_FEE, PACKAGE, TIERED
type BillingModel string

// BillingPeriod is the billing period for the price ex MONTHLY, ANNUAL, WEEKLY, DAILY
type BillingPeriod string

// BillingCadence is the billing cadence for the price ex RECURRING, ONETIME
type BillingCadence string

// BillingTier when Billing model is TIERED defines how to
// calculate the price for a given quantity
type BillingTier string

// PriceType is the type of the price ex USAGE, FIXED
type PriceType string

const (
	PRICE_TYPE_USAGE PriceType = "USAGE"
	PRICE_TYPE_FIXED PriceType = "FIXED"

	// Billing model for a flat fee per unit
	BILLING_MODEL_FLAT_FEE BillingModel = "FLAT_FEE"

	// Billing model for a package of units ex 1000 emails for $100
	BILLING_MODEL_PACKAGE BillingModel = "PACKAGE"

	// Billing model for a tiered pricing model
	// ex 1-100 emails for $100, 101-1000 emails for $90
	BILLING_MODEL_TIERED BillingModel = "TIERED"

	// For BILLING_CADENCE_RECURRING
	BILLING_PERIOD_MONTHLY   BillingPeriod = "MONTHLY"
	BILLING_PERIOD_ANNUAL    BillingPeriod = "ANNUAL"
	BILLING_PERIOD_WEEKLY    BillingPeriod = "WEEKLY"
	BILLING_PERIOD_DAILY     BillingPeriod = "DAILY"
	BILLING_PERIOD_QUARTER   BillingPeriod = "QUARTERLY"
	BILLING_PERIOD_HALF_YEAR BillingPeriod = "HALF_YEARLY"

	BILLING_CADENCE_RECURRING BillingCadence = "RECURRING"
	BILLING_CADENCE_ONETIME   BillingCadence = "ONETIME"

	// BILLING_TIER_VOLUME means all units price based on final tier reached.
	BILLING_TIER_VOLUME BillingTier = "VOLUME"

	// BILLING_TIER_SLAB means Tiers apply progressively as quantity increases
	BILLING_TIER_SLAB BillingTier = "SLAB"

	// MAX_BILLING_AMOUNT is the maximum allowed billing amount (as a safeguard)
	MAX_BILLING_AMOUNT = 1000000000000 // 1 trillion

	// ROUND_UP rounds to the ceiling value ex 1.99 -> 2.00
	ROUND_UP = "up"
	// ROUND_DOWN rounds to the floor value ex 1.99 -> 1.00
	ROUND_DOWN = "down"

	// DEFAULT_FLOATING_PRECISION is the default floating point precision
	DEFAULT_FLOATING_PRECISION = 2
)

func (b BillingCadence) Validate() error {
	allowed := []BillingCadence{
		BILLING_CADENCE_RECURRING,
		BILLING_CADENCE_ONETIME,
	}
	if !lo.Contains(allowed, b) {
		return ierr.NewError("invalid billing cadence").
			WithHint("Invalid billing cadence").
			WithReportableDetails(map[string]interface{}{
				"billing_cadence": b,
				"allowed":         allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (b BillingPeriod) Validate() error {
	if b == "" {
		return nil
	}

	allowed := []BillingPeriod{
		BILLING_PERIOD_MONTHLY,
		BILLING_PERIOD_ANNUAL,
		BILLING_PERIOD_WEEKLY,
		BILLING_PERIOD_DAILY,
		BILLING_PERIOD_QUARTER,
		BILLING_PERIOD_HALF_YEAR,
	}
	if !lo.Contains(allowed, b) {
		return ierr.NewError("invalid billing period").
			WithHint("Invalid billing period").
			WithReportableDetails(map[string]interface{}{
				"billing_period": b,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (b BillingModel) Validate() error {
	allowed := []BillingModel{
		BILLING_MODEL_FLAT_FEE,
		BILLING_MODEL_PACKAGE,
		BILLING_MODEL_TIERED,
	}
	if !lo.Contains(allowed, b) {
		return ierr.NewError("invalid billing model").
			WithHint("Invalid billing model").
			WithReportableDetails(map[string]interface{}{
				"billing_model": b,
				"allowed":       allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (b BillingTier) Validate() error {
	allowed := []BillingTier{
		BILLING_TIER_VOLUME,
		BILLING_TIER_SLAB,
	}
	if !lo.Contains(allowed, b) {
		return ierr.NewError("invalid billing tier").
			WithHint("Invalid billing tier").
			WithReportableDetails(map[string]interface{}{
				"billing_tier": b,
				"allowed":      allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (p PriceType) Validate() error {
	allowed := []PriceType{
		PRICE_TYPE_USAGE,
		PRICE_TYPE_FIXED,
	}
	if !lo.Contains(allowed, p) {
		return ierr.NewError("invalid price type").
			WithHint("Invalid price type").
			WithReportableDetails(map[string]interface{}{
				"price_type": p,
				"allowed":    allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// PriceFilter represents filters for price queries
type PriceFilter struct {
	*QueryFilter
	*TimeRangeFilter
	PlanIDs  []string `json:"plan_ids,omitempty" form:"plan_ids"`
	PriceIDs []string `json:"price_ids,omitempty" form:"price_ids"`
}

// NewPriceFilter creates a new PriceFilter with default values
func NewPriceFilter() *PriceFilter {
	return &PriceFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitPriceFilter creates a new PriceFilter with no pagination limits
func NewNoLimitPriceFilter() *PriceFilter {
	return &PriceFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

func (f PriceFilter) Validate() error {
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

	for _, planID := range f.PlanIDs {
		if planID == "" {
			return ierr.NewError("plan id can not be empty").
				WithHint("Plan ID cannot be empty").
				Mark(ierr.ErrValidation)
		}
	}

	for _, priceID := range f.PriceIDs {
		if priceID == "" {
			return ierr.NewError("price id can not be empty").
				WithHint("Price ID cannot be empty").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// WithPlanIDs adds plan IDs to the filter
func (f *PriceFilter) WithPlanIDs(planIDs []string) *PriceFilter {
	f.PlanIDs = planIDs
	return f
}

// WithPriceIDs adds price IDs to the filter
func (f *PriceFilter) WithPriceIDs(priceIDs []string) *PriceFilter {
	f.PriceIDs = priceIDs
	return f
}

// WithStatus sets the status on the filter
func (f *PriceFilter) WithStatus(status Status) *PriceFilter {
	f.Status = &status
	return f
}

// WithExpand sets the expand field on the filter
func (f *PriceFilter) WithExpand(expand string) *PriceFilter {
	f.Expand = &expand
	return f
}

// GetLimit implements BaseFilter interface
func (f *PriceFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *PriceFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *PriceFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *PriceFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *PriceFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *PriceFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *PriceFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
