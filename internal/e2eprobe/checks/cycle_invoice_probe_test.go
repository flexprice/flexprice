package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
)

func TestCycleInvoiceProbe_NoPersistentSubsIsNoOp(t *testing.T) {
	fc := newFakeClient()
	p := NewCycleInvoiceProbe(fc, e2eprobe.NewRegistry(), "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.invoices.queries != 0 {
		t.Errorf("expected 0 invoice queries, got %d", fc.invoices.queries)
	}
}

func TestCycleInvoiceProbe_QueriesInvoicesForRotatingSub(t *testing.T) {
	fc := newFakeClient()
	sub1, sub2 := "sub_1", "sub_2"
	fc.subs.subs = map[string]sdktypes.DtoSubscriptionResponse{
		sub1: {ID: &sub1},
		sub2: {ID: &sub2},
	}
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PersistentSubIDs: []string{sub1, sub2}})
	p := NewCycleInvoiceProbe(fc, reg, "run-1")
	_ = p.Run(context.Background())
	// Get returns a subscription response → extractBillingCycleLength
	// returns the 30-day default (no billing period set), so invoice query fires.
	if fc.invoices.queries == 0 {
		t.Errorf("expected at least 1 invoice query, got 0")
	}
}

// TestCycleInvoiceProbe_ChecksFreshness verifies the full happy path: sub with a
// known billing period + recent invoice → passes freshness invariant.
func TestCycleInvoiceProbe_ChecksFreshness(t *testing.T) {
	fc := newFakeClient()

	monthly := sdktypes.BillingPeriodMonthly
	count := int64(1)
	createdAt := time.Now().Add(-10 * 24 * time.Hour).Format(time.RFC3339) // 10 days old
	subID := "sub_fresh"
	fc.subs.subs = map[string]sdktypes.DtoSubscriptionResponse{
		subID: {
			ID:                 &subID,
			BillingPeriod:      &monthly,
			BillingPeriodCount: &count,
			CreatedAt:          &createdAt,
		},
	}

	// Invoice period_end was 5 days ago — well within 2*30d freshness window.
	recentPeriodEnd := time.Now().Add(-5 * 24 * time.Hour).Format(time.RFC3339)
	fc.invoices.invoices = []sdktypes.DtoInvoiceResponse{
		{PeriodEnd: &recentPeriodEnd},
	}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PersistentSubIDs: []string{subID}})
	p := NewCycleInvoiceProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("expected fresh invoice to pass, got: %v", err)
	}
	if fc.invoices.queries != 1 {
		t.Errorf("expected 1 invoice query, got %d", fc.invoices.queries)
	}
}

// TestCycleInvoiceProbe_FailsOnStaleInvoice verifies that a lag > 2*cycle is caught.
func TestCycleInvoiceProbe_FailsOnStaleInvoice(t *testing.T) {
	fc := newFakeClient()

	monthly := sdktypes.BillingPeriodMonthly
	count := int64(1)
	createdAt := time.Now().Add(-120 * 24 * time.Hour).Format(time.RFC3339)
	subID := "sub_stale"
	fc.subs.subs = map[string]sdktypes.DtoSubscriptionResponse{
		subID: {
			ID:                 &subID,
			BillingPeriod:      &monthly,
			BillingPeriodCount: &count,
			CreatedAt:          &createdAt,
		},
	}

	// Invoice period_end was 75 days ago — exceeds 2*30d = 60d.
	stalePeriodEnd := time.Now().Add(-75 * 24 * time.Hour).Format(time.RFC3339)
	fc.invoices.invoices = []sdktypes.DtoInvoiceResponse{
		{PeriodEnd: &stalePeriodEnd},
	}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PersistentSubIDs: []string{subID}})
	p := NewCycleInvoiceProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err == nil {
		t.Fatal("expected stale invoice to return an error, got nil")
	}
}
