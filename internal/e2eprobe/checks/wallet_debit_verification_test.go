package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestWalletDebitVerification_NoPreFundedIsNoOp(t *testing.T) {
	fc := newFakeClient()
	v := NewWalletDebitVerification(fc, e2eprobe.NewRegistry(), "run-1", WalletDebitOpts{})
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
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PreFundedCustomerIDs: []string{"c0"}})
	v := NewWalletDebitVerification(fc, reg, "run-1", WalletDebitOpts{
		EventCount:   10,
		PollInterval: 10 * time.Millisecond,
		PollTimeout:  50 * time.Millisecond,
	})
	_ = v.Run(context.Background())
	// extractWalletIDs returns nil when no wallet items, so the run early-exits
	// before ingest.
	fc.events.mu.Lock()
	defer fc.events.mu.Unlock()
	if len(fc.events.ingested) != 0 {
		t.Errorf("ingested=%d with empty wallet list, want 0", len(fc.events.ingested))
	}
}

// TestWalletDebitVerification_IngestsExpectedCount_WithWallet verifies the positive
// path: when wallets are present, extractWalletIDs returns IDs and events are ingested.
// The debit poll times out quickly because the fake balance is static.
func TestWalletDebitVerification_IngestsExpectedCount_WithWallet(t *testing.T) {
	fc := newFakeClient()
	walletID := "wallet_002"
	fc.wallets.walletItems = []types.DtoWalletResponse{{ID: &walletID}}
	// Set a large starting balance so top-up is skipped.
	fc.wallets.balance = "9999.00"

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PreFundedCustomerIDs: []string{"c0"}})
	v := NewWalletDebitVerification(fc, reg, "run-1", WalletDebitOpts{
		EventCount:   10,
		EventAmount:  "0.01",
		PollInterval: 10 * time.Millisecond,
		PollTimeout:  50 * time.Millisecond,
	})
	// Run will ingest 10 events then time out waiting for balance drop (fake
	// balance is static), returning a non-nil error. That's fine — we assert
	// event count below.
	_ = v.Run(context.Background())

	fc.events.mu.Lock()
	defer fc.events.mu.Unlock()
	if len(fc.events.ingested) != 10 {
		t.Errorf("expected 10 ingested events, got %d", len(fc.events.ingested))
	}
}
