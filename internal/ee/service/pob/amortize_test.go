package pob

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// billedLine is one amount over its service period, as an invoice line carries it.
type billedLine struct {
	amount     string
	start, end time.Time
}

func earnedBy(lines []billedLine, recognition types.RecognitionMethod, amortization types.AmortizationMethod, asOf time.Time, loc *time.Location) decimal.Decimal {
	total := decimal.Zero
	for _, l := range lines {
		total = total.Add(Earned(decimal.RequireFromString(l.amount), l.start, l.end, recognition, amortization, asOf, loc))
	}
	return total
}

// monthAmounts rounds the cumulative earned at each month boundary, so the months sum exactly to the total.
func monthAmounts(lines []billedLine, recognition types.RecognitionMethod, amortization types.AmortizationMethod, from, to time.Time) []string {
	var out []string
	for m := from; m.Before(to); m = m.AddDate(0, 1, 0) {
		byEnd := types.RoundToCurrencyPrecision(earnedBy(lines, recognition, amortization, m.AddDate(0, 1, 0), time.UTC), "usd")
		byStart := types.RoundToCurrencyPrecision(earnedBy(lines, recognition, amortization, m, time.UTC), "usd")
		out = append(out, byEnd.Sub(byStart).StringFixed(2))
	}
	return out
}

func TestEarned_ERDExamples(t *testing.T) {
	lines := []billedLine{{"300", date(2026, 1, 15), date(2026, 4, 15)}}

	tests := []struct {
		name         string
		amortization types.AmortizationMethod
		want         []string
	}{
		// The ERD prints Feb as 99.50; 99.51 is exact and makes the months sum to 300.
		{"period_even_prorated", types.AmortizationPeriodEvenProrated, []string{"54.56", "99.51", "99.50", "46.43"}},
		{"period_even", types.AmortizationPeriodEven, []string{"75.00", "75.00", "75.00", "75.00"}},
		{"daily", types.AmortizationDaily, []string{"56.67", "93.33", "103.33", "46.67"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, monthAmounts(lines, types.RecognitionMethodStraightLine, tt.amortization, date(2026, 1, 1), date(2026, 5, 1)))
		})
	}

	t.Run("prorated on Mar 10", func(t *testing.T) {
		earned := earnedBy(lines, types.RecognitionMethodStraightLine, types.AmortizationPeriodEvenProrated, date(2026, 3, 10), time.UTC)
		assert.Equal(t, "182.95", earned.StringFixed(2))
	})

	t.Run("daily is 3.33 a day", func(t *testing.T) {
		before := earnedBy(lines, types.RecognitionMethodStraightLine, types.AmortizationDaily, date(2026, 2, 10), time.UTC)
		after := earnedBy(lines, types.RecognitionMethodStraightLine, types.AmortizationDaily, date(2026, 2, 11), time.UTC)
		assert.Equal(t, "3.33", after.Sub(before).StringFixed(2))
	})
}

func TestEarned_PointInTime(t *testing.T) {
	lines := []billedLine{{"120", date(2026, 2, 10), date(2026, 2, 10)}}
	method := types.RecognitionMethodPointInTime

	assert.True(t, earnedBy(lines, method, types.AmortizationPeriodEvenProrated, date(2026, 2, 10), time.UTC).IsZero())
	assert.Equal(t, "120.00", earnedBy(lines, method, types.AmortizationPeriodEvenProrated, date(2026, 2, 11), time.UTC).StringFixed(2))
	assert.Equal(t, []string{"0.00", "120.00", "0.00"}, monthAmounts(lines, method, types.AmortizationPeriodEvenProrated, date(2026, 1, 1), date(2026, 4, 1)))
}

// Stripe's example: 90 for April, upgraded to 120 on Apr 21, earns 60 + 40 = 100 in April.
func TestEarned_ProrationLinesAdd(t *testing.T) {
	lines := []billedLine{
		{"90", date(2026, 4, 1), date(2026, 5, 1)},
		{"-30", date(2026, 4, 21), date(2026, 5, 1)}, // credit for the unused 10 days at 90
		{"40", date(2026, 4, 21), date(2026, 5, 1)},  // charge for 10 days at 120
	}
	method, amortization := types.RecognitionMethodStraightLine, types.AmortizationDaily

	assert.Equal(t, "60.00", earnedBy(lines, method, amortization, date(2026, 4, 21), time.UTC).StringFixed(2))
	assert.Equal(t, "100.00", earnedBy(lines, method, amortization, date(2026, 5, 1), time.UTC).StringFixed(2))
}

func TestEarned_PeriodsSumToTotal(t *testing.T) {
	lines := []billedLine{{"1000.01", date(2026, 1, 7), date(2027, 1, 23)}}
	for _, amortization := range []types.AmortizationMethod{types.AmortizationDaily, types.AmortizationPeriodEven, types.AmortizationPeriodEvenProrated} {
		t.Run(string(amortization), func(t *testing.T) {
			sum := decimal.Zero
			for _, a := range monthAmounts(lines, types.RecognitionMethodStraightLine, amortization, date(2026, 1, 1), date(2027, 2, 1)) {
				sum = sum.Add(decimal.RequireFromString(a))
			}
			assert.Equal(t, "1000.01", sum.StringFixed(2))
		})
	}
}

func TestEarned_LocalDays(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	// Midnight local is 18:30 UTC the previous day; days must be counted in local time.
	start := time.Date(2026, 2, 1, 0, 0, 0, 0, loc)
	lines := []billedLine{{"280", start, start.AddDate(0, 0, 28)}}

	earned := earnedBy(lines, types.RecognitionMethodStraightLine, types.AmortizationDaily, time.Date(2026, 2, 2, 0, 0, 0, 0, loc), loc)
	assert.Equal(t, "10.00", earned.StringFixed(2))
}
