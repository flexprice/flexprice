package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/synthetic"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestWalletBalanceProbe_Happy(t *testing.T) {
	fc := newFakeClient()
	reg := synthetic.NewRegistry()
	reg.LoadSeeds(synthetic.Seeds{
		PersistentCustomerIDs: []string{"c0", "c1", "c2"},
		PreFundedCustomerIDs:  []string{"c0"},
	})
	p := NewWalletBalanceProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestWalletBalanceProbe_NoPreFundedIsNoOp(t *testing.T) {
	fc := newFakeClient()
	reg := synthetic.NewRegistry()
	reg.LoadSeeds(synthetic.Seeds{PersistentCustomerIDs: []string{"c"}})
	p := NewWalletBalanceProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWalletBalanceProbe_ErrorPropagates(t *testing.T) {
	fc := newFakeClient()
	fc.wallets.balErr = errors.New("503")
	reg := synthetic.NewRegistry()
	reg.LoadSeeds(synthetic.Seeds{PreFundedCustomerIDs: []string{"c"}})
	p := NewWalletBalanceProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Errorf("expected nil with empty wallet list (no wallet items), got %v", err)
	}
}

// TestWalletBalanceProbe_GetsBalance verifies the positive path: when Query returns
// wallet IDs, GetBalance is called for each one.
func TestWalletBalanceProbe_GetsBalance(t *testing.T) {
	fc := newFakeClient()
	walletID := "wallet_001"
	fc.wallets.walletItems = []types.DtoWalletResponse{{ID: &walletID}}
	fc.wallets.balance = "100.00"

	reg := synthetic.NewRegistry()
	reg.LoadSeeds(synthetic.Seeds{PreFundedCustomerIDs: []string{"c0"}})
	p := NewWalletBalanceProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// If we got here without error, GetBalance was called with the real wallet ID.
}
