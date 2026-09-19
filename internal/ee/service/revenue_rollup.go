package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Skip reasons logged by RollupSubscription. Multi-period commitments are
// skipped whole rather than written wrong.
const (
	revenueRollupSkipMultiPeriodCommitment = "multi_period_commitment"
)

// RevenueService writes and maintains revenue_facts rows. It is a shadow
// write-path: it re-runs the billing preview, never mutates billing state,
// and logs reconciliation mismatches instead of blocking on them.
type RevenueService interface {
	// RollupSubscription splits the subscription's current billing period
	// into PROVISIONAL revenue_facts rows. Subscriptions it cannot split
	// faithfully (discounts, multi-period commitments) are skipped with a log.
	RollupSubscription(ctx context.Context, subscriptionID string) error

	// RollupDirty rolls every opted-in subscription with activity since the
	// given time. Over-rolling is harmless: the upsert is idempotent.
	RollupDirty(ctx context.Context, since time.Time) (rolled, skipped int, err error)

	// FinalizeSubscriptionPeriod flips the invoice's PROVISIONAL rows to
	// FINAL and stamps them with the invoice. When no provisional rows exist
	// it derives rows from the invoice itself first. Runs async after invoice
	// finalization.
	FinalizeSubscriptionPeriod(ctx context.Context, invoiceID string) error

	// RevertInvoiceFacts writes a negating twin for every FINAL row of a
	// voided invoice — FINAL rows are never edited. Idempotent; runs async
	// after VoidInvoice.
	RevertInvoiceFacts(ctx context.Context, invoiceID string) error

	// SweepDrift compares recently finalized/voided invoices against their
	// booked facts, logging drift and — only when auto_correct is on —
	// repairing via revert + re-derive + flip.
	SweepDrift(ctx context.Context, since time.Time) (checked, drifted, corrected int, err error)

	// ExportFacts pages the tenant's facts recomputed after since, ordered by
	// (computed_at, id). Denied unless the tenant opted in via settings.
	ExportFacts(ctx context.Context, since time.Time, afterID string, limit int) ([]*revenuefact.RevenueFact, error)
}

type revenueService struct {
	ServiceParams
}

// NewRevenueService returns the revenue_facts service.
func NewRevenueService(params ServiceParams) RevenueService {
	return &revenueService{ServiceParams: params}
}

// lineItemRows keeps one line item's rows together for reconciliation logging.
type lineItemRows struct {
	rows       []*revenuefact.RevenueFact
	amount     decimal.Decimal
	identifier string
}

func (s *revenueService) RollupSubscription(ctx context.Context, subscriptionID string) error {
	_, err := s.rollupSubscription(ctx, subscriptionID)
	return err
}

// rollupSubscription also reports policy skips, which RollupDirty tallies but
// the interface method does not expose.
func (s *revenueService) rollupSubscription(ctx context.Context, subscriptionID string) (skipped bool, err error) {
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return false, err
	}
	return s.rollupSubscriptionForPeriod(ctx, sub, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
}

