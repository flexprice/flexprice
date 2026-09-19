package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// TestE2E_RevenueFactsLifecycle walks the full life of revenue facts against
// the real billing preview engine: repeated ingestion (including backdated
// events) with rollups at different times, finalize, void, re-finalize via
// the invoice fallback, drift sweep with auto-correct, and export watermarks.
func (s *RevenueRollupSuite) TestE2E_RevenueFactsLifecycle() {
	ctx := s.ctx
	s.seedWorkedExample(ctx)
	s.enableRevenueAnalytics(ctx)

	previewTotal := func() decimal.Decimal {
		invReq, err := NewBillingService(s.serviceParams()).PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
			Subscription:   s.sub,
			PeriodStart:    s.periodStart,
			PeriodEnd:      s.periodEnd,
			ReferencePoint: types.ReferencePointRevenueFacts,
		})
		s.NoError(err)
		return invReq.Subtotal
	}
	provisionalRows := func() []*revenuefact.RevenueFact {
		rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
		s.NoError(err)
		return rows
	}
	sumNet := func(rows []*revenuefact.RevenueFact) decimal.Decimal {
		total := decimal.Zero
		for _, r := range rows {
			total = total.Add(r.NetAmount)
		}
		return total
	}

	// --- Stage 1: first rollup over the initially ingested usage ---
	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))
	firstRows := provisionalRows()
	s.NotEmpty(firstRows)
	firstTotal := sumNet(firstRows)
	s.True(firstTotal.Equal(previewTotal()), "rollup must reconcile to the engine preview")

	versionByID := make(map[string]int64, len(firstRows))
	for _, r := range firstRows {
		versionByID[r.ID] = r.Version
	}

	// --- Stage 2: backdated + new usage arrives, rollup runs again later ---
	extra := make([]*events.MeterUsage, 0, 2)
	for i, ts := range []time.Time{
		s.periodStart.Add(2 * time.Hour),               // backdated onto day 1
		s.periodStart.AddDate(0, 0, 20).Add(time.Hour), // fresh usage on day 21
	} {
		id := s.GetUUID()
		extra = append(extra, &events.MeterUsage{
			Event: events.Event{
				ID:                 id,
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				EventName:          "api_call_rollup_wk",
				ExternalCustomerID: "ext_rollup_wk",
				CustomerID:         "cust_rollup_wk",
				Timestamp:          ts,
				IngestedAt:         ts.Add(time.Duration(i) * time.Minute),
			},
			MeterID:    "meter_rollup_wk",
			QtyTotal:   decimal.NewFromInt(500),
			UniqueHash: fmt.Sprintf("e2e_extra:%s", id),
		})
	}
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, extra))

	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))
	secondRows := provisionalRows()
	s.Equal(len(firstRows), len(secondRows), "recompute must update rows in place, never duplicate")
	for _, r := range secondRows {
		s.Equal(versionByID[r.ID]+1, r.Version, "every row must bump its version on recompute")
	}
	s.True(sumNet(secondRows).Equal(previewTotal()), "backdated + new usage must be absorbed and still reconcile")

	// --- Stage 3: the invoice finalizes; provisional rows flip to FINAL ---
	inv := s.finalizeCurrentPreview(ctx)
	s.NoError(s.svc.FinalizeSubscriptionPeriod(ctx, inv.ID))

	s.Empty(provisionalRows(), "the flip must consume every provisional row")
	booked, err := s.store.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.True(sumNet(booked).Equal(inv.Subtotal.Sub(inv.TotalDiscount)), "FINAL rows must sum to the invoice")

	// --- Stage 4: the invoice is voided; facts net to zero via reverts ---
	// Mirror VoidInvoice: the status flips in the same operation that
	// triggers the revert hook.
	inv.InvoiceStatus = types.InvoiceStatusVoided
	voidedAt := time.Now().UTC()
	inv.VoidedAt = &voidedAt
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))
	s.NoError(s.svc.RevertInvoiceFacts(ctx, inv.ID))
	afterVoid, err := s.store.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.True(sumNet(afterVoid).IsZero(), "a voided invoice's facts must net to zero")
	s.Greater(len(afterVoid), len(booked), "reverts append, never edit")

	// --- Stage 5: a replacement invoice finalizes with no provisional rows;
	// the fallback derives rows from the invoice itself ---
	inv2 := s.finalizeCurrentPreview(ctx)
	s.NoError(s.svc.FinalizeSubscriptionPeriod(ctx, inv2.ID))
	booked2, err := s.store.ListByInvoiceID(ctx, inv2.ID)
	s.NoError(err)
	s.NotEmpty(booked2, "the invoice fallback must book rows when no provisional rows exist")
	s.True(sumNet(booked2).Equal(inv2.Subtotal.Sub(inv2.TotalDiscount)))

	// --- Stage 6: drift — an invoice finalized without its flip hook is
	// detected, and auto-correct repairs it ---
	inv3 := s.finalizeCurrentPreview(ctx) // hook deliberately not run

	cfg := *s.GetConfig()
	cfg.Analytics.RevenueRollup.AutoCorrect = true
	params := s.serviceParams()
	params.Config = &cfg
	sweeper := NewRevenueService(params)

	checked, drifted, corrected, err := sweeper.SweepDrift(ctx, s.periodStart)
	s.NoError(err)
	s.GreaterOrEqual(checked, 3, "all three invoices sit inside the sweep window")
	s.GreaterOrEqual(drifted, 1, "the missing flip must be detected")
	s.GreaterOrEqual(corrected, 1, "auto-correct must repair it")

	booked3, err := s.store.ListByInvoiceID(ctx, inv3.ID)
	s.NoError(err)
	s.True(sumNet(booked3).Equal(inv3.Subtotal.Sub(inv3.TotalDiscount)), "the corrected invoice must reconcile")

	// A clean second sweep: everything reconciles, nothing drifts.
	_, drifted, corrected, err = sweeper.SweepDrift(ctx, s.periodStart)
	s.NoError(err)
	s.Zero(drifted, "a repaired system must sweep clean")
	s.Zero(corrected)

	// --- Stage 7: export — snapshot, then watermark-incremental ---
	snapshot := s.exportAll(ctx, time.Time{})
	s.NotEmpty(snapshot)
	watermark := snapshot[len(snapshot)-1].ComputedAt

	s.Empty(s.exportAll(ctx, watermark), "nothing changed since the watermark")

	// New usage + rollup re-computes provisional rows for the (still open)
	// period — only those cross the watermark.
	late := extra[0]
	late.ID = s.GetUUID()
	late.UniqueHash = "e2e_late:" + late.ID
	late.Timestamp = s.periodStart.AddDate(0, 0, 25)
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{late}))
	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))

	incremental := s.exportAll(ctx, watermark)
	s.NotEmpty(incremental, "re-upserted rows must cross the watermark")
	s.Less(len(incremental), len(snapshot), "an incremental export must not re-send everything")

	// Export is denied once the tenant's setting is gone.
	s.NoError(s.GetStores().SettingsRepo.DeleteByKey(ctx, types.SettingKeyRevenueAnalyticsConfig))
	_, err = s.svc.ExportFacts(ctx, time.Time{}, "", 0)
	s.Error(err, "export must be denied for tenants that have not opted in")
}

