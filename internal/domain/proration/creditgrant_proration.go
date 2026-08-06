package proration

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// creditGrantCreditsScale matches the numeric(20,8) precision of
// credit_grant_applications.credits.
const creditGrantCreditsScale = 8

// CreditGrantProrationParams describes the window a credit grant's first
// application covers relative to the full billing period it sits in.
type CreditGrantProrationParams struct {
	// PeriodStart/PeriodEnd bound the full billing period (the denominator).
	PeriodStart time.Time
	PeriodEnd   time.Time

	// ProrationDate is where coverage actually begins (the numerator start).
	ProrationDate time.Time

	Strategy        types.ProrationStrategy
	OriginalCredits decimal.Decimal
}

// CreditGrantProrationResult carries the scaled credits plus the inputs that
// produced them, so callers can record an audit trail.
type CreditGrantProrationResult struct {
	Coefficient     decimal.Decimal
	ProratedCredits decimal.Decimal
	OriginalCredits decimal.Decimal

	PeriodStart   time.Time
	PeriodEnd     time.Time
	ProrationDate time.Time
	Strategy      types.ProrationStrategy
}

// CalculateCreditGrantProration scales OriginalCredits by the fraction of the
// billing period remaining at ProrationDate.
func CalculateCreditGrantProration(params CreditGrantProrationParams) (*CreditGrantProrationResult, error) {
	strategy := params.Strategy
	if strategy == "" {
		strategy = types.StrategySecondBased
	}

	coefficient, err := calculateProrationCoefficient(
		params.PeriodStart,
		params.PeriodEnd,
		params.ProrationDate,
		time.UTC,
		strategy,
	)
	if err != nil {
		return nil, err
	}

	return &CreditGrantProrationResult{
		Coefficient:     coefficient,
		ProratedCredits: params.OriginalCredits.Mul(coefficient).Round(creditGrantCreditsScale),
		OriginalCredits: params.OriginalCredits,
		PeriodStart:     params.PeriodStart,
		PeriodEnd:       params.PeriodEnd,
		ProrationDate:   params.ProrationDate,
		Strategy:        strategy,
	}, nil
}

// AuditMetadata renders the calculation as string pairs for storage on the
// credit grant application and the resulting wallet transaction.
func (r *CreditGrantProrationResult) AuditMetadata(source string) types.Metadata {
	return types.Metadata{
		"proration_applied":          "true",
		"proration_coefficient":      r.Coefficient.String(),
		"proration_original_credits": r.OriginalCredits.String(),
		"proration_period_start":     r.PeriodStart.UTC().Format(time.RFC3339),
		"proration_period_end":       r.PeriodEnd.UTC().Format(time.RFC3339),
		"proration_date":             r.ProrationDate.UTC().Format(time.RFC3339),
		"proration_strategy":         string(r.Strategy),
		"proration_source":           source,
	}
}
