package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// -----------------------------------------------------------------------------
// Ratio → AlertState mapping is the heart of the grant alert path. Verify each
// threshold boundary explicitly so a future thresholds tweak (see ERD §14
// open question 4) breaks a test rather than silently regressing a customer's
// alert delivery.
// -----------------------------------------------------------------------------

func TestEntitlementGrantStateFromRatio(t *testing.T) {
	dec := func(f float64) decimal.Decimal { return decimal.NewFromFloat(f) }
	cases := []struct {
		ratio decimal.Decimal
		want  types.AlertState
	}{
		// Below the info threshold (50%) → healthy.
		{dec(0), types.AlertStateOk},
		{dec(0.49), types.AlertStateOk},
		// Info kicks in at exactly 50%.
		{dec(0.5), types.AlertStateInfo},
		{dec(0.79), types.AlertStateInfo},
		// Warning at 80%.
		{dec(0.8), types.AlertStateWarning},
		{dec(0.99), types.AlertStateWarning},
		// In_alarm at 100% (exhaustion) — and any ratio above.
		{dec(1), types.AlertStateInAlarm},
		{dec(1.5), types.AlertStateInAlarm},
	}
	for _, tc := range cases {
		if got := entitlementGrantStateFromRatio(tc.ratio); got != tc.want {
			t.Fatalf("ratio %s → %q; want %q", tc.ratio, got, tc.want)
		}
	}
}

func TestEntitlementGrantThresholdPct(t *testing.T) {
	cases := []struct {
		usage, quota int64
		wantPct      int
		wantOK       bool
	}{
		// Under 50% → nothing crossed.
		{10, 100, 0, false},
		{49, 100, 0, false},
		// 50% info threshold.
		{50, 100, 50, true},
		{79, 100, 50, true},
		// 80% warning threshold.
		{80, 100, 80, true},
		{99, 100, 80, true},
		// 100% exhaustion — includes over-quota.
		{100, 100, 100, true},
		{250, 100, 100, true},
	}
	for _, tc := range cases {
		usage := decimal.NewFromInt(tc.usage)
		quota := decimal.NewFromInt(tc.quota)
		pct, ok := entitlementGrantThresholdPct(usage, quota)
		if ok != tc.wantOK {
			t.Fatalf("usage=%d quota=%d wantOK=%v got ok=%v (pct=%d)", tc.usage, tc.quota, tc.wantOK, ok, pct)
		}
		if pct != tc.wantPct {
			t.Fatalf("usage=%d quota=%d wantPct=%d got=%d", tc.usage, tc.quota, tc.wantPct, pct)
		}
	}
}

func TestEntitlementGrantThresholdPct_ZeroQuotaIsDefensiveNoOp(t *testing.T) {
	// Zero-quota grants should be impossible per validation, but the helper
	// must not divide by zero if one somehow appears.
	pct, ok := entitlementGrantThresholdPct(decimal.NewFromInt(10), decimal.Zero)
	if ok {
		t.Fatalf("expected ok=false for zero quota")
	}
	if pct != 0 {
		t.Fatalf("expected pct=0, got %d", pct)
	}
}
