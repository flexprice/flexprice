package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestCancelCustomerFlow_CancelsOldest(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
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
	s := NewCancelCustomerFlow(fc, e2eprobe.NewRegistry(), "run-1", InvoicePoll{})
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fc.subs.cancelled) != 0 {
		t.Errorf("expected 0 cancels")
	}
}

// TestCancelCustomerFlow_FindsInvoice verifies the positive path: once an invoice
// exists for the subscription, pollInvoice returns nil immediately.
func TestCancelCustomerFlow_FindsInvoice(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.RegisterEphemeral("subscription", "sub_a", time.Now().Add(-time.Hour))

	periodEnd := time.Now().Format(time.RFC3339)
	fc.invoices.invoices = []types.DtoInvoiceResponse{
		{PeriodEnd: &periodEnd},
	}

	s := NewCancelCustomerFlow(fc, reg, "run-1", InvoicePoll{Timeout: 200 * time.Millisecond, Interval: 10 * time.Millisecond})
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fc.subs.cancelled) != 1 {
		t.Errorf("expected 1 cancel, got %d", len(fc.subs.cancelled))
	}
	// Archive should have removed the ephemeral
	if len(reg.Ephemerals("subscription")) != 0 {
		t.Errorf("expected ephemeral to be archived after successful cancel+invoice, got %d remaining",
			len(reg.Ephemerals("subscription")))
	}
}