// rollupSubscriptionForPeriod rolls one explicit billing window (periodEnd exclusive)
func (s *revenueService) rollupSubscriptionForPeriod(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time) (skipped bool, err error) {
	subscriptionID := sub.ID

	// Multi-period commitments are deliberately OUT OF SCOPE for revenue_facts
	// for now (their true-up needs a rollup-maintained prior base; see ERD
	// open question Q3) — such subscriptions are skipped, not decomposed.
	// Line items also carry a CommitmentDuration field the engine ignores
	// today; skip defensively if one is ever set to a different period.
	multiPeriod := isMultiPeriodCommitment(sub)
	for _, li := range sub.LineItems {
		if li.CommitmentDuration != nil && *li.CommitmentDuration != sub.BillingPeriod {
			multiPeriod = true
			break
		}
	}
	if multiPeriod {
		s.Logger.Info(ctx, "revenue_rollup_skipped",
			"reason", revenueRollupSkipMultiPeriodCommitment,
			"subscription_id", subscriptionID)
		return true, nil
	}

	billingSvc := NewBillingService(s.ServiceParams)
	invReq, err := billingSvc.PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   sub,
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		ReferencePoint: types.ReferencePointRevenueFacts,
	})
	if err != nil {
		return false, err
	}

	if len(invReq.LineItems) == 0 {
		return false, nil
	}

	// Coupon amounts are not resolved by the preview — dry-run them here so
	// discounted subscriptions decompose instead of being skipped.
	lineDiscounts, invoiceDiscounts, err := s.resolveDiscounts(ctx, invReq)
	if err != nil {
		return false, err
	}

	// When usage exceeds a commitment, the engine reduces the usage lines and
	// adds an is_overage line whose split the daily curve cannot reproduce —
	// keep every usage line whole (period_only) instead of skipping the
	// subscription.
	overagePresent := hasOverageLine(invReq)

	inputs, err := s.loadRollupInputs(ctx, sub, periodStart, periodEnd, invReq.LineItems)
	if err != nil {
		return false, err
	}

	var allRows []*revenuefact.RevenueFact
	var groups []lineItemRows

	for i := range invReq.LineItems {
		item := &invReq.LineItems[i]

		itemPeriodStart := lo.FromPtr(item.PeriodStart)
		if itemPeriodStart.IsZero() {
			itemPeriodStart = periodStart
		}
		itemPeriodEndExclusive := lo.FromPtr(item.PeriodEnd)
		if itemPeriodEndExclusive.IsZero() {
			itemPeriodEndExclusive = periodEnd
		}
		// The engine's line item period end is half-open/exclusive; every
		// internal type here (revenuePeriod, previewLineItem.PeriodEnd) is inclusive
		// of the last calendar day, so convert once at the boundary.
		itemPeriod := revenuePeriod{Start: itemPeriodStart, End: itemPeriodEndExclusive.AddDate(0, 0, -1)}

		itemSubscriptionID := sub.ID
		if item.SubscriptionID != nil && *item.SubscriptionID != "" {
			itemSubscriptionID = *item.SubscriptionID
		}
		itemCustomerID := sub.CustomerID
		if childID, ok := item.Metadata[string(types.InvoiceLineItemMetadataKeyChildCustomerID)]; ok && childID != "" {
			itemCustomerID = childID
		}

		base := previewLineItem{
			TenantID:        sub.TenantID,
			EnvironmentID:   sub.EnvironmentID,
			CustomerID:      itemCustomerID,
			SubscriptionID:  itemSubscriptionID,
			SubLineItemID:   lo.FromPtr(item.SubscriptionLineItemID),
			Currency:        sub.Currency,
			Metadata:        item.Metadata,
			EngineAmount:    item.Amount,
			LineDiscount:    lineDiscounts[i],
			InvoiceDiscount: invoiceDiscounts[i],
			EntitlementQty:  lo.FromPtr(item.AdjustedEntitlementQuantity),
			PeriodStart:     itemPeriod.Start,
			PeriodEnd:       itemPeriod.End,
		}
		if base.SubLineItemID == "" {
			// Engine aggregate lines (subscription true-up and cumulative
			// overage/true-up) are built without a SubscriptionLineItemID;
			// fall back to the subscription id so the grain stays non-null.
			base.SubLineItemID = itemSubscriptionID
		}
		// The line item's net after discounts — every reconciliation below
		// compares against this, matching invoice Subtotal - TotalDiscount.
		netAmount := item.Amount.Sub(base.LineDiscount).Sub(base.InvoiceDiscount)

		isTrueup := item.Metadata.GetBool(types.MetadataKeyIsCommitmentTrueup)
		isOverage := item.Metadata.GetBool(types.MetadataKeyIsOverage)

		switch {
		case isTrueup || isOverage:
			// The engine assigns these lines a fresh random price_id every
			// compute; use a stable synthetic id so recomputes update in place.
			base.Price = &price.Price{ID: stableTrueupPriceID(base.SubLineItemID, itemSubscriptionID, isOverage)}

			row := decomposeCommitmentTrueup(base, itemPeriod)
			if isOverage {
				row = decomposeOverage(base, itemPeriod)
			}
			allRows = append(allRows, row)
			groups = append(groups, lineItemRows{rows: []*revenuefact.RevenueFact{row}, amount: netAmount, identifier: base.Price.ID})

		case lo.FromPtr(item.PriceType) == string(types.PRICE_TYPE_FIXED):
			p, hydrateErr := inputs.price(lo.FromPtr(item.PriceID))
			if hydrateErr != nil {
				return false, hydrateErr
			}
			base.Price = p
			if row := decomposeFixed(base, itemPeriod); row != nil {
				allRows = append(allRows, row)
				groups = append(groups, lineItemRows{rows: []*revenuefact.RevenueFact{row}, amount: netAmount, identifier: lo.FromPtr(item.PriceID)})
			}

		case lo.FromPtr(item.PriceType) == string(types.PRICE_TYPE_USAGE):
			// A missing price or meter is a hard error — never let
			// decompositionMode(nil, nil) silently default to Marginal.
			p, hydrateErr := inputs.price(lo.FromPtr(item.PriceID))
			if hydrateErr != nil {
				return false, hydrateErr
			}
			m, hydrateErr := inputs.meter(lo.FromPtr(item.MeterID))
			if hydrateErr != nil {
				return false, hydrateErr
			}
			base.Price = p
			base.Meter = m

			grants := inputs.grants(m.ID)
			grantBilled := grantsBillable(subLineItemByID(sub, base.SubLineItemID), p, m, grants)

			mode := decompositionMode(p, m)
			if overagePresent {
				// Commitment-reduced usage charges through a split the daily
				// curve cannot reproduce — keep the engine total whole.
				mode = types.PeriodOnly
			}

			var rows []*revenuefact.RevenueFact
			switch {
			case !overagePresent && grantBilled:
				// Grant-billed lines get their own per-day split: usage inside
				// the quota-crossed windows is billed, the rest entitled.
				curve, curveOK, curveErr := s.buildGrantOverageCurve(ctx, grantCurveInput{
					Price:               p,
					Meter:               m,
					PeriodStart:         itemPeriod.Start,
					PeriodEnd:           itemPeriod.exclusiveEnd(),
					EngineAmount:        item.Amount,
					Grants:              grants,
					ExternalCustomerIDs: inputs.extCustomerIDs,
					Timezone:            sub.Timezone,
				})
				if curveErr != nil {
					return false, curveErr
				}
				if curveOK {
					rows = decomposeUsageMarginal(base, curve)
				} else {
					rows = []*revenuefact.RevenueFact{decomposeUsagePeriodOnly(base, itemPeriod)}
				}
			case mode == types.Marginal:
				curve, curveErr := s.buildUsageCurve(ctx, usageCurveInput{
					Price:       p,
					MeterID:     m.ID,
					PeriodStart: itemPeriod.Start,
					// buildUsageCurve's upper bound is exclusive — convert back from the inclusive day.
					PeriodEnd:           itemPeriod.exclusiveEnd(),
					EntitlementLimit:    inputs.entitlementLimits[m.ID],
					ExternalCustomerIDs: inputs.extCustomerIDs,
					Timezone:            sub.Timezone,
				})
				if curveErr != nil {
					return false, curveErr
				}
				rows = decomposeUsageMarginal(base, curve)
			default:
				rows = []*revenuefact.RevenueFact{decomposeUsagePeriodOnly(base, itemPeriod)}
			}
			allRows = append(allRows, rows...)
			groups = append(groups, lineItemRows{rows: rows, amount: netAmount, identifier: lo.FromPtr(item.PriceID)})

		default:
			s.Logger.Info(ctx, "revenue_rollup_line_item_skipped",
				"subscription_id", subscriptionID,
				"price_type", lo.FromPtr(item.PriceType),
				"sub_line_item_id", base.SubLineItemID)
		}
	}

	// Reconciliation is shadow-only. A mismatch is logged/metriced —
	// never blocks the write, and billing itself is never touched.
	for _, g := range groups {
		for _, row := range g.rows {
			if residual, ok := reconcileRow(row); !ok {
				s.Logger.Info(ctx, "revenue_reconciliation_mismatch",
					"scope", "row", "subscription_id", subscriptionID,
					"fact_id", row.ID, "day", row.Day, "residual", residual.String())
			}
		}
		if residual, ok := reconcileLineItem(g.rows, g.amount); !ok {
			s.Logger.Info(ctx, "revenue_reconciliation_mismatch",
				"scope", "line_item", "subscription_id", subscriptionID,
				"line_item_id", g.identifier, "residual", residual.String())
		}
	}
	totalDiscount := decimal.Zero
	for i := range lineDiscounts {
		totalDiscount = totalDiscount.Add(lineDiscounts[i]).Add(invoiceDiscounts[i])
	}
	if residual, ok := reconcileInvoice(allRows, invReq.Subtotal.Sub(totalDiscount)); !ok {
		s.Logger.Info(ctx, "revenue_reconciliation_mismatch",
			"scope", "invoice", "subscription_id", subscriptionID, "residual", residual.String())
	}

	if len(allRows) == 0 {
		return false, nil
	}

	// Every row is PROVISIONAL with a non-empty price_id by construction;
	// re-running upserts in place instead of duplicating.
	if err := s.RevenueFactRepo.UpsertProvisional(ctx, allRows); err != nil {
		return false, err
	}

	return false, nil
}

