package proration

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalculateEntitlementGrantProration(t *testing.T) {
	jan1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	feb1 := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		periodStart     time.Time
		periodEnd       time.Time
		prorationDate   time.Time
		strategy        types.ProrationStrategy
		quota           decimal.Decimal
		wantCoefficient string
		wantQuota       string
	}{
		{
			name:            "mid period grants the remaining fraction",
			periodStart:     jan1,
			periodEnd:       feb1,
			prorationDate:   time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC),
			strategy:        types.StrategySecondBased,
			quota:           decimal.NewFromInt(1000),
			wantCoefficient: "0.3870967741935484",
			wantQuota:       "387.0967741935484",
		},
		{
			name:          "exactly on period start grants the full quota",
			periodStart:   jan1,
			periodEnd:     feb1,
			prorationDate: jan1,
			strategy:      types.StrategySecondBased,
			quota:         decimal.NewFromInt(1000),
			wantQuota:     "1000",
		},
		{
			name:          "exactly on period end grants nothing",
			periodStart:   jan1,
			periodEnd:     feb1,
			prorationDate: feb1,
			strategy:      types.StrategySecondBased,
			quota:         decimal.NewFromInt(1000),
			wantQuota:     "0",
		},
		{
			name:          "past period end clamps to zero rather than going negative",
			periodStart:   jan1,
			periodEnd:     feb1,
			prorationDate: time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC),
			strategy:      types.StrategySecondBased,
			quota:         decimal.NewFromInt(1000),
			wantQuota:     "0",
		},
		{
			name:          "mid period halves the quota",
			periodStart:   jan1,
			periodEnd:     feb1,
			prorationDate: time.Date(2025, 1, 16, 12, 0, 0, 0, time.UTC),
			strategy:      types.StrategySecondBased,
			quota:         decimal.NewFromInt(1000),
			wantQuota:     "500",
		},
		{
			name:          "defaults to second based when strategy is unset",
			periodStart:   jan1,
			periodEnd:     feb1,
			prorationDate: time.Date(2025, 1, 16, 12, 0, 0, 0, time.UTC),
			quota:         decimal.NewFromInt(1000),
			wantQuota:     "500",
		},
		{
			name:          "day based at period start grants the full quota",
			periodStart:   time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
			periodEnd:     time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC),
			prorationDate: time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
			strategy:      types.StrategyDayBased,
			quota:         decimal.NewFromInt(1000),
			wantQuota:     "1000",
		},
		{
			name:          "fractional quota stays fractional",
			periodStart:   jan1,
			periodEnd:     feb1,
			prorationDate: time.Date(2025, 1, 16, 12, 0, 0, 0, time.UTC),
			strategy:      types.StrategySecondBased,
			quota:         decimal.NewFromFloat(2.5),
			wantQuota:     "1.25",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateEntitlementGrantProration(EntitlementGrantProrationParams{
				PeriodStart:   tt.periodStart,
				PeriodEnd:     tt.periodEnd,
				ProrationDate: tt.prorationDate,
				Strategy:      tt.strategy,
				OriginalQuota: tt.quota,
			})
			require.NoError(t, err)

			assert.Equal(t, tt.wantQuota, got.ProratedQuota.String())
			assert.Equal(t, tt.quota.String(), got.OriginalQuota.String())
			if tt.wantCoefficient != "" {
				assert.Equal(t, tt.wantCoefficient, got.Coefficient.String())
			}
		})
	}
}

// An attach in the final seconds of a period must still produce a positive
// quota rather than silently rounding to zero; phase 4 decides whether such a
// sliver is worth writing.
func TestCalculateEntitlementGrantProration_SliverStaysPositive(t *testing.T) {
	jan1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	feb1 := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	got, err := CalculateEntitlementGrantProration(EntitlementGrantProrationParams{
		PeriodStart:   jan1,
		PeriodEnd:     feb1,
		ProrationDate: feb1.Add(-30 * time.Second),
		Strategy:      types.StrategySecondBased,
		OriginalQuota: decimal.NewFromInt(1),
	})
	require.NoError(t, err)

	assert.True(t, got.ProratedQuota.GreaterThan(decimal.Zero),
		"expected a positive sliver, got %s", got.ProratedQuota)
}

