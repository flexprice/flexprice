package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
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

// PriceScope indicates whether a price is at the plan level or subscription level
type PriceScope string

// PriceEntityType is the type of the entity that the price is associated with
// i.e. PLAN, SUBSCRIPTION, ADDON, PRICE
// If price is created for plan then it will have PLAN as entity type with entity id as plan id
// If prices is create for subscription then it will have SUBSCRIPTION as entity type with enitiy id as subscription id
// If prices is create for addon then it will have ADDON as entity type with enitiy id as addon id
// If prices is create for price overrides in subscription creation	 then it will have PRICE as entity type with enitiy id as price id
type PriceEntityType string

const (
	PRICE_ENTITY_TYPE_PLAN         PriceEntityType = "PLAN"
	PRICE_ENTITY_TYPE_SUBSCRIPTION PriceEntityType = "SUBSCRIPTION"
	PRICE_ENTITY_TYPE_ADDON        PriceEntityType = "ADDON"
	PRICE_ENTITY_TYPE_PRICE        PriceEntityType = "PRICE"
	PRICE_ENTITY_TYPE_COSTSHEET    PriceEntityType = "COSTSHEET"
)

func (p PriceEntityType) Validate() error {
	allowed := []PriceEntityType{
		PRICE_ENTITY_TYPE_PLAN,
		PRICE_ENTITY_TYPE_SUBSCRIPTION,
		PRICE_ENTITY_TYPE_ADDON,
		PRICE_ENTITY_TYPE_PRICE,
		PRICE_ENTITY_TYPE_COSTSHEET,
	}
	if p != "" && !lo.Contains(allowed, p) {
		return ierr.NewError("invalid price entity type").
			WithHint("Invalid price entity type").
			WithReportableDetails(map[string]interface{}{
				"price_entity_type": p,
				"allowed":           allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// Additional types needed for JSON fields
type PriceTier struct {
	// up_to is the quantity up to which this tier applies. It is null for the last tier.
	// IMPORTANT: Tier boundaries are INCLUSIVE.
	// - If up_to is 1000, then quantity less than or equal to 1000 belongs to this tier
	// - This behavior is consistent across both VOLUME and SLAB tier modes
	UpTo *uint64 `json:"up_to"`

	// unit_amount is the amount per unit for the given tier
	UnitAmount decimal.Decimal `json:"unit_amount"`

	// flat_amount is the flat amount for the given tier (optional)
	// Applied on top of unit_amount*quantity. Useful for cases like "2.7$ + 5c"
	FlatAmount *decimal.Decimal `json:"flat_amount,omitempty"`
}

type TransformQuantity struct {
	DivideBy int    `json:"divide_by,omitempty"`
	Round    string `json:"round,omitempty"`
}

const (
	PRICE_TYPE_USAGE PriceType = "USAGE"
	PRICE_TYPE_FIXED PriceType = "FIXED"

	// Price scope constants
	PRICE_SCOPE_PLAN         PriceScope = "PLAN"
	PRICE_SCOPE_SUBSCRIPTION PriceScope = "SUBSCRIPTION"

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
	// Tier boundaries are INCLUSIVE: if up_to is 1000, quantity 1000 belongs to this tier
	BILLING_TIER_VOLUME BillingTier = "VOLUME"

	// BILLING_TIER_SLAB means Tiers apply progressively as quantity increases
	// Tier boundaries are INCLUSIVE: if up_to is 1000, quantity 1000 belongs to this tier
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
	if b != "" && !lo.Contains(allowed, b) {
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

func (b BillingPeriod) String() string {
	return string(b)
}

func (b BillingModel) Validate() error {
	allowed := []BillingModel{
		BILLING_MODEL_FLAT_FEE,
		BILLING_MODEL_PACKAGE,
		BILLING_MODEL_TIERED,
	}

	if b != "" && !lo.Contains(allowed, b) {
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
	if b != "" && !lo.Contains(allowed, b) {
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

	if p != "" && !lo.Contains(allowed, p) {
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

func (p PriceScope) Validate() error {
	allowed := []PriceScope{
		PRICE_SCOPE_PLAN,
		PRICE_SCOPE_SUBSCRIPTION,
	}

	if p != "" && !lo.Contains(allowed, p) {
		return ierr.NewError("invalid price scope").
			WithHint("Invalid price scope").
			WithReportableDetails(map[string]interface{}{
				"price_scope": p,
				"allowed":     allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// PriceUnitType is the type of the price unit ex FIAT, CUSTOM
type PriceUnitType string

const (
	PRICE_UNIT_TYPE_FIAT   PriceUnitType = "FIAT"
	PRICE_UNIT_TYPE_CUSTOM PriceUnitType = "CUSTOM"
)

func (p PriceUnitType) Validate() error {
	allowed := []PriceUnitType{
		PRICE_UNIT_TYPE_FIAT,
		PRICE_UNIT_TYPE_CUSTOM,
	}

	if p != "" && !lo.Contains(allowed, p) {
		return ierr.NewError("invalid price unit type").
			WithHint("Price unit type must be either FIAT or CUSTOM").
			WithReportableDetails(map[string]interface{}{
				"price_unit_type": p,
				"allowed":         allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// PriceFilter represents filters for price queries
type PriceFilter struct {
	*QueryFilter
	*TimeRangeFilter
	PriceIDs []string `json:"price_ids,omitempty" form:"price_ids"`
	// Price override filtering fields
	PlanIDs            []string         `json:"plan_ids,omitempty" form:"plan_ids"`
	EntityType         *PriceEntityType `json:"entity_type,omitempty" form:"entity_type"`
	EntityIDs          []string         `json:"entity_ids,omitempty" form:"entity_ids"`
	SubscriptionID     *string          `json:"subscription_id,omitempty" form:"subscription_id"`
	ParentPriceID      *string          `json:"parent_price_id,omitempty" form:"parent_price_id"`
	MeterIDs           []string         `json:"meter_ids,omitempty" form:"meter_ids"`
	AllowExpiredPrices bool             `json:"allow_expired_prices,omitempty" form:"allow_expired_prices" default:"false"`

	StartDateLT *time.Time `json:"start_date_lt,omitempty" form:"start_date_lt"`
}

// NewPriceFilter creates a new PriceFilter with default values
func NewPriceFilter() *PriceFilter {
	return &PriceFilter{
		QueryFilter:        NewDefaultQueryFilter(),
		AllowExpiredPrices: false,
	}
}

// NewNoLimitPriceFilter creates a new PriceFilter with no pagination limits
func NewNoLimitPriceFilter() *PriceFilter {
	return &PriceFilter{
		QueryFilter:        NewNoLimitQueryFilter(),
		AllowExpiredPrices: false,
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

	for _, priceID := range f.PriceIDs {
		if priceID == "" {
			return ierr.NewError("price id can not be empty").
				WithHint("Price ID cannot be empty").
				Mark(ierr.ErrValidation)
		}
	}

	for _, planID := range f.PlanIDs {
		if planID == "" {
			return ierr.NewError("plan id can not be empty").
				WithHint("Plan ID cannot be empty").
				Mark(ierr.ErrValidation)
		}
	}

	// Validate entity type if provided
	if f.EntityType != nil {
		if err := f.EntityType.Validate(); err != nil {
			return err
		}
	}

	// Validate subscription ID if provided
	if f.SubscriptionID != nil && *f.SubscriptionID == "" {
		return ierr.NewError("subscription ID can not be empty").
			WithHint("Subscription ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Validate parent price ID if provided
	if f.ParentPriceID != nil && *f.ParentPriceID == "" {
		return ierr.NewError("parent price ID can not be empty").
			WithHint("Parent price ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Validate entity IDs if provided
	if len(f.EntityIDs) > 0 {
		for _, entityID := range f.EntityIDs {
			if entityID == "" {
				return ierr.NewError("entity ID can not be empty").
					WithHint("Entity ID cannot be empty").
					Mark(ierr.ErrValidation)
			}
		}
	}

	// Validate meter IDs if provided
	if len(f.MeterIDs) > 0 {
		for _, meterID := range f.MeterIDs {
			if meterID == "" {
				return ierr.NewError("meter ID can not be empty").
					WithHint("Meter ID cannot be empty").
					Mark(ierr.ErrValidation)
			}
		}
	}
	return nil
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

// WithAllowExpiredPrices sets the allow expired prices flag on the filter
func (f *PriceFilter) WithAllowExpiredPrices(allowExpiredPrices bool) *PriceFilter {
	f.AllowExpiredPrices = allowExpiredPrices
	return f
}

// WithEntityType sets the entity type on the filter
func (f *PriceFilter) WithEntityType(entityType PriceEntityType) *PriceFilter {
	f.EntityType = &entityType
	return f
}

// WithEntityIDs adds entity IDs to the filter
func (f *PriceFilter) WithEntityIDs(entityIDs []string) *PriceFilter {
	f.EntityIDs = entityIDs
	return f
}

// WithExpand sets the expand field on the filter
func (f *PriceFilter) WithExpand(expand string) *PriceFilter {
	f.Expand = &expand
	return f
}

// WithSubscriptionID sets the subscription ID filter
func (f *PriceFilter) WithSubscriptionID(subscriptionID string) *PriceFilter {
	f.SubscriptionID = &subscriptionID
	return f
}

// WithParentPriceID sets the parent price ID filter
func (f *PriceFilter) WithParentPriceID(parentPriceID string) *PriceFilter {
	f.ParentPriceID = &parentPriceID
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