func (s *revenueService) RollupDirty(ctx context.Context, since time.Time) (rolled, skipped int, err error) {
	err = s.forEachOptedInEnvironment(ctx, "revenue rollup dirty scan", func(envCtx context.Context) error {
		envRolled, envSkipped, envErr := s.rollupDirtyForEnvironment(envCtx, since)
		rolled += envRolled
		skipped += envSkipped
		return envErr
	})
	return rolled, skipped, err
}

// forEachOptedInEnvironment runs fn once per (tenant, environment) that opted
// in via revenue_analytics_config, with tenant/environment set on ctx — never
// a scan across all tenants. One environment's failure is logged and does not
// abort the others.
func (s *revenueService) forEachOptedInEnvironment(ctx context.Context, op string, fn func(ctx context.Context) error) error {
	tenantEnvConfigs, err := s.SettingsRepo.ListAllTenantEnvSettingsByKey(ctx, types.SettingKeyRevenueAnalyticsConfig)
	if err != nil {
		return err
	}

	for _, tec := range tenantEnvConfigs {
		cfg, cfgErr := utils.ToStruct[types.RevenueAnalyticsConfig](tec.Config)
		if cfgErr != nil {
			s.Logger.Info(ctx, "skipping tenant with malformed revenue_analytics_config",
				"tenant_id", tec.TenantID,
				"environment_id", tec.EnvironmentID,
				"error", cfgErr)
			continue
		}
		if !cfg.Enabled {
			continue
		}

		tenantCtx := types.SetTenantID(ctx, tec.TenantID)
		tenantCtx = types.SetEnvironmentID(tenantCtx, tec.EnvironmentID)

		if envErr := fn(tenantCtx); envErr != nil {
			s.Logger.Error(ctx, op+" failed for environment",
				"error", envErr,
				"tenant_id", tec.TenantID,
				"environment_id", tec.EnvironmentID)
		}
	}
	return nil
}

