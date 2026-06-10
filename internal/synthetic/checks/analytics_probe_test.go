package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/synthetic"
)

func TestAnalyticsProbe_Happy(t *testing.T) {
	fc := newFakeClient()
	reg := synthetic.NewRegistry()
	reg.LoadSeeds(synthetic.Seeds{PersistentCustomerIDs: []string{"c0"}})
	p := NewAnalyticsProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.events.analytics != 1 {
		t.Errorf("analytics calls=%d, want 1", fc.events.analytics)
	}
}

func TestAnalyticsProbe_PropagatesError(t *testing.T) {
	fc := newFakeClient()
	fc.events.anaErr = errors.New("503")
	reg := synthetic.NewRegistry()
	reg.LoadSeeds(synthetic.Seeds{PersistentCustomerIDs: []string{"c"}})
	p := NewAnalyticsProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestAnalyticsProbe_NoSeedsIsNoOp(t *testing.T) {
	fc := newFakeClient()
	p := NewAnalyticsProbe(fc, synthetic.NewRegistry(), "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fc.events.analytics != 0 {
		t.Errorf("expected 0 calls without seeds, got %d", fc.events.analytics)
	}
}
