package clickhouse

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestNormalizeUsageValue(t *testing.T) {
	for _, tc := range []struct {
		name        string
		value       float64
		aggregation types.AggregationType
		want        string
	}{
		{"signed negative", -20, types.AggregationSum, "-20"},
		{"max unchanged", -20, types.AggregationMax, "0"},
		{"avg unchanged", -20, types.AggregationAvg, "0"},
		{"latest unchanged", -20, types.AggregationLatest, "0"},
		{"multiplier unchanged", -20, types.AggregationSumWithMultiplier, "0"},
		{"weighted unchanged", -20, types.AggregationWeightedSum, "0"},
		{"positive", 70, types.AggregationSum, "70"},
		{"positive max", 70, types.AggregationMax, "70"},
		{"zero", 0, types.AggregationSum, "0"},
		{"fraction", -0.125, types.AggregationSum, "-0.125"},
		{"nan", math.NaN(), types.AggregationSum, "0"},
		{"infinity", math.Inf(-1), types.AggregationSum, "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeUsageValue(safeDecimalFromFloat(tc.value), tc.aggregation)
			require.True(t, got.Equal(decimal.RequireFromString(tc.want)), "want %s, got %s", tc.want, got)
		})
	}
}

type signedBucketRow struct {
	total, value float64
	at           time.Time
	group        string
	err          error
}

func (r signedBucketRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*float64) = r.total
	*dest[1].(*time.Time) = r.at
	*dest[2].(*float64) = r.value
	if len(dest) == 4 {
		*dest[3].(*string) = r.group
	}
	return nil
}

func TestScanBucketedRowSignedSum(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, aggregation := range []types.AggregationType{types.AggregationSum, types.AggregationMax} {
		for _, grouped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/grouped=%t", aggregation, grouped), func(t *testing.T) {
				total, value, gotAt, group, err := scanBucketedRow(signedBucketRow{total: -20, value: -70, at: at, group: "a"}, grouped, aggregation)
				require.NoError(t, err)
				wantTotal, wantValue := decimal.Zero, decimal.Zero
				if aggregation == types.AggregationSum {
					wantTotal, wantValue = decimal.NewFromInt(-20), decimal.NewFromInt(-70)
				}
				require.True(t, total.Equal(wantTotal))
				require.True(t, value.Equal(wantValue))
				require.Equal(t, at, gotAt)
				if grouped {
					require.Equal(t, "a", group)
				} else {
					require.Empty(t, group)
				}
			})
		}
	}
	t.Run("scan error", func(t *testing.T) {
		_, _, _, _, err := scanBucketedRow(signedBucketRow{err: fmt.Errorf("scan failed")}, false, types.AggregationSum)
		require.Error(t, err)
	})
}