// rollupDirtyForEnvironment scans one (tenant, environment)'s active
// subscriptions in pages and rolls every one with activity since `since`.
func (s *revenueService) rollupDirtyForEnvironment(ctx context.Context, since time.Time) (rolled, skipped int, err error) {
	const batchSize = 1000
	offset := 0

	for {
		filter := types.NewSubscriptionFilter()
		filter.Limit = lo.ToPtr(batchSize)
		filter.Offset = lo.ToPtr(offset)
		filter.Status = lo.ToPtr(types.StatusPublished)
		filter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusActive}
		subs, listErr := s.SubRepo.List(ctx, filter)
		if listErr != nil {
			return rolled, skipped, listErr
		}
		if len(subs) == 0 {
			return rolled, skipped, nil
		}

		for _, sub := range subs {
			if sub.UpdatedAt.Before(since) && sub.CurrentPeriodStart.Before(since) && sub.CurrentPeriodEnd.Before(since) {
				continue
			}

			wasSkipped, rollErr := s.rollupSubscription(ctx, sub.ID)
			if rollErr != nil {
				// Shadow write-path: one subscription's failure must never abort the
				// batch. Logged loud (never silent), tallied as skipped since it did
				// not produce rows.
				s.Logger.Error(ctx, "revenue rollup failed for subscription",
					"error", rollErr, "subscription_id", sub.ID)
				skipped++
				continue
			}
			if wasSkipped {
				skipped++
				continue
			}
			rolled++
		}

		if len(subs) < batchSize {
			return rolled, skipped, nil
		}
		offset += batchSize
	}
}

// finalizePeriodGroup identifies one (subscription, period) grain touched by
// FinalizeSubscriptionPeriod, used to re-list the FINAL rows for
// reconciliation after every line item's flip has been issued.
type finalizePeriodGroup struct {
	subscriptionID string
	periodStart    time.Time
	periodEnd      time.Time
}

