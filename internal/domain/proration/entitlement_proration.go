package proration

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entitlement"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// EntitlementProrationParams holds input for calculating entitlement proration
type EntitlementProrationParams struct {
	// Period information
	PeriodStart      time.Time // Start of the billing period
	PeriodEnd        time.Time // End of the billing period
	ProrationDate    time.Time // Date from which to prorate (start date or change date)
	CustomerTimezone string    // Customer's timezone for accurate calculation

	// Billing cycle information (for calendar billing proration)
	BillingCycle       types.BillingCycle  // Anniversary or Calendar
	BillingAnchor      time.Time           // Billing anchor date
	BillingPeriod      types.BillingPeriod // Monthly, Annual, etc.
	BillingPeriodCount int                 // Number of billing periods

	// Plan entitlements to prorate
	PlanEntitlements []*entitlement.Entitlement

	// Proration strategy
	Strategy types.ProrationStrategy // Default: StrategySecondBased
}

// EntitlementProrationResult holds the output of entitlement proration calculation
type EntitlementProrationResult struct {
	// Prorated limits by feature ID
	ProratedLimits map[string]int64 // feature_id -> prorated_limit

	// Calculation metadata
	ProrationCoefficient decimal.Decimal // The coefficient used (remaining_time / total_time)
	PeriodStart          time.Time       // Period start used
	PeriodEnd            time.Time       // Period end used
	ProrationDate        time.Time       // Proration date used
	TotalDays            int             // Total days in period
	RemainingDays        int             // Remaining days from proration date

	// Entitlement details
	EntitlementDetails []EntitlementProrationDetail

	// Additive proration metadata (for plan changes)
	IsAdditive          bool                               // Whether this is additive proration
	OldPlanContribution map[string]AdditiveProrationDetail // Old plan contribution by feature
	NewPlanContribution map[string]AdditiveProrationDetail // New plan contribution by feature
}

// EntitlementProrationDetail holds details for a single entitlement proration
type EntitlementProrationDetail struct {
	FeatureID        string          // Feature ID
	FeatureName      string          // Feature name (if available)
	OriginalLimit    int64           // Original limit from plan
	ProratedLimit    int64           // Calculated prorated limit
	Coefficient      decimal.Decimal // Coefficient used for this entitlement
	ParentID         string          // Parent entitlement ID
	UsageResetPeriod types.EntitlementUsageResetPeriod
}

// AdditiveProrationDetail holds details for additive proration breakdown
type AdditiveProrationDetail struct {
	PlanID        string          // Plan ID
	OriginalLimit int64           // Original limit from plan
	ProratedLimit int64           // Prorated limit
	Coefficient   decimal.Decimal // Coefficient used
}

// EntitlementProrationCalculator handles entitlement proration calculations
type EntitlementProrationCalculator struct {
	logger              *logger.Logger
	prorationCalculator Calculator // Reuse existing proration calculator
}

// NewEntitlementProrationCalculator creates a new entitlement proration calculator
func NewEntitlementProrationCalculator(logger *logger.Logger, prorationCalculator Calculator) *EntitlementProrationCalculator {
	return &EntitlementProrationCalculator{
		logger:              logger,
		prorationCalculator: prorationCalculator,
	}
}

// CalculateEntitlementProration calculates prorated entitlement limits
func (c *EntitlementProrationCalculator) CalculateEntitlementProration(
	ctx context.Context,
	params EntitlementProrationParams,
) (*EntitlementProrationResult, error) {
	c.logger.Infow("calculating entitlement proration",
		"period_start", params.PeriodStart,
		"period_end", params.PeriodEnd,
		"proration_date", params.ProrationDate,
		"entitlements_count", len(params.PlanEntitlements))

	// Validate parameters
	if err := c.validateParams(params); err != nil {
		return nil, err
	}

	// Calculate proration coefficient using existing calculator
	coefficient, err := c.calculateProrationCoefficient(params)
	if err != nil {
		return nil, err
	}

	c.logger.Debugw("proration coefficient calculated",
		"coefficient", coefficient.String())

	// Filter metered entitlements with usage limits
	meteredEntitlements := c.filterMeteredEntitlements(params.PlanEntitlements)
	c.logger.Debugw("filtered metered entitlements",
		"total", len(params.PlanEntitlements),
		"metered_with_limits", len(meteredEntitlements))

	// Calculate prorated limits
	result := &EntitlementProrationResult{
		ProratedLimits:       make(map[string]int64),
		ProrationCoefficient: coefficient,
		PeriodStart:          params.PeriodStart,
		PeriodEnd:            params.PeriodEnd,
		ProrationDate:        params.ProrationDate,
		EntitlementDetails:   make([]EntitlementProrationDetail, 0, len(meteredEntitlements)),
	}

	// Calculate days for metadata
	result.TotalDays = c.calculateDays(params.PeriodStart, params.PeriodEnd)
	result.RemainingDays = c.calculateDays(params.ProrationDate, params.PeriodEnd)

	// Prorate each entitlement
	for _, ent := range meteredEntitlements {
		if ent.UsageLimit == nil {
			// Skip unlimited entitlements
			c.logger.Debugw("skipping unlimited entitlement",
				"feature_id", ent.FeatureID)
			continue
		}

		originalLimit := *ent.UsageLimit
		proratedLimit := c.calculateProratedLimit(originalLimit, coefficient)

		result.ProratedLimits[ent.FeatureID] = proratedLimit

		// Add detail
		result.EntitlementDetails = append(result.EntitlementDetails, EntitlementProrationDetail{
			FeatureID:        ent.FeatureID,
			OriginalLimit:    originalLimit,
			ProratedLimit:    proratedLimit,
			Coefficient:      coefficient,
			ParentID:         ent.ID,
			UsageResetPeriod: ent.UsageResetPeriod,
		})

		c.logger.Debugw("entitlement prorated",
			"feature_id", ent.FeatureID,
			"original_limit", originalLimit,
			"prorated_limit", proratedLimit)
	}

	c.logger.Infow("entitlement proration completed",
		"prorated_count", len(result.ProratedLimits),
		"coefficient", coefficient.String())

	return result, nil
}

