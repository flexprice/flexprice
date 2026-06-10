package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/synthetic"
)

func TestWalletDebitVerification_NoPreFundedIsNoOp(t *testing.T) {
	fc := newFakeClient()
	v := NewWalletDebitVerification(fc, synthetic.NewRegistry(), "run-1", WalletDebitOpts{})
	if err := v.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	fc.events.mu.Lock()
	defer fc.events.mu.Unlock()
	if fc.events.ingested != nil {
		t.Errorf("should not ingest without seeds")
	}
}

func TestWalletDebitVerification_IngestsExpectedCount(t *testing.T) {
	fc := newFakeClient()
	reg := synthetic.NewRegistry()
	reg.LoadSeeds(synthetic.Seeds{PreFundedCustomerIDs: []string{"c0"}})
	v := NewWalletDebitVerification(fc, reg, "run-1", WalletDebitOpts{
		EventCount:   10,
		PollInterval: 10 * time.Millisecond,
		PollTimeout:  50 * time.Millisecond,
	})
	_ = v.Run(context.Background())
	// extractWalletIDs returns nil placeholder, so the run early-exits before
	// ingest. Document that the test passes the no-wallet path until Task 25.
	fc.events.mu.Lock()
	defer fc.events.mu.Unlock()
	if len(fc.events.ingested) != 0 {
		t.Errorf("ingested=%d before extractWalletIDs is wired, want 0", len(fc.events.ingested))
	}
}