func (s *revenueService) FinalizeSubscriptionPeriod(ctx context.Context, invoiceID string) error {
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return err
	}

	flipped, groupSeen, err := s.flipInvoiceLineItems(ctx, inv)
	if err != nil {
		return err
	}
	if len(groupSeen) == 0 {
		return nil
	}

	// Zero flipped rows means no provisional rows existed for this period
	// (re-drafted after a void, or never scheduled). Derive rows from the
	// finalized invoice itself, then flip again.
	if flipped == 0 {
		if jitErr := s.rollupFromInvoice(ctx, inv); jitErr != nil {
			return jitErr
		}
		if flipped, _, err = s.flipInvoiceLineItems(ctx, inv); err != nil {
			return err
		}
		if flipped == 0 {
			// Still nothing to stamp (e.g. only non-subscription line items) —
			// surface the gap, never block.
			s.Logger.Info(ctx, "revenue_facts_flip_gap", "invoice_id", invoiceID)
		}
	}

	// Re-check that the flipped rows sum to the invoice total. A mismatch is
	// logged, never blocks — and rows from other invoices in the same window
	// are excluded by the invoice_id filter.
	var finalRows []*revenuefact.RevenueFact
	for _, g := range groupSeen {
		rows, err := s.RevenueFactRepo.ListBySubscriptionPeriod(ctx, g.subscriptionID, g.periodStart, g.periodEnd, types.FactFinal)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.InvoiceID != nil && *r.InvoiceID == invoiceID {
				finalRows = append(finalRows, r)
			}
		}
	}

	if residual, ok := reconcileInvoice(finalRows, inv.Subtotal.Sub(inv.TotalDiscount)); !ok {
		s.Logger.Info(ctx, "revenue_reconciliation_mismatch",
			"scope", "invoice_final", "invoice_id", invoiceID, "residual", residual.String())
	}

	return nil
}

// flipInvoiceLineItems issues one FlipToFinal per flippable line item of inv,
// returning the total rows flipped and the distinct (subscription, period)
// grains touched.
func (s *revenueService) flipInvoiceLineItems(ctx context.Context, inv *invoice.Invoice) (int, map[string]finalizePeriodGroup, error) {
	flipped := 0
	groupSeen := make(map[string]finalizePeriodGroup)

	for _, li := range inv.LineItems {
		// Join on the LINE ITEM's own subscription, never the invoice's —
		// grouped invoicing can bill several child subscriptions on one
		// invoice, each carrying its own revenue_facts rows.
		subscriptionID := lo.FromPtr(li.SubscriptionID)
		periodStart := lo.FromPtr(li.PeriodStart)
		periodEndExclusive := lo.FromPtr(li.PeriodEnd)
		if subscriptionID == "" || periodStart.IsZero() || periodEndExclusive.IsZero() {
			// Non-subscription line items (one-off charges, credits, etc.) never
			// produced revenue_facts rows in RollupSubscription — nothing to flip.
			continue
		}
		// The invoice line item's period end is half-open/exclusive, same as
		// the preview engine's — convert to the inclusive day bound
		// revenue_facts rows are keyed/queried on.
		periodEnd := periodEndExclusive.AddDate(0, 0, -1)

		priceID := lo.FromPtr(li.PriceID)
		isTrueup := li.Metadata.GetBool(types.MetadataKeyIsCommitmentTrueup)
		isOverage := li.Metadata.GetBool(types.MetadataKeyIsOverage)
		if isTrueup || isOverage {
			// True-up/overage lines carry a random price_id per compute; match
			// the provisional rows by re-deriving the same stable synthetic id.
			priceID = stableTrueupPriceID(lo.FromPtr(li.SubscriptionLineItemID), subscriptionID, isOverage)
		}

		n, err := s.RevenueFactRepo.FlipToFinal(ctx, subscriptionID, priceID, periodStart, periodEnd, inv.ID, li.ID)
		if err != nil {
			return flipped, groupSeen, err
		}
		flipped += n

		key := fmt.Sprintf("%s|%d|%d", subscriptionID, periodStart.UnixNano(), periodEnd.UnixNano())
		groupSeen[key] = finalizePeriodGroup{subscriptionID: subscriptionID, periodStart: periodStart, periodEnd: periodEnd}
	}

	return flipped, groupSeen, nil
}

