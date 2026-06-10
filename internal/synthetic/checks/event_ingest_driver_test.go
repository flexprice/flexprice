package checks

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/synthetic"
)

func TestEventIngestDriver_EnqueuesOnePerRun(t *testing.T) {
	fc := newFakeClient()
	reg := synthetic.NewRegistry()
	reg.LoadSeeds(synthetic.Seeds{
		PersistentCustomerIDs: []string{"c0", "c1"},
		MeterIDs:              map[string]string{"synthetic_count": "meter_1"},
	})
	d := NewEventIngestDriver(fc, reg, 42, "run-1")
	for i := 0; i < 5; i++ {
		if err := d.Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	fc.async.mu.Lock()
	defer fc.async.mu.Unlock()
	if fc.async.queued != 5 {
		t.Errorf("queued=%d, want 5", fc.async.queued)
	}
}

func TestEventIngestDriver_NoSeedsIsNoOp(t *testing.T) {
	fc := newFakeClient()
	d := NewEventIngestDriver(fc, synthetic.NewRegistry(), 1, "run-1")
	if err := d.Run(context.Background()); err != nil {
		t.Errorf("Run: %v", err)
	}
	if fc.async.queued != 0 {
		t.Errorf("expected 0 enqueues without seeds, got %d", fc.async.queued)
	}
}
