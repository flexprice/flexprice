package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/e2eprobe"
)

func TestAnalyticsProbe_Happy(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PersistentCustomerIDs: []string{"c0"}})
	p := NewAnalyticsProbe(fc, reg, "run-1", nil)
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
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PersistentCustomerIDs: []string{"c"}})
	p := NewAnalyticsProbe(fc, reg, "run-1", nil)
	if err := p.Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestAnalyticsProbe_NoSeedsIsNoOp(t *testing.T) {
	fc := newFakeClient()
	p := NewAnalyticsProbe(fc, e2eprobe.NewRegistry(), "run-1", nil)
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fc.events.analytics != 0 {
		t.Errorf("expected 0 calls without seeds, got %d", fc.events.analytics)
	}
}
