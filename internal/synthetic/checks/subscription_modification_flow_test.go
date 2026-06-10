package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/synthetic"
)

func TestSubscriptionModificationFlow_NoEphemeralsIsNoOp(t *testing.T) {
	fc := newFakeClient()
	s := NewSubscriptionModificationFlow(fc, synthetic.NewRegistry(), "run-1")
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionModificationFlow_AddsLineItem(t *testing.T) {
	fc := newFakeClient()
	reg := synthetic.NewRegistry()
	reg.RegisterEphemeral("subscription", "sub_a", time.Now().Add(-10*time.Minute))
	s := NewSubscriptionModificationFlow(fc, reg, "run-1")
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}
