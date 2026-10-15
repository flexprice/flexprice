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
		{AggregationSignedSum, false},

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

func TestAggregationType_SignedSum(t *testing.T) {
	if !AggregationSignedSum.Validate() {
		t.Fatal("SIGNED_SUM should be a valid aggregation type")
	}
	if !AggregationSignedSum.RequiresField() {
		t.Fatal("SIGNED_SUM should require a field")
	}

	cases := []struct {
		typ  AggregationType
		want bool
	}{
		{AggregationSignedSum, true},
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