// calculateProrationCoefficient calculates the proration coefficient by reusing the shared helper
// For calendar billing, it uses the full calendar period (e.g., Dec 1-31) to match price proration
// For anniversary billing, it uses the actual subscription period
func (c *EntitlementProrationCalculator) calculateProrationCoefficient(
	params EntitlementProrationParams,
) (decimal.Decimal, error) {
	// Load customer timezone
	loc, err := time.LoadLocation(params.CustomerTimezone)
	if err != nil {
		return decimal.Zero, ierr.WithError(err).
			WithHintf("failed to load customer timezone '%s'", params.CustomerTimezone).
			Mark(ierr.ErrSystem)
	}

	// Determine the period to use for proration calculation
	// For calendar billing, we need to use the full calendar period (like price proration does)
	var periodStart, periodEnd time.Time

	if params.BillingCycle == types.BillingCycleCalendar {
		// For calendar billing, calculate the previous billing date to get the full period
		// This ensures entitlement proration matches price proration
		// Example: Subscription starts Dec 26, billing anchor is Jan 1
		// Previous billing date is Dec 1, so we prorate based on 6/31 days
		previousBillingDate, err := types.PreviousBillingDate(
			params.BillingAnchor,
			params.BillingPeriodCount,
			params.BillingPeriod,
		)
		if err != nil {
			// Fallback to subscription period start if calculation fails
			c.logger.Warnw("failed to calculate previous billing date for calendar proration, using fallback",
				"error", err,
				"billing_anchor", params.BillingAnchor,
				"billing_period", params.BillingPeriod,
				"billing_period_count", params.BillingPeriodCount)
			periodStart = params.PeriodStart
		} else {
			periodStart = previousBillingDate
		}
		periodEnd = params.PeriodEnd
	} else {
		// For anniversary billing, use the actual subscription period
		periodStart = params.PeriodStart
		periodEnd = params.PeriodEnd
	}

	// Convert times to customer timezone
	prorationDateInTZ := params.ProrationDate.In(loc)
	periodStartInTZ := periodStart.In(loc)
	periodEndInTZ := periodEnd.In(loc)

	// Use the shared coefficient calculation helper
	// This eliminates code duplication and ensures consistency with price proration
	coefficient, err := calculateProrationCoefficient(
		periodStartInTZ,
		periodEndInTZ,
		prorationDateInTZ,
		loc,
		params.Strategy,
	)
	if err != nil {
		return decimal.Zero, err
	}

	c.logger.Debugw("proration coefficient calculated via shared helper",
		"coefficient", coefficient.String(),
		"period_start", periodStart,
		"period_end", periodEnd)

	return coefficient, nil
}

// calculateProratedLimit calculates the prorated limit with standard rounding
func (c *EntitlementProrationCalculator) calculateProratedLimit(
	originalLimit int64,
	coefficient decimal.Decimal,
) int64 {
	// Calculate prorated value
	proratedValue := decimal.NewFromInt(originalLimit).Mul(coefficient)

	// Round to nearest integer (standard rounding: 0.5 rounds up)
	rounded := proratedValue.Round(0)

	// Convert to int64
	proratedLimit := rounded.IntPart()

	// Ensure non-negative
	if proratedLimit < 0 {
		proratedLimit = 0
	}

	return proratedLimit
}

// filterMeteredEntitlements filters entitlements to only metered ones with limits
func (c *EntitlementProrationCalculator) filterMeteredEntitlements(
	entitlements []*entitlement.Entitlement,
) []*entitlement.Entitlement {
	filtered := make([]*entitlement.Entitlement, 0)

	for _, ent := range entitlements {
		// Only include metered features with usage limits
		if ent.FeatureType == types.FeatureTypeMetered && ent.UsageLimit != nil {
			filtered = append(filtered, ent)
		}
	}

	return filtered
}

// calculateDays calculates the number of days between two dates
func (c *EntitlementProrationCalculator) calculateDays(start, end time.Time) int {
	duration := end.Sub(start)
	days := int(duration.Hours() / 24)
	if days < 0 {
		days = 0
	}
	return days
}

// validateParams validates entitlement proration parameters
func (c *EntitlementProrationCalculator) validateParams(params EntitlementProrationParams) error {
	if params.PeriodStart.IsZero() {
		return ierr.NewError("period start is required").
			WithHint("Provide a valid period start date").
			Mark(ierr.ErrValidation)
	}

	if params.PeriodEnd.IsZero() {
		return ierr.NewError("period end is required").
			WithHint("Provide a valid period end date").
			Mark(ierr.ErrValidation)
	}

	if params.PeriodEnd.Before(params.PeriodStart) {
		return ierr.NewError("period end cannot be before period start").
			WithHint("Check period dates").
			Mark(ierr.ErrValidation)
	}

	if params.ProrationDate.IsZero() {
		return ierr.NewError("proration date is required").
			WithHint("Provide a valid proration date").
			Mark(ierr.ErrValidation)
	}

	if params.CustomerTimezone == "" {
		return ierr.NewError("customer timezone is required").
			WithHint("Provide a valid timezone").
			Mark(ierr.ErrValidation)
	}

	if params.PlanEntitlements == nil {
		return ierr.NewError("plan entitlements are required").
			WithHint("Provide plan entitlements to prorate").
			Mark(ierr.ErrValidation)
	}

	return nil
}
