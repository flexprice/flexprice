package proration

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// entitlementGrantQuotaScale matches the numeric(25,15) precision of
// entitlement_grants.quota.
const entitlementGrantQuotaScale = 15

// EntitlementGrantProrationParams describes the window an entitlement grant's
// quota covers relative to the full billing period it sits in.
type EntitlementGrantProrationParams struct {
	PeriodStart time.Time
	PeriodEnd   time.Time

	ProrationDate time.Time

	Strategy      types.ProrationStrategy
	OriginalQuota decimal.Decimal
}

type EntitlementGrantProrationResult struct {
	Coefficient   decimal.Decimal
	ProratedQuota decimal.Decimal
	OriginalQuota decimal.Decimal

	PeriodStart   time.Time
	PeriodEnd     time.Time
	ProrationDate time.Time
	Strategy      types.ProrationStrategy
}

func CalculateEntitlementGrantProration(params EntitlementGrantProrationParams) (*EntitlementGrantProrationResult, error) {
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

	return &EntitlementGrantProrationResult{
		Coefficient:   coefficient,
		ProratedQuota: params.OriginalQuota.Mul(coefficient).Round(entitlementGrantQuotaScale),
		OriginalQuota: params.OriginalQuota,
		PeriodStart:   params.PeriodStart,
		PeriodEnd:     params.PeriodEnd,
		ProrationDate: params.ProrationDate,
		Strategy:      strategy,
	}, nil
}

func (r *EntitlementGrantProrationResult) AuditMetadata(source string) types.Metadata {
	return types.Metadata{
		"proration_applied":        "true",
		"proration_coefficient":    r.Coefficient.String(),
		"proration_original_quota": r.OriginalQuota.String(),
		"proration_period_start":   r.PeriodStart.UTC().Format(time.RFC3339),
		"proration_period_end":     r.PeriodEnd.UTC().Format(time.RFC3339),
		"proration_date":           r.ProrationDate.UTC().Format(time.RFC3339),
		"proration_strategy":       string(r.Strategy),
		"proration_source":         source,
	}
}
