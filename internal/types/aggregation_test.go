package types

import "testing"

func TestAggregationType_SupportsExpression(t *testing.T) {
	cases := []struct {
		typ  AggregationType
		want bool
	}{
		{AggregationSum, true},
		{AggregationAvg, true},
		{AggregationMax, true},
		{AggregationLatest, true},

		{AggregationCount, false},
		{AggregationCountUnique, false},
		{AggregationSumWithMultiplier, false},
		{AggregationWeightedSum, false},

		// Unknown aggregation defaults to false.
		{AggregationType("BOGUS"), false},
	}
	for _, c := range cases {
		t.Run(string(c.typ), func(t *testing.T) {
			if got := c.typ.SupportsExpression(); got != c.want {
				t.Fatalf("SupportsExpression for %s = %v, want %v", c.typ, got, c.want)
			}
		})
	}
}