// rollupFromInvoice derives period_only PROVISIONAL rows from a finalized
// invoice.s own line items — the fallback when none exist for its period.
// Per-day usage splitting is deliberately not reconstructed here.
func (s *revenueService) rollupFromInvoice(ctx context.Context, inv *invoice.Invoice) error {
	priceCache := map[string]*price.Price{}
	meterCache := map[string]*meter.Meter{}
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	var rows []*revenuefact.RevenueFact
	for _, li := range inv.LineItems {
		subscriptionID := lo.FromPtr(li.SubscriptionID)
		periodStart := lo.FromPtr(li.PeriodStart)
		periodEndExclusive := lo.FromPtr(li.PeriodEnd)
		if subscriptionID == "" || periodStart.IsZero() || periodEndExclusive.IsZero() {
			continue
		}
		period := revenuePeriod{Start: periodStart, End: periodEndExclusive.AddDate(0, 0, -1)}

		base := previewLineItem{
			TenantID:        tenantID,
			EnvironmentID:   environmentID,
			CustomerID:      li.CustomerID,
			SubscriptionID:  subscriptionID,
			SubLineItemID:   lo.FromPtr(li.SubscriptionLineItemID),
			Currency:        li.Currency,
			Metadata:        li.Metadata,
			EngineAmount:    li.Amount,
			LineDiscount:    li.LineItemDiscount,
			InvoiceDiscount: li.InvoiceLevelDiscount,
			EntitlementQty:  lo.FromPtr(li.AdjustedEntitlementQuantity),
			PeriodStart:     period.Start,
			PeriodEnd:       period.End,
		}
		if base.SubLineItemID == "" {
			// Invoice line items may lack a subscription-line-item id; the
			// invoice line item id is stable for this invoice.
			base.SubLineItemID = li.ID
		}

		isTrueup := li.Metadata.GetBool(types.MetadataKeyIsCommitmentTrueup)
		isOverage := li.Metadata.GetBool(types.MetadataKeyIsOverage)
		switch {
		case isTrueup || isOverage:
			// Derive the synthetic id from the RAW subscription-line-item id,
			// exactly like flipInvoiceLineItems does — never from the grain
			// fallback above, or the flip would miss these rows.
			base.Price = &price.Price{ID: stableTrueupPriceID(lo.FromPtr(li.SubscriptionLineItemID), subscriptionID, isOverage)}
			row := decomposeCommitmentTrueup(base, period)
			if isOverage {
				row = decomposeOverage(base, period)
			}
			rows = append(rows, row)

		case lo.FromPtr(li.PriceType) == string(types.PRICE_TYPE_FIXED):
			p, err := s.getPrice(ctx, priceCache, lo.FromPtr(li.PriceID))
			if err != nil {
				return err
			}
			base.Price = p
			if row := decomposeFixed(base, period); row != nil {
				rows = append(rows, row)
			}

		case lo.FromPtr(li.PriceType) == string(types.PRICE_TYPE_USAGE):
			p, err := s.getPrice(ctx, priceCache, lo.FromPtr(li.PriceID))
			if err != nil {
				return err
			}
			base.Price = p
			if meterID := lo.FromPtr(li.MeterID); meterID != "" {
				m, err := s.getMeter(ctx, meterCache, meterID)
				if err != nil {
					return err
				}
				base.Meter = m
			}
			rows = append(rows, decomposeUsagePeriodOnly(base, period))

		default:
			s.Logger.Info(ctx, "revenue_jit_rollup_line_item_skipped",
				"invoice_id", inv.ID,
				"invoice_line_item_id", li.ID,
				"price_type", lo.FromPtr(li.PriceType))
		}
	}

	if len(rows) == 0 {
		return nil
	}
	return s.RevenueFactRepo.UpsertProvisional(ctx, rows)
}

// RevertInvoiceFacts posts contra rows for every FINAL fact stamped with a
// now-voided invoice — FINAL rows are immutable, so a void reverses them
// rather than editing or deleting. Idempotent via the repository.
func (s *revenueService) RevertInvoiceFacts(ctx context.Context, invoiceID string) error {
	n, err := s.RevenueFactRepo.RevertByInvoice(ctx, invoiceID)
	if err != nil {
		return err
	}
	if n > 0 {
		s.Logger.Info(ctx, "revenue facts reverted for voided invoice",
			"invoice_id", invoiceID, "revert_rows", n)
	}
	return nil
}

// stableTrueupPriceID is the synthetic price_id for true-up/overage rows —
// stable across recomputes, unlike the engine.s per-compute random id. Falls
// back to the subscription id when there is no line-item id.
func stableTrueupPriceID(subLineItemID, subscriptionID string, isOverage bool) string {
	stableID := subLineItemID
	if stableID == "" {
		stableID = subscriptionID
	}
	prefix := "trueup:"
	if isOverage {
		prefix = "overage:"
	}
	return prefix + stableID
}

func (s *revenueService) getPrice(ctx context.Context, cache map[string]*price.Price, id string) (*price.Price, error) {
	if id == "" {
		return nil, ierr.NewError("price id is required").
			WithHint("cannot hydrate a price with an empty id").
			Mark(ierr.ErrValidation)
	}
	if p, ok := cache[id]; ok {
		return p, nil
	}
	p, err := s.PriceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	cache[id] = p
	return p, nil
}

