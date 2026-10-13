package proration

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CalculateProrationCoefficient is the share of one full billing period that used covers, at most 1, along with
// that full period. Seconds by default; StrategyDayBased counts local calendar days instead.
// Every charge, credit and grant proration goes through it.
// Period and count here can differ for smaller cadence items running inside the sub's cadence.
// e.g. calendar monthly sub, used [Jan 15, Feb 1): full [Jan 1, Feb 1), coefficient 17/31.
func CalculateProrationCoefficient(
	sub *subscription.Subscription,
	period types.BillingPeriod,
	count int,
	used types.Period,
	strategy types.ProrationStrategy,
) (decimal.Decimal, types.Period, error) {
	grid, err := types.NewBillingPeriodGrid(sub.BillingAnchor, period, max(count, 1), sub.Timezone)
	if err != nil {
		return decimal.Zero, types.Period{}, err
	}
	full := types.FullBillingPeriod(used.Start, grid)

	var usedUnits, fullUnits int64
	if strategy == types.StrategyDayBased {
		loc, err := time.LoadLocation(types.ResolveTimezone(sub.Timezone))
		if err != nil {
			loc = time.UTC
		}
		usedUnits = int64(daysInDurationWithDST(used.Start.In(loc), used.End.In(loc), loc))
		fullUnits = int64(daysInDurationWithDST(full.Start.In(loc), full.End.In(loc), loc))
	} else {
		usedUnits = int64(used.End.Sub(used.Start).Round(time.Second) / time.Second)
		fullUnits = int64(full.End.Sub(full.Start).Round(time.Second) / time.Second)
	}

	if usedUnits <= 0 {
		return decimal.Zero, full, nil
	}
	if usedUnits >= fullUnits {
		return decimal.NewFromInt(1), full, nil
	}

	return decimal.NewFromInt(usedUnits).Div(decimal.NewFromInt(fullUnits)), full, nil
}

type AuditParams struct {
	Source        string
	Coefficient   decimal.Decimal
	OriginalKey   string
	OriginalValue decimal.Decimal
	PeriodStart   time.Time
	PeriodEnd     time.Time
	ProrationDate time.Time
	Strategy      types.ProrationStrategy
}

// AuditMetadata renders the calculation for storage. The coefficient alone says
// whether proration applied — a value of 1 means it did not.
func AuditMetadata(p AuditParams) types.Metadata {
	strategy := p.Strategy
	if strategy == "" {
		strategy = types.StrategySecondBased
	}
	m := types.Metadata{
		"proration_coefficient":  p.Coefficient.String(),
		p.OriginalKey:            p.OriginalValue.String(),
		"proration_period_start": p.PeriodStart.UTC().Format(time.RFC3339),
		"proration_period_end":   p.PeriodEnd.UTC().Format(time.RFC3339),
		"proration_date":         p.ProrationDate.UTC().Format(time.RFC3339),
		"proration_strategy":     string(strategy),
	}
	if p.Source != "" {
		m["proration_source"] = p.Source
	}
	return m
}
