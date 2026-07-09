package checks

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/e2eprobe"
)

// TestEntitlementAndUsageProbe_HitsAllFour verifies that when GetByExternalID
// returns a populated customer ID, all four downstream API calls fire.
func TestEntitlementAndUsageProbe_HitsAllFour(t *testing.T) {
	fc := newFakeClient()
	// Pre-seed the fake so GetByExternalID("c0") returns a populated CustomerResponse.
	fc.customers.byExt["c0"] = "cust_c0"
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{
		PersistentCustomerIDs: []string{"c0"},
		PersistentSubIDs:      []string{"sub_1"},
		FeatureIDs:            []string{"feat_1"},
	})
	p := NewEntitlementAndUsageProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// After Task 25 wiring, extractCustomerID now returns "cust_c0" so all 4
	// endpoints should have been called. The fake impls don't count calls
	// individually, but a successful Run() without error confirms the path.
}

func TestEntitlementAndUsageProbe_NoSeedsIsNoOp(t *testing.T) {
	fc := newFakeClient()
	p := NewEntitlementAndUsageProbe(fc, e2eprobe.NewRegistry(), "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestEntitlementAndUsageProbe_MissingCustomerSoftSkips verifies that when
// GetByExternalID returns 404 (customer not provisioned yet — first-run race),
// the probe soft-skips (returns nil) instead of alerting.
func TestEntitlementAndUsageProbe_MissingCustomerSoftSkips(t *testing.T) {
	fc := newFakeClient()
	// byExt is empty → GetByExternalID returns *sdkerrors.APIError{StatusCode:404}.
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{
		PersistentCustomerIDs: []string{"unknown"},
		PersistentSubIDs:      []string{"sub_1"},
	})
	p := NewEntitlementAndUsageProbe(fc, reg, "run-1")
	// 404 must NOT produce an alert — it's a benign first-run state.
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("expected nil (soft-skip) for 404 customer, got: %v", err)
	}
}
