package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/e2eprobe"
)

func TestSeedEnsure_AllPresent(t *testing.T) {
	fc := newFakeClient()
	for _, ev := range []string{"e2eprobe_count", "e2eprobe_sum", "e2eprobe_avg", "e2eprobe_count_unique",
		"e2eprobe_latest", "e2eprobe_max", "e2eprobe_sum_multiplier", "e2eprobe_weighted_sum", "e2eprobe_sum_filtered"} {
		_, _ = fc.meters.Create(context.Background(), e2eprobe.CreateMeterRequest{EventName: ev, Name: ev})
	}
	for i := 0; i < 10; i++ {
		fc.customers.byExt[persistentExternalCustomerID(i)] = "cust_id"
	}
	reg := e2eprobe.NewRegistry()
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
	reg := e2eprobe.NewRegistry()
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
