package service

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// A NEVER allowance is spent once over the subscription. Walking a 2 GB allowance
// through consecutive periods: the free amount is consumed in the period that crosses
// it and never offered again.
func TestCumulativeOverage(t *testing.T) {
	d := decimal.NewFromInt
	allowed := d(2)

	tests := []struct {
		name  string
		prev  decimal.Decimal // cumulative usage to the start of this period
		total decimal.Decimal // cumulative usage to the end of this period
		want  decimal.Decimal
	}{
		{"first period, still under the allowance", d(0), d(1), d(0)},
		{"first period, crosses the allowance", d(0), d(3), d(1)},
		{"allowance already spent, bills the whole period", d(3), d(5), d(2)},
		{"allowance already spent, quiet period", d(5), d(5), d(0)},
		{"lands exactly on the allowance", d(0), d(2), d(0)},
		{"crosses by a fraction", decimal.NewFromFloat(1.5), d(3), d(1)},
		// A signed-delta meter can fall back under the allowance; the customer is not
		// credited for a period they spent below it.
		{"usage falls back below the allowance", d(5), d(1), d(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cumulativeOverage(tt.total, tt.prev, allowed)
			assert.True(t, got.Equal(tt.want), "want %s, got %s", tt.want, got)
		})
	}
}

// The allowance must not refresh: six periods of 1.5 GB against a 2 GB allowance bill
// 7 GB in total, not zero.
func TestCumulativeOverage_DoesNotRefreshEachPeriod(t *testing.T) {
	allowed := decimal.NewFromInt(2)
	perPeriod := decimal.NewFromFloat(1.5)

	cumulative := decimal.Zero
	billed := decimal.Zero
	for i := 0; i < 6; i++ {
		prev := cumulative
		cumulative = cumulative.Add(perPeriod)
		billed = billed.Add(cumulativeOverage(cumulative, prev, allowed))
	}

	assert.True(t, cumulative.Equal(decimal.NewFromInt(9)), "cumulative usage")
	assert.True(t, billed.Equal(decimal.NewFromInt(7)), "billed total, want 7 got %s", billed)
}
