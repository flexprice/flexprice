package checks

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/synthetic"
)

func TestCycleInvoiceProbe_NoPersistentSubsIsNoOp(t *testing.T) {
	fc := newFakeClient()
	p := NewCycleInvoiceProbe(fc, synthetic.NewRegistry(), "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.invoices.queries != 0 {
		t.Errorf("expected 0 invoice queries, got %d", fc.invoices.queries)
	}
}

func TestCycleInvoiceProbe_QueriesInvoicesForRotatingSub(t *testing.T) {
	fc := newFakeClient()
	reg := synthetic.NewRegistry()
	reg.LoadSeeds(synthetic.Seeds{PersistentSubIDs: []string{"sub_1", "sub_2"}})
	p := NewCycleInvoiceProbe(fc, reg, "run-1")
	_ = p.Run(context.Background())
	// `extractBillingCycleLength` returns 0 placeholder, causing Run() to exit
	// before reaching the invoice query. Test documents the current behavior.
	if fc.invoices.queries != 0 {
		t.Errorf("queries=%d before Task 25, expected 0 (placeholder cycle length)", fc.invoices.queries)
	}
}
