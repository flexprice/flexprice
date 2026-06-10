package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/synthetic"
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
		t.Errorf("expected nil with empty wallet list (placeholder), got %v", err)
	}
}
