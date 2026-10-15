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
		{AggregationNetSum, false},

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

func TestAggregationType_NetSum(t *testing.T) {
	if !AggregationNetSum.Validate() {
		t.Fatal("NET_SUM should be a valid aggregation type")
	}
	if !AggregationNetSum.RequiresField() {
		t.Fatal("NET_SUM should require a field")
	}

	cases := []struct {
		typ  AggregationType
		want bool
	}{
		{AggregationNetSum, true},
		{AggregationSum, false},
		{AggregationSumWithMultiplier, false},
		{AggregationWeightedSum, false},
		{AggregationMax, false},
		{AggregationLatest, false},
		{AggregationCount, false},
	}
	for _, c := range cases {
		t.Run(string(c.typ), func(t *testing.T) {
			if got := c.typ.KeepsNegatives(); got != c.want {
				t.Fatalf("KeepsNegatives for %s = %v, want %v", c.typ, got, c.want)
			}
		})
	}
}
