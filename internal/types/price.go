package types

import (
	"fmt"

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
	BILLING_PERIOD_MONTHLY BillingPeriod = "MONTHLY"
	BILLING_PERIOD_ANNUAL  BillingPeriod = "ANNUAL"
	BILLING_PERIOD_WEEKLY  BillingPeriod = "WEEKLY"
	BILLING_PERIOD_DAILY   BillingPeriod = "DAILY"

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
	// ROUND_NEAREST rounds to the nearest value ex
	ROUND_NEAREST = "nearest"

	// DEFAULT_FLOATING_PRECISION is the default floating point precision
	DEFAULT_FLOATING_PRECISION = 2
)

func (b BillingCadence) Validate() error {
	allowed := []BillingCadence{
		BILLING_CADENCE_RECURRING,
		BILLING_CADENCE_ONETIME,
	}
	if !lo.Contains(allowed, b) {
		return fmt.Errorf("invalid billing cadence: %s", b)
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
	}
	if !lo.Contains(allowed, b) {
		return fmt.Errorf("invalid billing period: %s", b)
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
		return fmt.Errorf("invalid billing model: %s", b)
	}
	return nil
}

func (b BillingTier) Validate() error {
	allowed := []BillingTier{
		BILLING_TIER_VOLUME,
		BILLING_TIER_SLAB,
	}
	if !lo.Contains(allowed, b) {
		return fmt.Errorf("invalid billing tier: %s", b)
	}
	return nil
}

func (p PriceType) Validate() error {
	allowed := []PriceType{
		PRICE_TYPE_USAGE,
		PRICE_TYPE_FIXED,
	}
	if !lo.Contains(allowed, p) {
		return fmt.Errorf("invalid price type: %s", p)
	}
	return nil
}
