package checks

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
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

func TestCycleInvoiceProbe_QueriesInvoicesByCustomerID(t *testing.T) {
	fc := newFakeClient()
	sub1, sub2 := "sub_1", "sub_2"
	custID := "cust_abc"
	fc.subs.subs = map[string]sdktypes.DtoSubscriptionResponse{
		sub1: {ID: &sub1, CustomerID: &custID},
		sub2: {ID: &sub2, CustomerID: &custID},
	}
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PersistentSubIDs: []string{sub1, sub2}})
	p := NewCycleInvoiceProbe(fc, reg, "run-1")
	_ = p.Run(context.Background())
	// Get returns a sub with CustomerID → invoice query should fire using customer_id filter.
	if fc.invoices.queries == 0 {
		t.Errorf("expected at least 1 invoice query, got 0")
	}
	// Verify the filter passed used CustomerID not SubscriptionID.
	if fc.invoices.lastFilter.CustomerID == nil {
		t.Errorf("expected invoice query to use CustomerID filter, got nil CustomerID")
	}
	if fc.invoices.lastFilter.SubscriptionID != nil {
		t.Errorf("expected invoice query NOT to use SubscriptionID filter (unreliable), but SubscriptionID was set")
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
	custID := "cust_fresh"
	fc.subs.subs = map[string]sdktypes.DtoSubscriptionResponse{
		subID: {
			ID:                 &subID,
			CustomerID:         &custID,
			BillingPeriod:      &monthly,
			BillingPeriodCount: &count,
			CreatedAt:          &createdAt,
		},
	}

	// Invoice period_end was 5 days ago — well within 2*30d freshness window.
	// SubscriptionID is set so the client-side filter matches.
	recentPeriodEnd := time.Now().Add(-5 * 24 * time.Hour).Format(time.RFC3339)
	fc.invoices.invoices = []sdktypes.DtoInvoiceResponse{
		{SubscriptionID: &subID, PeriodEnd: &recentPeriodEnd},
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

// TestCycleInvoiceProbe_SubNotFoundSoftSkips verifies that when the sub lookup returns
// 404 (first-run race — PersistentSubIDs populated before the sub actually exists),
// the probe soft-skips rather than alerting.
func TestCycleInvoiceProbe_SubNotFoundSoftSkips(t *testing.T) {
	fc := newFakeClient()
	// Inject a 404 API error so the soft-skip guard fires.
	fc.subs.subErr = &sdkerrors.APIError{StatusCode: http.StatusNotFound, Message: "not found"}
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PersistentSubIDs: []string{"sub_missing"}})
	p := NewCycleInvoiceProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("expected nil (soft-skip) for 404 sub, got: %v", err)
	}
	if fc.invoices.queries != 0 {
		t.Errorf("expected 0 invoice queries after 404 soft-skip, got %d", fc.invoices.queries)
	}
}

// TestCycleInvoiceProbe_FailsOnStaleInvoice verifies that a lag > 2*cycle is caught.
func TestCycleInvoiceProbe_FailsOnStaleInvoice(t *testing.T) {
	fc := newFakeClient()

	monthly := sdktypes.BillingPeriodMonthly
	count := int64(1)
	createdAt := time.Now().Add(-120 * 24 * time.Hour).Format(time.RFC3339)
	subID := "sub_stale"
	custID := "cust_stale"
	fc.subs.subs = map[string]sdktypes.DtoSubscriptionResponse{
		subID: {
			ID:                 &subID,
			CustomerID:         &custID,
			BillingPeriod:      &monthly,
			BillingPeriodCount: &count,
			CreatedAt:          &createdAt,
		},
	}

	// Invoice period_end was 75 days ago — exceeds 2*30d = 60d.
	// SubscriptionID is set so the client-side filter matches.
	stalePeriodEnd := time.Now().Add(-75 * 24 * time.Hour).Format(time.RFC3339)
	fc.invoices.invoices = []sdktypes.DtoInvoiceResponse{
		{SubscriptionID: &subID, PeriodEnd: &stalePeriodEnd},
	}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PersistentSubIDs: []string{subID}})
	p := NewCycleInvoiceProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err == nil {
		t.Fatal("expected stale invoice to return an error, got nil")
	}
}

// TestCycleInvoiceProbe_ClientSideFiltersOtherSubInvoices verifies that invoices
// returned by the customer query that belong to a different subscription are
// excluded from the freshness check.
func TestCycleInvoiceProbe_ClientSideFiltersOtherSubInvoices(t *testing.T) {
	fc := newFakeClient()

	monthly := sdktypes.BillingPeriodMonthly
	count := int64(1)
	createdAt := time.Now().Add(-120 * 24 * time.Hour).Format(time.RFC3339)
	subID := "sub_target"
	otherSubID := "sub_other"
	custID := "cust_shared"
	fc.subs.subs = map[string]sdktypes.DtoSubscriptionResponse{
		subID: {
			ID:                 &subID,
			CustomerID:         &custID,
			BillingPeriod:      &monthly,
			BillingPeriodCount: &count,
			CreatedAt:          &createdAt,
		},
	}

	// The customer has invoices but they belong to a different sub — the target sub
	// has no invoices. The probe should soft-skip (sub is old but no invoices).
	recentPeriodEnd := time.Now().Add(-5 * 24 * time.Hour).Format(time.RFC3339)
	fc.invoices.invoices = []sdktypes.DtoInvoiceResponse{
		{SubscriptionID: &otherSubID, PeriodEnd: &recentPeriodEnd},
	}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PersistentSubIDs: []string{subID}})
	p := NewCycleInvoiceProbe(fc, reg, "run-1")
	// sub is 120 days old, >2*30d, and has no matching invoices → should alert.
	err := p.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for old sub with no matching invoices, got nil")
	}
}

// TestCycleInvoiceProbe_NoCustomerIDSkips verifies that when the subscription
// response has no customer_id, the probe skips gracefully without alerting.
func TestCycleInvoiceProbe_NoCustomerIDSkips(t *testing.T) {
	fc := newFakeClient()
	subID := "sub_nocust"
	fc.subs.subs = map[string]sdktypes.DtoSubscriptionResponse{
		subID: {ID: &subID}, // no CustomerID
	}
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PersistentSubIDs: []string{subID}})
	p := NewCycleInvoiceProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("expected skip (no customer_id), got: %v", err)
	}
	if fc.invoices.queries != 0 {
		t.Errorf("expected 0 invoice queries when customer_id absent, got %d", fc.invoices.queries)
	}
}
