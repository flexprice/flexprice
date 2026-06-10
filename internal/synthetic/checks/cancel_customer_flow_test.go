package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/synthetic"
)

func TestCancelCustomerFlow_CancelsOldest(t *testing.T) {
	fc := newFakeClient()
	reg := synthetic.NewRegistry()
	old := time.Now().Add(-time.Hour)
	mid := time.Now().Add(-30 * time.Minute)
	reg.RegisterEphemeral("subscription", "sub_old", old)
	reg.RegisterEphemeral("subscription", "sub_mid", mid)
	s := NewCancelCustomerFlow(fc, reg, "run-1", InvoicePoll{Timeout: 30 * time.Millisecond, Interval: 5 * time.Millisecond})
	_ = s.Run(context.Background())
	if len(fc.subs.cancelled) != 1 || fc.subs.cancelled[0] != "sub_old" {
		t.Errorf("cancelled=%+v", fc.subs.cancelled)
	}
	if fc.invoices.queries == 0 {
		t.Errorf("invoice query never fired")
	}
}

func TestCancelCustomerFlow_NoEphemeralsIsNoOp(t *testing.T) {
	fc := newFakeClient()
	s := NewCancelCustomerFlow(fc, synthetic.NewRegistry(), "run-1", InvoicePoll{})
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fc.subs.cancelled) != 0 {
		t.Errorf("expected 0 cancels")
	}
}
