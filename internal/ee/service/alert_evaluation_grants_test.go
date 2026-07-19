package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Phase 1 only fires on exhaustion. The ratio→state helper collapses to
// "in_alarm at or above 1.0, ok otherwise". Guards against a regression that
// re-adds intermediate thresholds without an ADR-worthy discussion.
func TestEntitlementGrantStateFromRatio(t *testing.T) {
	dec := func(f float64) decimal.Decimal { return decimal.NewFromFloat(f) }
	cases := []struct {
		ratio decimal.Decimal
		want  types.AlertState
	}{
		{dec(0), types.AlertStateOk},
		{dec(0.5), types.AlertStateOk},
		{dec(0.99), types.AlertStateOk},
		{dec(1), types.AlertStateInAlarm},
		{dec(1.5), types.AlertStateInAlarm},
	}
	for _, tc := range cases {
		if got := entitlementGrantStateFromRatio(tc.ratio); got != tc.want {
			t.Fatalf("ratio %s → %q; want %q", tc.ratio, got, tc.want)
		}
	}
}
