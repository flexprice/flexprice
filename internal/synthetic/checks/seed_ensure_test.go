package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/synthetic"
)

func TestSeedEnsure_AllPresent(t *testing.T) {
	fc := newFakeClient()
	for _, ev := range []string{"synthetic_count", "synthetic_sum", "synthetic_avg", "synthetic_count_unique",
		"synthetic_latest", "synthetic_max", "synthetic_sum_multiplier", "synthetic_weighted_sum", "synthetic_sum_filtered"} {
		_, _ = fc.meters.Create(context.Background(), synthetic.CreateMeterRequest{EventName: ev, Name: ev})
	}
	for i := 0; i < 10; i++ {
		fc.customers.byExt[persistentExternalCustomerID(i)] = "cust_id"
	}
	reg := synthetic.NewRegistry()
	s := NewSeedEnsure(fc, reg, "run-1")
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fc.meters.created) != 9 {
		t.Errorf("meters.created (pre + by check) = %d, want 9 (no new creates)", len(fc.meters.created))
	}
	if len(fc.customers.created) != 0 {
		t.Errorf("customers.created=%d, want 0", len(fc.customers.created))
	}
	got := reg.Seeds()
	if len(got.PersistentCustomerIDs) != 10 {
		t.Errorf("PersistentCustomerIDs=%d, want 10", len(got.PersistentCustomerIDs))
	}
	if len(got.PreFundedCustomerIDs) != 3 {
		t.Errorf("PreFundedCustomerIDs=%d, want 3", len(got.PreFundedCustomerIDs))
	}
	if len(got.MeterIDs) != 9 {
		t.Errorf("MeterIDs=%d, want 9", len(got.MeterIDs))
	}
}

func TestSeedEnsure_CreatesMissing(t *testing.T) {
	fc := newFakeClient()
	fc.customers.getErr = errors.New("404")
	reg := synthetic.NewRegistry()
	s := NewSeedEnsure(fc, reg, "run-1")
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fc.meters.created) != 9 {
		t.Errorf("meters.created=%d, want 9", len(fc.meters.created))
	}
	if len(fc.customers.created) != 10 {
		t.Errorf("customers.created=%d, want 10", len(fc.customers.created))
	}
}