func (s *revenueService) getMeter(ctx context.Context, cache map[string]*meter.Meter, id string) (*meter.Meter, error) {
	if id == "" {
		return nil, ierr.NewError("meter id is required").
			WithHint("cannot hydrate a meter with an empty id").
			Mark(ierr.ErrValidation)
	}
	if m, ok := cache[id]; ok {
		return m, nil
	}
	m, err := s.MeterRepo.GetMeter(ctx, id)
	if err != nil {
		return nil, err
	}
	cache[id] = m
	return m, nil
}

// rollupInputs is the reference data one rollup pass needs, hydrated once up
// front: prices and meters in one bulk query each, entitlement limits and
// grant-covered meters from one entitlement aggregation, and the customer
// scope for usage reads.
type rollupInputs struct {
	prices            map[string]*price.Price
	meters            map[string]*meter.Meter
	entitlementLimits map[string]decimal.Decimal
	// grantsByMeterID holds the entitlement grants billing each meter — the
	// daily grant curve derives per-day billed vs entitled usage from their
	// quota-crossed windows.
	grantsByMeterID map[string][]*entitlementgrant.EntitlementGrant
	// extCustomerIDs scope usage reads to this subscription's customers
	// (parent + inherited children), matching the engine's own scoping.
	extCustomerIDs []string
}

// price returns the hydrated price for id, erroring on ids the bulk load did
// not find (a dangling line-item reference).
func (in *rollupInputs) price(id string) (*price.Price, error) {
	p, ok := in.prices[id]
	if !ok {
		return nil, ierr.NewErrorf("price %q not found for revenue rollup", id).
			WithHint("line item references a price that does not exist").
			Mark(ierr.ErrNotFound)
	}
	return p, nil
}

// meter returns the hydrated meter for id, erroring like price.
func (in *rollupInputs) meter(id string) (*meter.Meter, error) {
	m, ok := in.meters[id]
	if !ok {
		return nil, ierr.NewErrorf("meter %q not found for revenue rollup", id).
			WithHint("line item references a meter that does not exist").
			Mark(ierr.ErrNotFound)
	}
	return m, nil
}

func (in *rollupInputs) grants(meterID string) []*entitlementgrant.EntitlementGrant {
	return in.grantsByMeterID[meterID]
}

// resolveDiscounts dry-runs coupon application over the preview (nothing is
// persisted, no redemption is counted — same calculator the customer preview
// uses) and returns, per line item: its own coupon discount and its share of
// invoice-level discounts, allocated in proportion to post-line-discount
// amounts so the shares sum exactly to the invoice-level total.
func (s *revenueService) resolveDiscounts(ctx context.Context, invReq *dto.CreateInvoiceRequest) (lineDiscounts, invoiceDiscounts []decimal.Decimal, err error) {
	n := len(invReq.LineItems)
	lineDiscounts = make([]decimal.Decimal, n)
	invoiceDiscounts = make([]decimal.Decimal, n)

	// Discount amounts already present on the preview items count as-is.
	for i := range invReq.LineItems {
		li := &invReq.LineItems[i]
		lineDiscounts[i] = lineDiscounts[i].Add(lo.FromPtr(li.LineItemDiscount))
		invoiceDiscounts[i] = invoiceDiscounts[i].Add(lo.FromPtr(li.InvoiceLevelDiscount))
	}
	if len(invReq.InvoiceCoupons) == 0 && len(invReq.LineItemCoupons) == 0 {
		return lineDiscounts, invoiceDiscounts, nil
	}

	inv, err := invReq.ToInvoice(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(inv.LineItems) != n {
		return nil, nil, ierr.NewError("preview invoice line items diverged from the request").
			WithHint("cannot map coupon discounts back onto preview line items").
			Mark(ierr.ErrSystem)
	}

	result, err := NewCouponApplicationService(s.ServiceParams).CalculateCouponsForInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice:         inv,
		InvoiceCoupons:  invReq.InvoiceCoupons,
		LineItemCoupons: invReq.LineItemCoupons,
	})
	if err != nil {
		return nil, nil, err
	}

	weights := make([]decimal.Decimal, n)
	for i, li := range inv.LineItems {
		lineDiscounts[i] = lineDiscounts[i].Add(li.LineItemDiscount)
		weights[i] = invReq.LineItems[i].Amount.Sub(lineDiscounts[i])
	}
	for i, share := range spreadAmount(result.TotalInvoiceLevelDiscount, weights) {
		invoiceDiscounts[i] = invoiceDiscounts[i].Add(share)
	}
	return lineDiscounts, invoiceDiscounts, nil
}