// finalizeCurrentPreview persists a finalized invoice built from the current
// preview — the invoice the billing engine would have issued right now.
func (s *RevenueRollupSuite) finalizeCurrentPreview(ctx context.Context) *invoice.Invoice {
	invReq, err := NewBillingService(s.serviceParams()).PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   s.sub,
		PeriodStart:    s.periodStart,
		PeriodEnd:      s.periodEnd,
		ReferencePoint: types.ReferencePointRevenueFacts,
	})
	s.NoError(err)

	inv, err := invReq.ToInvoice(ctx)
	s.NoError(err)
	inv.InvoiceStatus = types.InvoiceStatusFinalized
	now := time.Now().UTC()
	inv.FinalizedAt = &now
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))
	return inv
}

// exportAll drains ExportFacts pages from the given watermark.
func (s *RevenueRollupSuite) exportAll(ctx context.Context, since time.Time) []*revenuefact.RevenueFact {
	var all []*revenuefact.RevenueFact
	afterID := ""
	for {
		page, err := s.svc.ExportFacts(ctx, since, afterID, 100)
		s.NoError(err)
		all = append(all, page...)
		if len(page) < 100 {
			return all
		}
		since = page[len(page)-1].ComputedAt
		afterID = page[len(page)-1].ID
	}
}
