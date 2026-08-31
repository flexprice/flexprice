package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
	"github.com/stretchr/testify/require"
)

func TestMultiCadenceInvoiceProbe_NoSeedSkips(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{}) // MultiCadenceSubID = ""
	p := NewMultiCadenceInvoiceProbe(fc, reg, "run-1", nil)
	require.NoError(t, p.Run(context.Background()))
}

// monthlyItem constructs a fan-out invoice line item for a specific source
// subscription-line-item and calendar month.
func monthlyItem(srcID string, year int, month time.Month) sdktypes.InvoiceLineItemResponse {
	id := srcID
	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC)
	return sdktypes.InvoiceLineItemResponse{
		SubscriptionLineItemID: &id,
		PeriodStart:            &start,
		PeriodEnd:              &end,
	}
}

func TestMultiCadenceInvoiceProbe_LatestInvoiceHasMonthlyLineItems(t *testing.T) {
	fc := newFakeClient()
	subID := "sub_quarterly_test"
	custID := "cust_test"
	quarterly := sdktypes.BillingPeriodQuarterly
	count := int64(1)
	invStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	invEnd := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	fc.subs.subs = map[string]sdktypes.SubscriptionResponse{
		subID: {
			ID:                 &subID,
			CustomerID:         &custID,
			BillingPeriod:      &quarterly,
			BillingPeriodCount: &count,
		},
	}
	// Mirror the seed plan: TWO monthly source line items (FIXED + USAGE),
	// each fanned out into three monthly windows = 6 line items total.
	fc.invoices.invoices = []sdktypes.InvoiceResponse{{
		SubscriptionID: &subID,
		PeriodStart:    &invStart,
		PeriodEnd:      &invEnd,
		LineItems: []sdktypes.InvoiceLineItemResponse{
			monthlyItem("sli_fixed", 2026, time.January),
			monthlyItem("sli_fixed", 2026, time.February),
			monthlyItem("sli_fixed", 2026, time.March),
			monthlyItem("sli_usage", 2026, time.January),
			monthlyItem("sli_usage", 2026, time.February),
			monthlyItem("sli_usage", 2026, time.March),
		},
	}}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{MultiCadenceSubID: subID})
	p := NewMultiCadenceInvoiceProbe(fc, reg, "run-1", nil)
	require.NoError(t, p.Run(context.Background()))
}

func TestMultiCadenceInvoiceProbe_SingleLineItemIsRegression(t *testing.T) {
	fc := newFakeClient()
	subID := "sub_quarterly_regr"
	custID := "cust_test"
	quarterly := sdktypes.BillingPeriodQuarterly
	count := int64(1)
	invStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	invEnd := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	sliID := "sli_regression"

	fc.subs.subs = map[string]sdktypes.SubscriptionResponse{
		subID: {
			ID:                 &subID,
			CustomerID:         &custID,
			BillingPeriod:      &quarterly,
			BillingPeriodCount: &count,
		},
	}
	fc.invoices.invoices = []sdktypes.InvoiceResponse{{
		SubscriptionID: &subID,
		PeriodStart:    &invStart,
		PeriodEnd:      &invEnd,
		LineItems: []sdktypes.InvoiceLineItemResponse{
			// Only ONE line item spanning the full quarter — this is the
			// pre-fan-out behavior and must trip the probe.
			{SubscriptionLineItemID: &sliID, PeriodStart: &invStart, PeriodEnd: &invEnd},
		},
	}}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{MultiCadenceSubID: subID})
	p := NewMultiCadenceInvoiceProbe(fc, reg, "run-1", nil)
	err := p.Run(context.Background())
	require.Error(t, err, "single-line quarterly invoice should be flagged as a fan-out regression")
}

// TestMultiCadenceInvoiceProbe_PartialFanOutIsRegression covers the case
// CodeRabbit flagged: one source line item fans out correctly (3 monthly
// windows) but a second source produces only 1. The old "count all monthly
// items" check would have passed (4 monthly items ≥ 3); the per-source
// check must reject it.
func TestMultiCadenceInvoiceProbe_PartialFanOutIsRegression(t *testing.T) {
	fc := newFakeClient()
	subID := "sub_quarterly_partial"
	custID := "cust_test"
	quarterly := sdktypes.BillingPeriodQuarterly
	count := int64(1)
	invStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	invEnd := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	brokenSLI := "sli_broken"
	brokenStart := invStart
	brokenEnd := invEnd

	fc.subs.subs = map[string]sdktypes.SubscriptionResponse{
		subID: {
			ID:                 &subID,
			CustomerID:         &custID,
			BillingPeriod:      &quarterly,
			BillingPeriodCount: &count,
		},
	}
	fc.invoices.invoices = []sdktypes.InvoiceResponse{{
		SubscriptionID: &subID,
		PeriodStart:    &invStart,
		PeriodEnd:      &invEnd,
		LineItems: []sdktypes.InvoiceLineItemResponse{
			monthlyItem("sli_good", 2026, time.January),
			monthlyItem("sli_good", 2026, time.February),
			monthlyItem("sli_good", 2026, time.March),
			// Broken price: single quarter-wide line item = fan-out regression
			// for this specific source, even though the total count is 4.
			{SubscriptionLineItemID: &brokenSLI, PeriodStart: &brokenStart, PeriodEnd: &brokenEnd},
		},
	}}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{MultiCadenceSubID: subID})
	p := NewMultiCadenceInvoiceProbe(fc, reg, "run-1", nil)
	err := p.Run(context.Background())
	require.Error(t, err, "one source line item with 1 monthly window (rest quarterly) must be flagged")
}