func (s *revenueService) loadRollupInputs(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time, lineItems []dto.CreateInvoiceLineItemRequest) (*rollupInputs, error) {
	priceIDs := make([]string, 0, len(lineItems))
	meterIDs := make([]string, 0, len(lineItems))
	for i := range lineItems {
		li := &lineItems[i]
		// True-up/overage price ids are generated fresh by the engine on every
		// compute and never persisted to the prices table — fetching them
		// would always miss, and decomposition substitutes a stable synthetic
		// id for them anyway.
		if li.Metadata.GetBool(types.MetadataKeyIsCommitmentTrueup) || li.Metadata.GetBool(types.MetadataKeyIsOverage) {
			continue
		}
		if id := lo.FromPtr(li.PriceID); id != "" {
			priceIDs = append(priceIDs, id)
		}
		if lo.FromPtr(li.PriceType) == string(types.PRICE_TYPE_USAGE) {
			if id := lo.FromPtr(li.MeterID); id != "" {
				meterIDs = append(meterIDs, id)
			}
		}
	}

	prices, err := s.PriceRepo.ListByIDs(ctx, lo.Uniq(priceIDs))
	if err != nil {
		return nil, err
	}
	meters, err := s.MeterRepo.ListByIDs(ctx, lo.Uniq(meterIDs))
	if err != nil {
		return nil, err
	}

	subscriptionService := NewSubscriptionService(s.ServiceParams)
	agg, err := subscriptionService.GetAggregatedSubscriptionEntitlements(ctx, sub.ID, nil)
	if err != nil {
		return nil, err
	}
	extCustomerIDs, err := subscriptionService.ExternalCustomerIDsForSubscription(ctx, sub)
	if err != nil {
		return nil, err
	}

	limits := make(map[string]decimal.Decimal)
	meterByFeatureID := make(map[string]string)
	for _, f := range agg.Features {
		if f.Feature == nil || types.FeatureType(f.Feature.Type) != types.FeatureTypeMetered || f.Feature.MeterID == "" {
			continue
		}
		meterByFeatureID[f.Feature.ID] = f.Feature.MeterID
		if f.Entitlement != nil && f.Entitlement.IsEnabled && f.Entitlement.UsageLimit != nil {
			limits[f.Feature.MeterID] = decimal.NewFromInt(*f.Entitlement.UsageLimit)
		}
	}

	grantsByMeterID, err := s.loadGrantsByMeterID(ctx, sub, periodStart, periodEnd, meterByFeatureID)
	if err != nil {
		return nil, err
	}

	return &rollupInputs{
		prices:            lo.KeyBy(prices, func(p *price.Price) string { return p.ID }),
		meters:            lo.KeyBy(meters, func(m *meter.Meter) string { return m.ID }),
		entitlementLimits: limits,
		grantsByMeterID:   grantsByMeterID,
		extCustomerIDs:    extCustomerIDs,
	}, nil
}

// loadGrantMeterIDs returns the meters covered by feature-scoped entitlement
// grants in this billing cycle — mirroring loadEntitlementGrantsByMeterID's
// scoping in the billing engine.
func (s *revenueService) loadGrantsByMeterID(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time, meterByFeatureID map[string]string) (map[string][]*entitlementgrant.EntitlementGrant, error) {
	if s.EntitlementGrantRepo == nil {
		return nil, nil
	}

	filter := types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(sub.CustomerID).
		WithSubscriptionIDs(sub.ID).
		WithCycleOverlap(periodStart, periodEnd)
	grants, err := s.EntitlementGrantRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	out := make(map[string][]*entitlementgrant.EntitlementGrant)
	for _, g := range grants {
		if g == nil || !g.IsFeatureScoped() {
			continue
		}
		if meterID := meterByFeatureID[g.ScopeEntityID]; meterID != "" {
			out[meterID] = append(out[meterID], g)
		}
	}
	return out, nil
}

// hasOverageLine reports whether any line item is the engine's synthetic
// overage charge (metadata-flagged).
func hasOverageLine(invReq *dto.CreateInvoiceRequest) bool {
	for _, li := range invReq.LineItems {
		if li.Metadata.GetBool(types.MetadataKeyIsOverage) {
			return true
		}
	}
	return false
}
