package aggregatedusage

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCutoff(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"just after the grace moves the cutoff", time.Date(2026, 11, 18, 13, 10, 0, 0, time.UTC), time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)},
		{"inside the grace keeps the previous hour", time.Date(2026, 11, 18, 13, 9, 59, 0, time.UTC), time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)},
		{"non-UTC input is normalised", time.Date(2026, 11, 18, 18, 40, 0, 0, time.FixedZone("IST", 5*3600+1800)), time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.True(t, tt.want.Equal(Cutoff(tt.now)), "got %s", Cutoff(tt.now))
		})
	}
}

func TestSupportsAggregation(t *testing.T) {
	tests := []struct {
		aggregation types.AggregationType
		want        bool
	}{
		{types.AggregationSum, true},
		{types.AggregationCount, true},
		{types.AggregationMax, true},
		{types.AggregationCountUnique, false},
		{types.AggregationLatest, false},
		{types.AggregationAvg, false},
		{types.AggregationSumWithMultiplier, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.aggregation), func(t *testing.T) {
			require.Equal(t, tt.want, SupportsAggregation(tt.aggregation))
		})
	}
}
