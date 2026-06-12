package checks

import (
	"context"
	"strings"
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

	// Register customer ephemeral with external ID "e2eprobe-cust-eph-old".
	reg.RegisterEphemeral("customer", "e2eprobe-cust-eph-old", old)
	reg.RegisterEphemeral("subscription", "sub_old", old)
	reg.RegisterEphemeral("subscription", "sub_mid", mid)

	// Track the external customer ID in byExt so GetByExternalID works if called.
	fc.customers.byExt["e2eprobe-cust-eph-old"] = "cust-internal-old"

	// Populate subs so Get returns a CANCELLED status with customer ID info.
	cancelled := types.SubscriptionStatusCancelled
	extCustID := "e2eprobe-cust-eph-old"
	internalCustID := "cust-internal-old"
	fc.subs.subs = map[string]types.DtoSubscriptionResponse{
		"sub_old": {
			ID:                 strPtr("sub_old"),
			SubscriptionStatus: &cancelled,
			CustomerID:         strPtr(internalCustID),
			Customer:           &types.DtoCustomerResponse{ExternalID: strPtr(extCustID)},
		},
		"sub_mid": {ID: strPtr("sub_mid")},
	}

	s := NewCancelCustomerFlow(fc, reg, "run-1", InvoicePoll{Timeout: 30 * time.Millisecond, Interval: 5 * time.Millisecond})
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fc.subs.cancelled) != 1 || fc.subs.cancelled[0] != "sub_old" {
		t.Errorf("cancelled=%+v", fc.subs.cancelled)
	}
	// Verify Get was called (pollSubStatusCancelled must have fired).
	if fc.subs.gets == 0 {
		t.Errorf("expected at least 1 Get call, got 0")
	}
	// Verify the customer was also deleted (best-effort cleanup).
	if len(fc.customers.deleted) != 1 || fc.customers.deleted[0] != internalCustID {
		t.Errorf("customer deleted=%v, want [%s]", fc.customers.deleted, internalCustID)
	}
	// Verify the customer ephemeral was archived from the registry.
	remaining := reg.Ephemerals("customer")
	for _, e := range remaining {
		if e.ID == extCustID {
			t.Errorf("customer ephemeral %q still in registry after cancel; should have been archived", extCustID)
		}
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

// TestCancelCustomerFlow_StatusNeverCancelled_TimesOut verifies that if the sub
// never reaches CANCELLED status the probe times out with an informative error
// that includes the observed status.
func TestCancelCustomerFlow_StatusNeverCancelled_TimesOut(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.RegisterEphemeral("subscription", "sub_stuck", time.Now().Add(-time.Hour))

	// Sub stays ACTIVE — never transitions to cancelled.
	active := types.SubscriptionStatusActive
	fc.subs.subs = map[string]types.DtoSubscriptionResponse{
		"sub_stuck": {ID: strPtr("sub_stuck"), SubscriptionStatus: &active},
	}

	s := NewCancelCustomerFlow(fc, reg, "run-1", InvoicePoll{Timeout: 30 * time.Millisecond, Interval: 5 * time.Millisecond})
	err := s.Run(context.Background())
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "sub_stuck") {
		t.Errorf("error message missing sub ID: %v", msg)
	}
	if !strings.Contains(msg, "active") && !strings.Contains(msg, "cancelled") {
		t.Errorf("error message missing observed status: %v", msg)
	}
}
