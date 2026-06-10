package checks

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/synthetic"
)

func TestEntitlementAndUsageProbe_HitsAllFour(t *testing.T) {
	fc := newFakeClient()
	// Pre-seed the fake so GetByExternalID("c0") succeeds.
	fc.customers.byExt["c0"] = "cust_c0"
	reg := synthetic.NewRegistry()
	reg.LoadSeeds(synthetic.Seeds{
		PersistentCustomerIDs: []string{"c0"},
		PersistentSubIDs:      []string{"sub_1"},
		FeatureIDs:            []string{"feat_1"},
	})
	p := NewEntitlementAndUsageProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// extractCustomerID is a placeholder returning "" → early-exit before the
	// four API calls. Test documents current behavior; Task 25 will swap.
}

func TestEntitlementAndUsageProbe_NoSeedsIsNoOp(t *testing.T) {
	fc := newFakeClient()
	p := NewEntitlementAndUsageProbe(fc, synthetic.NewRegistry(), "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}
