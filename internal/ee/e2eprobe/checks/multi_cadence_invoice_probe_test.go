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
	p := NewMultiCadenceInvoiceProbe(fc, reg, "run-1")
	require.NoError(t, p.Run(context.Background()))
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
	fc.invoices.invoices = []sdktypes.InvoiceResponse{{
		SubscriptionID: &subID,
		PeriodStart:    &invStart,
		PeriodEnd:      &invEnd,
		LineItems: []sdktypes.InvoiceLineItemResponse{
			{PeriodStart: dateP(2026, 1, 1), PeriodEnd: dateP(2026, 2, 1)},
			{PeriodStart: dateP(2026, 2, 1), PeriodEnd: dateP(2026, 3, 1)},
			{PeriodStart: dateP(2026, 3, 1), PeriodEnd: dateP(2026, 4, 1)},
		},
	}}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{MultiCadenceSubID: subID})
	p := NewMultiCadenceInvoiceProbe(fc, reg, "run-1")
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
			{PeriodStart: &invStart, PeriodEnd: &invEnd},
		},
	}}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{MultiCadenceSubID: subID})
	p := NewMultiCadenceInvoiceProbe(fc, reg, "run-1")
	err := p.Run(context.Background())
	require.Error(t, err, "single-line quarterly invoice should be flagged as a fan-out regression")
}