func TestCalculateEntitlementGrantProration_InvalidPeriod(t *testing.T) {
	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		periodStart time.Time
		periodEnd   time.Time
		strategy    types.ProrationStrategy
	}{
		{
			name:        "negative second based period",
			periodStart: start,
			periodEnd:   start.AddDate(0, -1, 0),
			strategy:    types.StrategySecondBased,
		},
		{
			name:        "zero length second based period",
			periodStart: start,
			periodEnd:   start,
			strategy:    types.StrategySecondBased,
		},
		{
			name:        "unknown strategy",
			periodStart: start,
			periodEnd:   start.AddDate(0, 1, 0),
			strategy:    types.ProrationStrategy("fortnightly"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateEntitlementGrantProration(EntitlementGrantProrationParams{
				PeriodStart:   tt.periodStart,
				PeriodEnd:     tt.periodEnd,
				ProrationDate: tt.periodStart,
				Strategy:      tt.strategy,
				OriginalQuota: decimal.NewFromInt(1000),
			})
			assert.Error(t, err)
		})
	}
}

// The prorated quota and the addon's prorated charge must cover the same
// window, so both must derive from the same coefficient.
func TestCalculateEntitlementGrantProration_MatchesPriceProrationCoefficient(t *testing.T) {
	periodStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	prorationDate := time.Date(2025, 1, 20, 8, 30, 0, 0, time.UTC)

	want, err := calculateProrationCoefficient(
		periodStart, periodEnd, prorationDate, time.UTC, types.StrategySecondBased,
	)
	require.NoError(t, err)

	got, err := CalculateEntitlementGrantProration(EntitlementGrantProrationParams{
		PeriodStart:   periodStart,
		PeriodEnd:     periodEnd,
		ProrationDate: prorationDate,
		Strategy:      types.StrategySecondBased,
		OriginalQuota: decimal.NewFromInt(1000),
	})
	require.NoError(t, err)

	assert.True(t, want.Equal(got.Coefficient),
		"entitlement grant coefficient %s must equal price proration coefficient %s",
		got.Coefficient, want)
}

// Credits and quotas describing the same attach must scale identically.
func TestCalculateEntitlementGrantProration_MatchesCreditGrantCoefficient(t *testing.T) {
	periodStart := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	prorationDate := time.Date(2025, 6, 17, 4, 15, 0, 0, time.UTC)

	credits, err := CalculateCreditGrantProration(CreditGrantProrationParams{
		PeriodStart:     periodStart,
		PeriodEnd:       periodEnd,
		ProrationDate:   prorationDate,
		Strategy:        types.StrategySecondBased,
		OriginalCredits: decimal.NewFromInt(100),
	})
	require.NoError(t, err)

	quota, err := CalculateEntitlementGrantProration(EntitlementGrantProrationParams{
		PeriodStart:   periodStart,
		PeriodEnd:     periodEnd,
		ProrationDate: prorationDate,
		Strategy:      types.StrategySecondBased,
		OriginalQuota: decimal.NewFromInt(100),
	})
	require.NoError(t, err)

	assert.True(t, credits.Coefficient.Equal(quota.Coefficient),
		"credit coefficient %s must equal quota coefficient %s",
		credits.Coefficient, quota.Coefficient)
}

func TestEntitlementGrantProrationResult_AuditMetadata(t *testing.T) {
	res, err := CalculateEntitlementGrantProration(EntitlementGrantProrationParams{
		PeriodStart:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:     time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
		ProrationDate: time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC),
		Strategy:      types.StrategySecondBased,
		OriginalQuota: decimal.NewFromInt(1000),
	})
	require.NoError(t, err)

	md := res.AuditMetadata("addon_attach")

	assert.Equal(t, "true", md["proration_applied"])
	assert.Equal(t, "0.3870967741935484", md["proration_coefficient"])
	assert.Equal(t, "1000", md["proration_original_quota"])
	assert.Equal(t, "addon_attach", md["proration_source"])
	assert.Equal(t, string(types.StrategySecondBased), md["proration_strategy"])
	assert.Equal(t, "2025-01-01T00:00:00Z", md["proration_period_start"])
	assert.Equal(t, "2025-02-01T00:00:00Z", md["proration_period_end"])
	assert.Equal(t, "2025-01-20T00:00:00Z", md["proration_date"])
}
