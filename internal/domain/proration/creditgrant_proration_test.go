package proration

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalculateCreditGrantProration(t *testing.T) {
	jan1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	feb1 := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		periodStart     time.Time
		periodEnd       time.Time
		prorationDate   time.Time
		strategy        types.ProrationStrategy
		credits         decimal.Decimal
		wantCoefficient string
		wantCredits     string
	}{
		{
			name:            "mid period grants the remaining fraction",
			periodStart:     jan1,
			periodEnd:       feb1,
			prorationDate:   time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC),
			strategy:        types.StrategySecondBased,
			credits:         decimal.NewFromInt(100),
			wantCoefficient: "0.3870967741935484",
			wantCredits:     "38.70967742",
		},
		{
			// A zero-credit application fails the wallet top-up, which rolls back the
			// transaction that would have created the next period — so a stub must never
			// round down to nothing.
			name:          "a stub of a few seconds still grants something",
			periodStart:   jan1,
			periodEnd:     feb1,
			prorationDate: feb1.Add(-30 * time.Second),
			strategy:      types.StrategySecondBased,
			credits:       decimal.NewFromInt(1),
			wantCredits:   "0.0000112",
		},
		{
			name:          "exactly on period start grants everything",
			periodStart:   jan1,
			periodEnd:     feb1,
			prorationDate: jan1,
			strategy:      types.StrategySecondBased,
			credits:       decimal.NewFromInt(100),
			wantCredits:   "100",
		},
		{
			name:          "exactly on period end grants nothing",
			periodStart:   jan1,
			periodEnd:     feb1,
			prorationDate: feb1,
			strategy:      types.StrategySecondBased,
			credits:       decimal.NewFromInt(100),
			wantCredits:   "0",
		},
		{
			name:          "past period end clamps to zero rather than going negative",
			periodStart:   jan1,
			periodEnd:     feb1,
			prorationDate: time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC),
			strategy:      types.StrategySecondBased,
			credits:       decimal.NewFromInt(100),
			wantCredits:   "0",
		},
		{
			name:          "mid period halves the credits",
			periodStart:   jan1,
			periodEnd:     feb1,
			prorationDate: time.Date(2025, 1, 16, 12, 0, 0, 0, time.UTC),
			strategy:      types.StrategySecondBased,
			credits:       decimal.NewFromInt(100),
			wantCredits:   "50",
		},
		{
			name:          "defaults to second based when strategy is unset",
			periodStart:   jan1,
			periodEnd:     feb1,
			prorationDate: time.Date(2025, 1, 16, 12, 0, 0, 0, time.UTC),
			credits:       decimal.NewFromInt(100),
			wantCredits:   "50",
		},
		{
			name:          "day based at period start grants everything",
			periodStart:   time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
			periodEnd:     time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC),
			prorationDate: time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
			strategy:      types.StrategyDayBased,
			credits:       decimal.NewFromInt(100),
			wantCredits:   "100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateCreditGrantProration(CreditGrantProrationParams{
				PeriodStart:     tt.periodStart,
				PeriodEnd:       tt.periodEnd,
				ProrationDate:   tt.prorationDate,
				Strategy:        tt.strategy,
				OriginalCredits: tt.credits,
			})
			require.NoError(t, err)

			assert.Equal(t, tt.wantCredits, got.ProratedCredits.String())
			assert.Equal(t, tt.credits.String(), got.OriginalCredits.String())
			if tt.wantCoefficient != "" {
				assert.Equal(t, tt.wantCoefficient, got.Coefficient.String())
			}
		})
	}
}

func TestCalculateCreditGrantProration_InvalidPeriod(t *testing.T) {
	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	_, err := CalculateCreditGrantProration(CreditGrantProrationParams{
		PeriodStart:     start,
		PeriodEnd:       start.AddDate(0, -1, 0),
		ProrationDate:   start,
		Strategy:        types.StrategySecondBased,
		OriginalCredits: decimal.NewFromInt(100),
	})

	assert.Error(t, err)
}

// The credit stub and the invoiced price stub must cover the same window, so both
// must derive from the same coefficient.
func TestCalculateCreditGrantProration_MatchesPriceProrationCoefficient(t *testing.T) {
	periodStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	prorationDate := time.Date(2025, 1, 20, 8, 30, 0, 0, time.UTC)

	want, err := calculateProrationCoefficient(
		periodStart, periodEnd, prorationDate, time.UTC, types.StrategySecondBased,
	)
	require.NoError(t, err)

	got, err := CalculateCreditGrantProration(CreditGrantProrationParams{
		PeriodStart:     periodStart,
		PeriodEnd:       periodEnd,
		ProrationDate:   prorationDate,
		Strategy:        types.StrategySecondBased,
		OriginalCredits: decimal.NewFromInt(100),
	})
	require.NoError(t, err)

	assert.True(t, want.Equal(got.Coefficient),
		"credit grant coefficient %s must equal price proration coefficient %s",
		got.Coefficient, want)
}

func TestCreditGrantProrationResult_AuditMetadata(t *testing.T) {
	res, err := CalculateCreditGrantProration(CreditGrantProrationParams{
		PeriodStart:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:       time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
		ProrationDate:   time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC),
		Strategy:        types.StrategySecondBased,
		OriginalCredits: decimal.NewFromInt(100),
	})
	require.NoError(t, err)

	md := res.AuditMetadata("addon_attach")

	assert.Equal(t, "true", md["proration_applied"])
	assert.Equal(t, "100", md["proration_original_credits"])
	assert.Equal(t, "addon_attach", md["proration_source"])
	assert.Equal(t, string(types.StrategySecondBased), md["proration_strategy"])
	assert.Equal(t, "2025-01-01T00:00:00Z", md["proration_period_start"])
	assert.Equal(t, "2025-02-01T00:00:00Z", md["proration_period_end"])
	assert.Equal(t, "2025-01-20T00:00:00Z", md["proration_date"])
}
