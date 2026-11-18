package usagerollup

import (
	"sort"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
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

func TestSupports(t *testing.T) {
	tests := []struct {
		aggregation types.AggregationType
		bucketSize  types.WindowSize
		want        bool
	}{
		{types.AggregationSum, "", true},
		{types.AggregationCount, "", true},
		{types.AggregationMax, "", true},
		{types.AggregationMax, types.WindowSizeHour, false},
		{types.AggregationCountUnique, "", false},
		{types.AggregationLatest, "", false},
		{types.AggregationAvg, "", false},
		{types.AggregationSumWithMultiplier, "", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.aggregation)+"/"+string(tt.bucketSize), func(t *testing.T) {
			require.Equal(t, tt.want, Supports(tt.aggregation, tt.bucketSize))
		})
	}
}

func TestAggregator(t *testing.T) {
	cutoff := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	at := func(hour, minute int) time.Time { return time.Date(2026, 11, 18, hour, minute, 0, 0, time.UTC) }
	d := decimal.RequireFromString

	type row struct {
		customer    string
		ts          time.Time
		meter       string
		aggregation types.AggregationType
		bucketSize  types.WindowSize
		qty         string
	}
	tests := []struct {
		name        string
		rows        []row
		want        []Bucket
		wantCounted int
		wantTooOld  int
	}{
		{
			name: "sum adds within an hour and splits at the hour boundary",
			rows: []row{
				{"c1", at(12, 0), "m1", types.AggregationSum, "", "2.5"},
				{"c1", at(12, 59), "m1", types.AggregationSum, "", "-0.5"},
				{"c1", at(13, 0), "m1", types.AggregationSum, "", "4"},
			},
			want: []Bucket{
				{"c1", at(12, 0), "m1", types.AggregationSum, d("2")},
				{"c1", at(13, 0), "m1", types.AggregationSum, d("4")},
			},
			wantCounted: 3,
		},
		{
			name: "count adds one per row regardless of quantity",
			rows: []row{
				{"c1", at(12, 5), "m2", types.AggregationCount, "", "7"},
				{"c1", at(12, 6), "m2", types.AggregationCount, "", "0"},
			},
			want:        []Bucket{{"c1", at(12, 0), "m2", types.AggregationCount, d("2")}},
			wantCounted: 2,
		},
		{
			name: "max keeps the highest value, including when all are negative",
			rows: []row{
				{"c1", at(12, 1), "m3", types.AggregationMax, "", "-5"},
				{"c1", at(12, 2), "m3", types.AggregationMax, "", "-2"},
				{"c1", at(12, 3), "m3", types.AggregationMax, "", "-9"},
			},
			want:        []Bucket{{"c1", at(12, 0), "m3", types.AggregationMax, d("-2")}},
			wantCounted: 3,
		},
		{
			name: "customers and meters stay separate",
			rows: []row{
				{"c1", at(12, 0), "m1", types.AggregationSum, "", "1"},
				{"c2", at(12, 0), "m1", types.AggregationSum, "", "1"},
				{"c1", at(12, 0), "m4", types.AggregationSum, "", "1"},
			},
			want: []Bucket{
				{"c1", at(12, 0), "m1", types.AggregationSum, d("1")},
				{"c2", at(12, 0), "m1", types.AggregationSum, d("1")},
				{"c1", at(12, 0), "m4", types.AggregationSum, d("1")},
			},
			wantCounted: 3,
		},
		{
			name: "rows before the cutoff are dropped and counted; the cutoff hour itself is kept",
			rows: []row{
				{"c1", at(9, 59), "m1", types.AggregationSum, "", "1"},
				{"c1", at(10, 0), "m1", types.AggregationSum, "", "1"},
			},
			want:        []Bucket{{"c1", at(10, 0), "m1", types.AggregationSum, d("1")}},
			wantCounted: 1,
			wantTooOld:  1,
		},
		{
			name: "unsupported meters and rows without a customer are ignored, not counted as too old",
			rows: []row{
				{"c1", at(9, 0), "m5", types.AggregationCountUnique, "", "1"},
				{"c1", at(12, 0), "m6", types.AggregationMax, types.WindowSizeHour, "1"},
				{"", at(12, 0), "m1", types.AggregationSum, "", "1"},
			},
			want: []Bucket{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAggregator(cutoff)
			for _, r := range tt.rows {
				a.Add(r.customer, r.ts, r.meter, r.aggregation, r.bucketSize, d(r.qty))
			}
			got := a.Buckets()
			sort.Slice(got, func(i, j int) bool { return bucketOrder(got[i]) < bucketOrder(got[j]) })
			sort.Slice(tt.want, func(i, j int) bool { return bucketOrder(tt.want[i]) < bucketOrder(tt.want[j]) })
			require.Len(t, got, len(tt.want))
			for i := range tt.want {
				require.Equal(t, tt.want[i].ExternalCustomerID, got[i].ExternalCustomerID)
				require.True(t, tt.want[i].Hour.Equal(got[i].Hour))
				require.Equal(t, tt.want[i].MeterID, got[i].MeterID)
				require.Equal(t, tt.want[i].Aggregation, got[i].Aggregation)
				require.True(t, tt.want[i].Quantity.Equal(got[i].Quantity), "quantity %s, want %s", got[i].Quantity, tt.want[i].Quantity)
			}
			require.Equal(t, tt.wantCounted, a.RowCount())
			require.Equal(t, tt.wantTooOld, a.TooOld())
		})
	}
}

func TestAggregatorNilSafe(t *testing.T) {
	var a *Aggregator
	a.Add("c1", time.Now(), "m1", types.AggregationSum, "", decimal.NewFromInt(1))
	require.Empty(t, a.Buckets())
	require.Zero(t, a.RowCount())
	require.Zero(t, a.TooOld())
}

func bucketOrder(b Bucket) string {
	return b.ExternalCustomerID + "|" + b.Hour.Format(time.RFC3339) + "|" + b.MeterID
}
