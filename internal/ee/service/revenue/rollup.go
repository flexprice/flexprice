package revenue

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
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

// Service is the revenue_facts service: a shadow write-path that re-runs the
// billing preview, never mutates billing state, and logs reconciliation
// mismatches instead of blocking on them. The interface lives in
// internal/interfaces so the service layer's invoice hooks can call it
// without importing this package.
type Service = interfaces.RevenueService

type revenueService struct {
	service.ServiceParams
}

// New returns the revenue_facts service.
func New(params service.ServiceParams) Service {
	return &revenueService{ServiceParams: params}
}

// lineItemRows keeps one line item's rows together for reconciliation logging.
type lineItemRows struct {
	rows       []*revenuefact.RevenueFact
	amount     decimal.Decimal
	identifier string
}

func (s *revenueService) RollupSubscription(ctx context.Context, subscriptionID string) error {
	enabled, err := s.revenueAnalyticsEnabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}

	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return err
	}
	_, err = s.rollupSubscription(ctx, sub)
	return err
}

// rollupSubscription also reports policy skips, which RollupDirty tallies but
// the interface method does not expose.
func (s *revenueService) rollupSubscription(ctx context.Context, sub *subscription.Subscription) (skipped bool, err error) {
	return s.rollupSubscriptionForPeriod(ctx, sub, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
}

// rollupSubscriptionForPeriod rolls one explicit billing window (periodEnd
// exclusive) into PROVISIONAL revenue_facts rows, in five steps:
//
//  1. Guard: multi-period commitments are skipped whole (ERD Q3).
//  2. Preview: re-run the billing engine for the window under
//     ReferencePointRevenueFacts (includes the coupon dry-run).
//  3. Decompose: each preview line item becomes rows by its kind — true-up,
//     overage (decomposeOverageRows), fixed, or usage (decomposeUsageRows).
//  4. Reconcile (shadow-only): row/line-item/invoice sums are checked against
//     the engine's own amounts; mismatches are logged, never block.
//  5. Upsert: rows land on the provisional grain, idempotently.
func (s *revenueService) rollupSubscriptionForPeriod(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time) (skipped bool, err error) {
	lineItems, err := s.SubscriptionLineItemRepo.ListBySubscription(ctx, sub)
	if err != nil {
		return false, err
	}
	subscriptionID := sub.ID
	sub.LineItems = lineItems

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

	billingSvc := service.NewBillingService(s.ServiceParams)
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

	// When usage exceeded the commitment, the engine emitted each usage line
	// as a pair sharing one sub_line_item_id: a "within commitment" line and
	// an "overage" line. Record each pair's overage amount so both halves can
	// be split per day around the commitment boundary.
	overageAmountBySLI := map[string]decimal.Decimal{}
	for i := range invReq.LineItems {
		li := &invReq.LineItems[i]
		if li.Metadata.GetBool(types.MetadataKeyIsOverage) && lo.FromPtr(li.PriceType) == string(types.PRICE_TYPE_USAGE) {
			if sli := lo.FromPtr(li.SubscriptionLineItemID); sli != "" {
				overageAmountBySLI[sli] = li.Amount
			}
		}
	}
	// Daily curves for overage lines, produced while their normal sibling
	// decomposes; a nil entry means the pair fell back to whole-period rows.
	overageCurves := map[string][]dayCharge{}

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
		// The engine's line item period end is half-open/exclusive; rows are
		// keyed on the inclusive last day it covers.
		itemPeriod := periodDays(itemPeriodStart, itemPeriodEndExclusive, locationOf(sub.Timezone))

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
			LineDiscount:    lo.FromPtr(item.LineItemDiscount),
			InvoiceDiscount: lo.FromPtr(item.InvoiceLevelDiscount),
			EntitlementQty:  lo.FromPtr(item.AdjustedEntitlementQuantity),
			PeriodStart:     itemPeriod.Start,
			PeriodEnd:       itemPeriod.End,
			Timezone:        sub.Timezone,
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
		case isTrueup:
			// The engine assigns true-up lines a fresh random price_id every
			// compute; use a stable synthetic id so recomputes update in place.
			base.Price = &price.Price{ID: stableTrueupPriceID(base.SubLineItemID, itemSubscriptionID, false)}
			row := decomposeCommitmentTrueup(base, itemPeriod)
			allRows = append(allRows, row)
			groups = append(groups, lineItemRows{rows: []*revenuefact.RevenueFact{row}, amount: netAmount, identifier: base.Price.ID})

		case isOverage:
			// Same stable synthetic id story as true-up lines.
			base.Price = &price.Price{ID: stableTrueupPriceID(base.SubLineItemID, itemSubscriptionID, true)}
			base.Source = types.RevenueSourceOverage
			rows := s.decomposeOverageRows(ctx, sub, inputs, base, item, itemPeriod, overageCurves)
			allRows = append(allRows, rows...)
			groups = append(groups, lineItemRows{rows: rows, amount: netAmount, identifier: base.Price.ID})

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
			rows, usageErr := s.decomposeUsageRows(ctx, sub, inputs, base, item, itemPeriod, overageAmountBySLI, overageCurves)
			if usageErr != nil {
				return false, usageErr
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

	s.logReconciliationMismatches(ctx, subscriptionID, groups, allRows, invReq)

	if len(allRows) == 0 {
		return false, nil
	}

	// Reconciliation above ran on every row. Only the write is narrowed: a
	// nightly pass recomputes days that are already stored and identical, and
	// rewriting them is most of the write volume.
	stored, err := s.RevenueFactRepo.ListBySubscriptionPeriod(ctx, subscriptionID, periodStart, periodEnd, types.FactProvisional)
	if err != nil {
		return false, err
	}
	toWrite := changedRows(allRows, stored)

	// The export contract says absence of rows means "not enabled or not yet
	// computed — never that revenue was zero". A period whose every row
	// carries nothing would otherwise vanish entirely and read as uncomputed,
	// so keep one row as the record that it was. One row per subscription per
	// period, only when nothing is stored for it yet.
	if len(toWrite) == 0 && len(stored) == 0 && len(allRows) > 0 {
		toWrite = allRows[:1]
	}
	if len(toWrite) == 0 {
		return false, nil
	}

	// Every row is PROVISIONAL with a non-empty price_id by construction;
	// re-running upserts in place instead of duplicating.
	if err := s.RevenueFactRepo.UpsertProvisional(ctx, toWrite); err != nil {
		return false, err
	}

	return false, nil
}

// logReconciliationMismatches checks the freshly decomposed rows against the
// engine's own amounts at every grain — per row, per line item, and for the
// whole preview. Shadow-only: a mismatch is logged/metriced, never blocks the
// write, and billing itself is never touched.
func (s *revenueService) logReconciliationMismatches(ctx context.Context, subscriptionID string, groups []lineItemRows, allRows []*revenuefact.RevenueFact, invReq *dto.CreateInvoiceRequest) {
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
	for i := range invReq.LineItems {
		li := &invReq.LineItems[i]
		totalDiscount = totalDiscount.Add(lo.FromPtr(li.LineItemDiscount)).Add(lo.FromPtr(li.InvoiceLevelDiscount))
	}
	if residual, ok := reconcileInvoice(allRows, invReq.Subtotal.Sub(totalDiscount)); !ok {
		s.Logger.Info(ctx, "revenue_reconciliation_mismatch",
			"scope", "invoice", "subscription_id", subscriptionID, "residual", residual.String())
	}
}

// decomposeUsageRows turns one usage line item into revenue_facts rows,
// picking one of four ways to place its charge on days:
//
//  1. Line-level commitment: the engine folded the commitment's true-up or
//     overage INTO this line's own amount (no sibling line) and left the
//     breakdown on CommitmentInfo — split the parts out first.
//  2. Entitlement-grant billing: the engine charged only the usage that fell
//     inside the grants' quota-crossed windows. Split per day along those
//     windows — days before any quota was crossed show entitled usage with
//     zero net. Falls to (4) when the windows carry no usage to shape by.
//  3. This line has an overage sibling (usage exceeded the subscription-level
//     commitment, so the engine emitted a second line for the excess): split
//     the daily curve at the commitment boundary — days until this line's
//     amount is reached are billed here, the excess accrues on the sibling's
//     curve, which is stashed in overageCurves for its own loop iteration.
//  4. Otherwise decompositionMode decides: one row per day when daily deltas
//     are meaningful for this price/meter, one whole-period row when not.
//
// Whole-period is always the last resort: it also catches (2) and (3) when no
// daily shape can be derived, marking the pair's stash nil so the sibling
// falls back the same way.
func (s *revenueService) decomposeUsageRows(
	ctx context.Context,
	sub *subscription.Subscription,
	inputs *rollupInputs,
	base previewLineItem,
	item *dto.CreateInvoiceLineItemRequest,
	itemPeriod revenuePeriod,
	overageAmountBySLI map[string]decimal.Decimal,
	overageCurves map[string][]dayCharge,
) ([]*revenuefact.RevenueFact, error) {
	// A missing price or meter is a hard error — never let
	// decompositionMode(nil, nil) silently default to Marginal.
	p, err := inputs.price(lo.FromPtr(item.PriceID))
	if err != nil {
		return nil, err
	}
	m, err := inputs.meter(lo.FromPtr(item.MeterID))
	if err != nil {
		return nil, err
	}
	base.Price = p
	base.Meter = m

	wholePeriod := func() []*revenuefact.RevenueFact {
		return []*revenuefact.RevenueFact{decomposeUsagePeriodOnly(base, itemPeriod)}
	}

	if rows, handled, err := s.decomposeLineCommitmentRows(ctx, sub, inputs, base, item, itemPeriod, p, m); handled || err != nil {
		return rows, err
	}

	if overageAmount, paired := overageAmountBySLI[base.SubLineItemID]; paired {
		grantBilled := grantsBillable(subLineItemByID(sub, base.SubLineItemID), p, m, inputs.grants(m.ID))
		// The commitment split walks a cumulative curve; a bucketed line has
		// no such curve, so its pair stays whole-period.
		if grantBilled || price.IsBucketed(p, m) || decompositionMode(p, m) != types.Marginal {
			// No daily shape for this pair — both halves stay whole-period.
			overageCurves[base.SubLineItemID] = nil
			return wholePeriod(), nil
		}
		curve, err := s.buildUsageCurve(ctx, usageCurveInput{
			Price:               p,
			MeterID:             m.ID,
			PeriodStart:         itemPeriod.Start,
			PeriodEnd:           itemPeriod.exclusiveEnd(),
			EntitlementLimit:    inputs.entitlementLimits[m.ID],
			ExternalCustomerIDs: inputs.extCustomerIDs,
			Timezone:            sub.Timezone,
			Usage:               inputs.usage(m.ID),
		})
		if err != nil {
			return nil, err
		}
		if len(curve) == 0 {
			overageCurves[base.SubLineItemID] = nil
			return wholePeriod(), nil
		}
		normalQty := quantityAtCharge(ctx, service.NewPriceService(s.ServiceParams), p, item.Amount, curve[len(curve)-1].CumulativeBillableQty)
		normalCurve, overageCurve := splitCurveAtCommitment(curve, item.Amount, overageAmount, listRate(p), normalQty)
		overageCurves[base.SubLineItemID] = overageCurve
		return decomposeUsageMarginal(base, normalCurve), nil
	}

	if grants := inputs.grants(m.ID); grantsBillable(subLineItemByID(sub, base.SubLineItemID), p, m, grants) {
		curve, shapeKnown, err := s.buildGrantOverageCurve(ctx, grantCurveInput{
			Price:               p,
			Meter:               m,
			PeriodStart:         itemPeriod.Start,
			PeriodEnd:           itemPeriod.exclusiveEnd(),
			EngineAmount:        item.Amount,
			Grants:              grants,
			ExternalCustomerIDs: inputs.extCustomerIDs,
			Timezone:            sub.Timezone,
			Usage:               inputs.usage(m.ID),
		})
		if err != nil {
			return nil, err
		}
		if !shapeKnown {
			return wholePeriod(), nil
		}
		return decomposeUsageMarginal(base, curve), nil
	}

	if decompositionMode(p, m) != types.Marginal {
		return wholePeriod(), nil
	}

	// Bucketed lines are priced window by window rather than off a cumulative
	// curve. An entitlement limit is consumed across the whole period, which
	// that per-window shape does not model, so those stay whole-period.
	if price.IsBucketed(p, m) {
		if inputs.entitlementLimits[m.ID].IsPositive() {
			return wholePeriod(), nil
		}
		curve, shapeKnown, bErr := s.buildBucketedCurve(ctx, bucketedCurveInput{
			Price:               p,
			Meter:               m,
			Sub:                 sub,
			PeriodStart:         itemPeriod.Start,
			PeriodEnd:           itemPeriod.exclusiveEnd(),
			EngineAmount:        item.Amount,
			ExternalCustomerIDs: inputs.extCustomerIDs,
			Timezone:            sub.Timezone,
		})
		if bErr != nil {
			return nil, bErr
		}
		if !shapeKnown {
			return wholePeriod(), nil
		}
		return decomposeUsageMarginal(base, curve), nil
	}

	curve, err := s.buildUsageCurve(ctx, usageCurveInput{
		Price:       p,
		MeterID:     m.ID,
		PeriodStart: itemPeriod.Start,
		// buildUsageCurve's upper bound is exclusive — convert back from the inclusive day.
		PeriodEnd:           itemPeriod.exclusiveEnd(),
		EntitlementLimit:    inputs.entitlementLimits[m.ID],
		ExternalCustomerIDs: inputs.extCustomerIDs,
		Timezone:            sub.Timezone,
		Usage:               inputs.usage(m.ID),
	})
	if err != nil {
		return nil, err
	}
	return decomposeUsageMarginal(base, curve), nil
}

// decomposeLineCommitmentRows handles a usage line whose LINE-LEVEL commitment
// adjusted its amount in place: the engine bills max(usage, commitment) plus a
// factored overage on the same line and reports the utilized/true-up/overage
// breakdown on CommitmentInfo. handled=false means the line carries no such
// adjustment and the caller's normal paths apply.
//
// The parts become rows sharing the line's real price but distinct sources —
// the within-commitment usage (per day when a curve exists), a period_only
// commitment_trueup row, and per-day overage rows split at the commitment
// boundary. Discounts stay on the usage part only, so netting once holds:
// usage + trueup + overage − discounts == the line's net amount.
func (s *revenueService) decomposeLineCommitmentRows(
	ctx context.Context,
	sub *subscription.Subscription,
	inputs *rollupInputs,
	base previewLineItem,
	item *dto.CreateInvoiceLineItemRequest,
	itemPeriod revenuePeriod,
	p *price.Price,
	m *meter.Meter,
) (rows []*revenuefact.RevenueFact, handled bool, err error) {
	info := item.CommitmentInfo
	if info == nil {
		return nil, false, nil
	}
	trueUp, overage := info.ComputedTrueUpAmount, info.ComputedOverageAmount
	if !trueUp.IsPositive() && !overage.IsPositive() {
		return nil, false, nil
	}
	within := item.Amount.Sub(trueUp).Sub(overage)

	// Only the usage part keeps the line's discounts; the extra rows carry
	// zero so nothing double-counts against the line's net amount.
	partBase := base
	partBase.LineDiscount, partBase.InvoiceDiscount = decimal.Zero, decimal.Zero
	usageBase := base
	usageBase.EngineAmount = within

	// A windowed commitment settles each bucket on its own, so when those
	// buckets nest inside days every part is datable: committed usage, overage
	// and true-up each land on the day their window covers.
	if info.IsWindowed && bucketedDayGrain(p, m) &&
		!grantsBillable(subLineItemByID(sub, base.SubLineItemID), p, m, inputs.grants(m.ID)) &&
		!inputs.entitlementLimits[m.ID].IsPositive() {
		perDay, ok, bErr := s.decomposeBucketedCommitmentRows(ctx, sub, base, item, itemPeriod, p, m, info, inputs.extCustomerIDs)
		if bErr != nil {
			return nil, true, bErr
		}
		if ok {
			return perDay, true, nil
		}
	}

	// Otherwise the parts stay whole-period: a windowed commitment has no
	// single boundary to split at, and a bucketed line has no cumulative curve
	// to walk.
	marginal := !info.IsWindowed && !price.IsBucketed(p, m) &&
		decompositionMode(p, m) == types.Marginal &&
		!grantsBillable(subLineItemByID(sub, base.SubLineItemID), p, m, inputs.grants(m.ID))
	if marginal {
		curve, curveErr := s.buildUsageCurve(ctx, usageCurveInput{
			Price:               p,
			MeterID:             m.ID,
			PeriodStart:         itemPeriod.Start,
			PeriodEnd:           itemPeriod.exclusiveEnd(),
			EntitlementLimit:    inputs.entitlementLimits[m.ID],
			ExternalCustomerIDs: inputs.extCustomerIDs,
			Timezone:            sub.Timezone,
			Usage:               inputs.usage(m.ID),
		})
		if curveErr != nil {
			return nil, true, curveErr
		}
		switch {
		case len(curve) == 0 || curve[len(curve)-1].CumulativeGrossQty.IsZero():
			// No usage at all (the curve walks the period even without
			// events) — fall through to the whole-period parts, where a
			// zero within-commitment amount books no usage rows.
		case overage.IsPositive():
			normalQty := quantityAtCharge(ctx, service.NewPriceService(s.ServiceParams), p, within, curve[len(curve)-1].CumulativeBillableQty)
			normalCurve, overageCurve := splitCurveAtCommitment(curve, within, overage, listRate(p), normalQty)
			ob := partBase
			ob.EngineAmount = overage
			ob.Source = types.RevenueSourceOverage
			rows = append(rows, decomposeUsageMarginal(usageBase, normalCurve)...)
			rows = append(rows, decomposeUsageMarginal(ob, overageCurve)...)
		default:
			rows = append(rows, decomposeUsageMarginal(usageBase, curve)...)
		}
	}

	// No daily shape (windowed, non-marginal mode, grants, or no usage at
	// all): the parts stay whole-period. A zero within-commitment amount
	// (true-up with no usage) books no usage row at all.
	if len(rows) == 0 {
		if !within.IsZero() || overage.IsPositive() {
			rows = append(rows, decomposeUsagePeriodOnly(usageBase, itemPeriod))
		}
		if overage.IsPositive() {
			ob := partBase
			ob.EngineAmount = overage
			rows = append(rows, decomposeOverage(ob, itemPeriod))
		}
	}

	if trueUp.IsPositive() {
		tb := partBase
		if len(rows) == 0 {
			// No usage row exists to carry the line's discounts — the
			// true-up row is the whole line, so it keeps them.
			tb = base
		}
		tb.EngineAmount = trueUp
		rows = append(rows, decomposeCommitmentTrueup(tb, itemPeriod))
	}
	return rows, true, nil
}

// decomposeOverageRows turns one overage line into revenue_facts rows: the
// daily curve its normal sibling stashed, its own curve when the whole line
// is overage (commitment already exhausted), or one whole-period row when no
// daily shape exists.
func (s *revenueService) decomposeOverageRows(
	ctx context.Context,
	sub *subscription.Subscription,
	inputs *rollupInputs,
	base previewLineItem,
	item *dto.CreateInvoiceLineItemRequest,
	itemPeriod revenuePeriod,
	overageCurves map[string][]dayCharge,
) []*revenuefact.RevenueFact {
	// The row id is synthetic (stableTrueupPriceID) but the meter is real —
	// hydrate it so overage rows persist meter_id and aggregation type.
	if m, ok := inputs.meters[lo.FromPtr(item.MeterID)]; ok {
		base.Meter = m
	}
	if curve, paired := overageCurves[base.SubLineItemID]; paired {
		if curve == nil {
			return []*revenuefact.RevenueFact{decomposeOverage(base, itemPeriod)}
		}
		return decomposeUsageMarginal(base, curve)
	}

	// No normal sibling: the commitment was already exhausted, so this line
	// carries all of the item's usage as overage. Split its own curve with a
	// zero within-commitment amount; the line's real price/meter (unlike its
	// synthetic row id) drives the curve.
	p, okP := inputs.prices[lo.FromPtr(item.PriceID)]
	m, okM := inputs.meters[lo.FromPtr(item.MeterID)]
	if !okP || !okM || decompositionMode(p, m) != types.Marginal ||
		grantsBillable(subLineItemByID(sub, base.SubLineItemID), p, m, inputs.grants(m.ID)) {
		return []*revenuefact.RevenueFact{decomposeOverage(base, itemPeriod)}
	}

	// A bucketed line has no cumulative curve to split: each window is charged
	// on its own quantity. The whole line is overage here, so the per-window
	// curve IS the overage curve. An entitlement limit is consumed across the
	// period, which that per-window shape does not model.
	if price.IsBucketed(p, m) {
		if inputs.entitlementLimits[m.ID].IsPositive() {
			return []*revenuefact.RevenueFact{decomposeOverage(base, itemPeriod)}
		}
		curve, shapeKnown, bErr := s.buildBucketedCurve(ctx, bucketedCurveInput{
			Price:               p,
			Meter:               m,
			Sub:                 sub,
			PeriodStart:         itemPeriod.Start,
			PeriodEnd:           itemPeriod.exclusiveEnd(),
			EngineAmount:        item.Amount,
			ExternalCustomerIDs: inputs.extCustomerIDs,
			Timezone:            sub.Timezone,
		})
		if bErr != nil {
			s.Logger.Error(ctx, "bucketed overage curve failed, falling back to whole-period row",
				"error", bErr, "subscription_id", sub.ID, "sub_line_item_id", base.SubLineItemID)
			return []*revenuefact.RevenueFact{decomposeOverage(base, itemPeriod)}
		}
		if !shapeKnown {
			return []*revenuefact.RevenueFact{decomposeOverage(base, itemPeriod)}
		}
		return decomposeUsageMarginal(base, curve)
	}

	curve, err := s.buildUsageCurve(ctx, usageCurveInput{
		Price:               p,
		MeterID:             m.ID,
		PeriodStart:         itemPeriod.Start,
		PeriodEnd:           itemPeriod.exclusiveEnd(),
		EntitlementLimit:    inputs.entitlementLimits[m.ID],
		ExternalCustomerIDs: inputs.extCustomerIDs,
		Timezone:            sub.Timezone,
		Usage:               inputs.usage(m.ID),
	})
	if err != nil {
		// Shadow path: a curve failure downgrades to a whole-period row
		// rather than failing the line.
		s.Logger.Error(ctx, "overage curve failed, falling back to whole-period row",
			"error", err, "subscription_id", sub.ID, "sub_line_item_id", base.SubLineItemID)
		return []*revenuefact.RevenueFact{decomposeOverage(base, itemPeriod)}
	}
	_, overageCurve := splitCurveAtCommitment(curve, decimal.Zero, item.Amount, listRate(p), decimal.Zero)
	return decomposeUsageMarginal(base, overageCurve)
}

func (s *revenueService) RollupDirty(ctx context.Context, req types.RollupDirtyRequest) (types.RollupDirtyResult, error) {
	var result types.RollupDirtyResult

	// Environments are walked in a stable order, so a cursor naming one of them
	// means "this one, partially, then the rest". A cursor whose environment is
	// gone -- disabled, deleted, or its setting removed -- is ignored rather
	// than obeyed: skipping until a match that never comes would walk the whole
	// list, roll nothing, and report success.
	resuming := req.Cursor != nil && s.environmentIsOptedIn(ctx, req.Cursor.EnvironmentID)
	if req.Cursor != nil && !resuming {
		s.Logger.Info(ctx, "revenue rollup ignoring a stale cursor",
			"environment_id", req.Cursor.EnvironmentID)
	}

	// Once an environment fails, the cursor stops advancing. Letting a later
	// environment record its progress would make the retry skip the failed one.
	cursorFrozen := false
	setCursor := func(c types.RollupCursor) {
		if cursorFrozen {
			return
		}
		result.Cursor = &c
		if req.OnProgress != nil {
			req.OnProgress(c)
		}
	}

	err := s.forEachOptedInEnvironment(ctx, "revenue rollup dirty scan", func(envCtx context.Context) error {
		envID := types.GetEnvironmentID(envCtx)
		var after string
		if resuming {
			if req.Cursor.EnvironmentID != envID {
				// Environments before the cursor's are already done.
				return nil
			}
			after = req.Cursor.LastSubscriptionID
			resuming = false
		}

		envRolled, envSkipped, envErr := s.rollupDirtyForEnvironment(envCtx, req, after, setCursor)
		result.Rolled += envRolled
		result.Skipped += envSkipped
		if envErr != nil {
			cursorFrozen = true
		}
		return envErr
	})
	return result, err
}

// environmentIsOptedIn reports whether a cursor's environment is still one the
// rollup walks. Anything it cannot confirm is treated as gone, so the scan
// restarts from the top rather than skipping every environment.
func (s *revenueService) environmentIsOptedIn(ctx context.Context, environmentID string) bool {
	if environmentID == "" {
		return false
	}
	configs, err := s.SettingsRepo.ListAllTenantEnvSettingsByKey(ctx, types.SettingKeyRevenueAnalyticsConfig)
	if err != nil {
		return false
	}
	for _, tec := range configs {
		if tec.EnvironmentID != environmentID {
			continue
		}
		cfg, cfgErr := utils.ToStruct[types.RevenueAnalyticsConfig](tec.Config)
		if cfgErr == nil && cfg.Enabled {
			return true
		}
	}
	return false
}

// forEachOptedInEnvironment runs fn once per (tenant, environment) that opted
// in via revenue_analytics_config, with tenant/environment set on ctx — never
// a scan across all tenants. One environment's failure never blocks the
// others, but the combined error is returned so the Temporal activity fails
// (and retries) instead of reporting success with partial counters.
func (s *revenueService) forEachOptedInEnvironment(ctx context.Context, op string, fn func(ctx context.Context) error) error {
	tenantEnvConfigs, err := s.SettingsRepo.ListAllTenantEnvSettingsByKey(ctx, types.SettingKeyRevenueAnalyticsConfig)
	if err != nil {
		return err
	}

	// Stable order, so an environment cursor means the same thing on a retry.
	sort.Slice(tenantEnvConfigs, func(i, j int) bool {
		if tenantEnvConfigs[i].TenantID != tenantEnvConfigs[j].TenantID {
			return tenantEnvConfigs[i].TenantID < tenantEnvConfigs[j].TenantID
		}
		return tenantEnvConfigs[i].EnvironmentID < tenantEnvConfigs[j].EnvironmentID
	})

	var envErrs []error
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
		if tec.TenantID == "" || tec.EnvironmentID == "" {
			s.Logger.Info(ctx, "skipping revenue_analytics_config without tenant or environment",
				"tenant_id", tec.TenantID,
				"environment_id", tec.EnvironmentID)
			continue
		}

		tenantCtx := types.SetTenantID(ctx, tec.TenantID)
		tenantCtx = types.SetEnvironmentID(tenantCtx, tec.EnvironmentID)

		if envErr := fn(tenantCtx); envErr != nil {
			s.Logger.Error(ctx, op+" failed for environment",
				"error", envErr,
				"tenant_id", tec.TenantID,
				"environment_id", tec.EnvironmentID)
			envErrs = append(envErrs, envErr)
		}
	}
	return errors.Join(envErrs...)
}

// scanScope decides which subscriptions a pass must roll. Over-scoping costs a
// read and a diff that writes nothing; under-scoping leaves facts silently
// stale, so every uncertain answer widens the scope rather than narrowing it.
type scanScope struct {
	full bool
	// customers with usage ingested since the window opened
	customers map[string]struct{}
	// parent subscriptions whose inherited children saw usage
	activeParents map[string]struct{}
	// subscriptions whose committed minimum accrues without usage
	accruing map[string]struct{}
	since    time.Time
	// now anchors the period-open grace window
	now time.Time
}

// periodOpenGrace keeps a freshly opened period in scope until the next
// scheduled full rebuild. The opening roll is what writes the fixed charges and
// commitment true-ups a subscription owes before anything is metered, and if it
// fails the period-start trigger has already passed by the next run — the
// window would close on a subscription that never got its rows.
//
// Deliberately a clock window rather than a per-subscription probe of what is
// already written. That probe is a group-by over every provisional fact, and
// revenue_facts holds a row per line item per day: at production shape that is
// millions of rows scanned on every run, growing with the table. Spanning the
// rebuild interval instead means a failed opening roll is retried on every run
// until the rebuild would have caught it anyway, so the two never leave a gap
// between them.
const periodOpenGrace = 7 * 24 * time.Hour

// backdateLookback bounds how far back a late-arriving event is looked for.
// meter_usage is partitioned on the event timestamp, so an unbounded probe
// reads every partition the tenant has ever written. Events backdated further
// than this are picked up by the scheduled full rebuild.
const backdateLookback = 90 * 24 * time.Hour

// includes reports whether this subscription has to be rolled. Usage is only
// one of the triggers: a subscription with no usage at all still owes fixed
// charges, commitment true-ups and per-window bucketed true-ups, and those are
// written when its period opens.
func (sc scanScope) includes(sub *subscription.Subscription) bool {
	if sc.full {
		return true
	}
	if _, ok := sc.customers[sub.CustomerID]; ok {
		return true
	}
	// Usage on an inherited child bills the parent, and the child's usage rows
	// carry the child's customer id — the parent would otherwise look quiet.
	if _, ok := sc.activeParents[sub.ID]; ok {
		return true
	}
	// A windowed commitment's true-up fills empty windows, and the bucketed
	// curve is clamped to today, so one more window becomes billable every day
	// with no usage at all. Every other trigger reads such a subscription as
	// quiet, and its accrual would stop after the period's opening roll.
	if _, ok := sc.accruing[sub.ID]; ok {
		return true
	}
	if !sub.UpdatedAt.Before(sc.since) {
		return true
	}
	// A period that opened inside the window needs its opening rows even
	// though nothing has been metered against it yet.
	// It stays in scope for a few runs, so a failed opening roll gets another
	// chance: by the next run the period start is already outside the window.
	return !sub.CurrentPeriodStart.Before(sc.now.Add(-periodOpenGrace))
}

// catalogChangedSince reports whether any price in this environment was edited
// since the window opened. It is deliberately coarse — one edited price widens
// the whole environment to a full pass — because the alternative is resolving
// which subscriptions reference it, and a full pass is now cheap enough that
// the precision is not worth the query.
//
// Coupon and entitlement edits are not covered here and are repaired by the
// scheduled full rebuild instead.
func (s *revenueService) catalogChangedSince(ctx context.Context, since time.Time) bool {
	filter := types.NewNoLimitPriceFilter()
	filter.UpdatedAfter = lo.ToPtr(since)
	filter.AllowExpiredPrices = true
	filter.Limit = lo.ToPtr(1)

	prices, err := s.PriceRepo.List(ctx, filter)
	if err != nil {
		s.Logger.Info(ctx, "revenue rollup treating the catalog as changed",
			"error", err.Error(), "reason", "price change probe failed")
		return true
	}
	if len(prices) > 0 {
		s.Logger.Info(ctx, "revenue rollup widening to a full scan", "reason", "price edited")
		return true
	}
	return false
}

// scanScopeFor resolves the scope for one environment. Anything it cannot
// answer confidently resolves to a full pass.
func (s *revenueService) scanScopeFor(ctx context.Context, req types.RollupDirtyRequest) scanScope {
	full := scanScope{full: true, since: req.Since}
	if req.ForceFull {
		return full
	}

	// A price edit changes the amount on every subscription using it, but bumps
	// nothing on the subscription itself, so usage and updated_at both miss it.
	if s.catalogChangedSince(ctx, req.Since) {
		return full
	}

	activity, err := s.MeterUsageRepo.GetUsageActivitySince(ctx, &events.UsageActivityParams{
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		IngestedAfter: req.Since,
		// Backdating beyond this is repaired by the scheduled full rebuild;
		// without the bound the read scans every partition ever written.
		TimestampAfter: req.Since.Add(-backdateLookback),
	})
	if err != nil {
		s.Logger.Info(ctx, "revenue rollup falling back to a full scan",
			"error", err.Error(), "reason", "usage activity read failed")
		return full
	}
	if activity.Unattributed {
		s.Logger.Info(ctx, "revenue rollup falling back to a full scan",
			"reason", "usage with no customer id")
		return full
	}

	// meter_usage keys on the external customer id; subscriptions key on the
	// internal one, so resolve. Bounded by the customers that actually saw
	// usage, not by the customer table.
	customers, err := s.internalCustomerIDs(ctx, activity.ExternalCustomerIDs)
	if err != nil {
		s.Logger.Info(ctx, "revenue rollup falling back to a full scan",
			"error", err.Error(), "reason", "customer id resolution failed")
		return full
	}

	activeParents, err := s.parentsOfActiveChildren(ctx, customers)
	if err != nil {
		s.Logger.Info(ctx, "revenue rollup falling back to a full scan",
			"error", err.Error(), "reason", "inherited-child lookup failed")
		return full
	}

	accruing, err := s.subscriptionsWithAccruingCommitments(ctx)
	if err != nil {
		s.Logger.Info(ctx, "revenue rollup falling back to a full scan",
			"error", err.Error(), "reason", "commitment true-up lookup failed")
		return full
	}

	return scanScope{
		customers:     customers,
		activeParents: activeParents,
		accruing:      accruing,
		since:         req.Since,
		now:           time.Now().UTC(),
	}
}

// subscriptionsWithAccruingCommitments returns the subscriptions whose
// committed minimum is billable without usage: a windowed commitment's true-up
// fills empty windows, and the bucketed curve is clamped to today, so one more
// window becomes billable each day. They must be rolled every pass rather than
// once when the period opens.
//
// Keyed on subscription rather than customer so a customer's other
// subscriptions are not dragged in, and predicated on a plain boolean so the
// scan does not evaluate jsonb per row -- this table runs to millions of rows
// per environment.
func (s *revenueService) subscriptionsWithAccruingCommitments(ctx context.Context) (map[string]struct{}, error) {
	if s.SubscriptionLineItemRepo == nil {
		return nil, nil
	}
	ids, err := s.SubscriptionLineItemRepo.SubscriptionIDsWithWindowedCommitment(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out, nil
}

// internalCustomerIDs resolves external customer ids to internal ones.
// meter_usage stores external_customer_id and has no internal customer_id
// column, so the scan cannot compare its result against subscriptions without
// this step.
func (s *revenueService) internalCustomerIDs(ctx context.Context, externalIDs []string) (map[string]struct{}, error) {
	if len(externalIDs) == 0 {
		return map[string]struct{}{}, nil
	}

	filter := types.NewNoLimitCustomerFilter()
	filter.ExternalIDs = externalIDs
	customers, err := s.CustomerRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	out := make(map[string]struct{}, len(customers))
	for _, c := range customers {
		out[c.ID] = struct{}{}
	}
	return out, nil
}

// parentsOfActiveChildren maps the parent subscriptions of inherited children
// whose customers saw usage. A parent subscription bills its children's usage,
// but those usage rows carry the child's customer id, so matching on the
// parent's own customer alone would leave it looking quiet.
func (s *revenueService) parentsOfActiveChildren(ctx context.Context, customers map[string]struct{}) (map[string]struct{}, error) {
	if len(customers) == 0 {
		return nil, nil
	}

	// Scoped to the customers that actually saw usage: listing every inherited
	// subscription in the environment would be unbounded, and all but a handful
	// of them could not qualify anyway.
	filter := types.NewNoLimitSubscriptionFilter()
	filter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeInherited}
	filter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusActive}
	filter.CustomerIDs = lo.Keys(customers)
	children, err := s.SubRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	parents := map[string]struct{}{}
	for _, child := range children {
		parentID := lo.FromPtr(child.ParentSubscriptionID)
		if parentID == "" {
			continue
		}
		parents[parentID] = struct{}{}
	}
	return parents, nil
}

// rollupDirtyForEnvironment scans one (tenant, environment)'s active
// subscriptions in pages and rolls every one with activity since `since`.
func (s *revenueService) rollupDirtyForEnvironment(ctx context.Context, req types.RollupDirtyRequest, after string, setCursor func(types.RollupCursor)) (rolled, skipped int, err error) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	if tenantID == "" || environmentID == "" {
		return 0, 0, nil
	}

	scope := s.scanScopeFor(ctx, req)

	const batchSize = 1000
	offset := 0
	maxSeen := after

	for {
		// Listing pages by offset under a stable id sort; the cursor is a
		// resume filter applied to what comes back, not a pagination key. That
		// keeps a retry from redoing completed subscriptions without needing
		// the repository to support keyset paging.
		filter := types.NewSubscriptionFilter()
		filter.Limit = lo.ToPtr(batchSize)
		filter.Offset = lo.ToPtr(offset)
		filter.Status = lo.ToPtr(types.StatusPublished)
		filter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusActive}
		filter.Sort = []*types.SortCondition{{Field: "id", Direction: types.SortDirectionAsc}}
		page, listErr := s.SubRepo.List(ctx, filter)
		if listErr != nil {
			return rolled, skipped, listErr
		}
		if len(page) == 0 {
			return rolled, skipped, nil
		}
		pageSize := len(page)
		subs := subscriptionsAfter(page, after)

		for _, sub := range subs {
			if sub.TenantID != tenantID || sub.EnvironmentID != environmentID {
				s.Logger.Info(ctx, "skipping subscription outside rollup environment",
					"subscription_id", sub.ID,
					"subscription_tenant_id", sub.TenantID,
					"subscription_environment_id", sub.EnvironmentID)
				continue
			}
			if !scope.includes(sub) {
				continue
			}

			// Checkpoint per subscription, not per page. A page is 1000
			// subscriptions; at a few hundred milliseconds each that outruns the
			// activity's heartbeat timeout, and the attempt would be killed
			// before it ever reported progress -- the exact failure the cursor
			// exists to prevent.
			checkpoint := func() {
				setCursor(types.RollupCursor{EnvironmentID: environmentID, LastSubscriptionID: maxSeen})
			}

			// Report progress BEFORE the roll as well. One subscription here can
			// carry hundreds of line items over an elapsed period, and a single
			// slow one outlasting the heartbeat timeout kills the attempt
			// mid-subscription -- checkpointing only on completion means the
			// heartbeat gap is however long the slowest subscription takes.
			checkpoint()

			if sub.ID > maxSeen {
				maxSeen = sub.ID
			}

			wasSkipped, rollErr := s.rollupSubscription(ctx, sub)
			if rollErr != nil {
				// Shadow write-path: one subscription's failure must never abort the
				// batch. Logged loud (never silent), tallied as skipped since it did
				// not produce rows.
				s.Logger.Error(ctx, "revenue rollup failed for subscription",
					"error", rollErr, "subscription_id", sub.ID)
				skipped++
				checkpoint()
				continue
			}
			if wasSkipped {
				skipped++
				checkpoint()
				continue
			}
			rolled++
			checkpoint()
		}

		if pageSize < batchSize {
			return rolled, skipped, nil
		}
		offset += batchSize
	}
}

// subscriptionsAfter keeps only the subscriptions ordered after `after`, so a
// resumed pass does not redo completed work. It filters rather than slices, so
// it is correct whatever order the repository returned — and a cursor naming a
// subscription that has since been deleted still resumes in the right place,
// because the cursor is an ordering, not a row reference.
func subscriptionsAfter(subs []*subscription.Subscription, after string) []*subscription.Subscription {
	if after == "" {
		return subs
	}
	kept := make([]*subscription.Subscription, 0, len(subs))
	for _, sub := range subs {
		if sub.ID > after {
			kept = append(kept, sub)
		}
	}
	return kept
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
	enabled, err := s.revenueAnalyticsEnabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}

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
	subLocations := map[string]*time.Location{}

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
		// the preview engine's — match on the same day-grained bounds the
		// rollup wrote.
		loc := s.subscriptionLocation(ctx, subLocations, subscriptionID)
		period := periodDays(periodStart, periodEndExclusive, loc)
		periodStart, periodEnd := dayOf(period.Start, loc), period.End

		priceID := lo.FromPtr(li.PriceID)
		isTrueup := li.Metadata.GetBool(types.MetadataKeyIsCommitmentTrueup)
		isOverage := li.Metadata.GetBool(types.MetadataKeyIsOverage)
		if isTrueup || isOverage {
			// True-up/overage lines carry a random price_id per compute; match
			// the provisional rows by re-deriving the same stable synthetic id.
			priceID = stableTrueupPriceID(lo.FromPtr(li.SubscriptionLineItemID), subscriptionID, isOverage)
		}
		// Two line items can share one price and period; the sub-line-item id
		// keeps each flip on its own rows. The fallback must match what the
		// rollup wrote for id-less (aggregate) lines: the subscription id.
		subLineItemID := lo.FromPtr(li.SubscriptionLineItemID)
		if subLineItemID == "" {
			subLineItemID = subscriptionID
		}

		n, err := s.RevenueFactRepo.FlipToFinal(ctx, subscriptionID, priceID, subLineItemID, periodStart, periodEnd, inv.ID, li.ID)
		if err != nil {
			return flipped, groupSeen, err
		}
		flipped += n

		key := fmt.Sprintf("%s|%d|%d", subscriptionID, periodStart.UnixNano(), periodEnd.UnixNano())
		groupSeen[key] = finalizePeriodGroup{subscriptionID: subscriptionID, periodStart: periodStart, periodEnd: periodEnd}
	}

	return flipped, groupSeen, nil
}

// subscriptionLocation resolves the timezone a subscription's days are split
// in, memoised per invoice. Bounds computed in any other zone will not line up
// with the rows buildUsageCurve wrote. A subscription that cannot be read
// falls back to UTC rather than failing the shadow path.
func (s *revenueService) subscriptionLocation(ctx context.Context, cache map[string]*time.Location, subscriptionID string) *time.Location {
	if loc, ok := cache[subscriptionID]; ok {
		return loc
	}
	loc := time.UTC
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		s.Logger.Info(ctx, "revenue facts falling back to UTC day bounds",
			"subscription_id", subscriptionID, "error", err.Error())
	} else {
		loc = locationOf(sub.Timezone)
	}
	cache[subscriptionID] = loc
	return loc
}

// rollupFromInvoice derives period_only PROVISIONAL rows from a finalized
// invoice.s own line items — the fallback when none exist for its period.
// Per-day usage splitting is deliberately not reconstructed here.
func (s *revenueService) rollupFromInvoice(ctx context.Context, inv *invoice.Invoice) error {
	priceCache := map[string]*price.Price{}
	meterCache := map[string]*meter.Meter{}
	subLocations := map[string]*time.Location{}
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
		loc := s.subscriptionLocation(ctx, subLocations, subscriptionID)
		period := periodDays(periodStart, periodEndExclusive, loc)

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
			Timezone:        loc.String(),
		}
		if base.SubLineItemID == "" {
			// Same fallback as the preview path AND the flip's matching rule —
			// all three must derive the identical id or the flip misses rows.
			base.SubLineItemID = subscriptionID
		}

		isTrueup := li.Metadata.GetBool(types.MetadataKeyIsCommitmentTrueup)
		isOverage := li.Metadata.GetBool(types.MetadataKeyIsOverage)
		before := len(rows)
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

		// Stamp the invoice these rows were derived from. The flip sets the
		// same ids when it promotes them; carrying them from the start keeps a
		// row that never flips traceable to its source.
		for _, row := range rows[before:] {
			row.InvoiceID = lo.ToPtr(inv.ID)
			row.InvoiceLineItemID = lo.ToPtr(li.ID)
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
	enabled, err := s.revenueAnalyticsEnabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}

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
	// usageByMeter is every meter's per-day usage for this subscription, read
	// once. Reading per line item instead is what made a full pass take hours:
	// a subscription here carries hundreds of line items over a handful of
	// meters.
	usageByMeter map[string][]events.DailyUsagePoint
}

// usage returns the meter's pre-read per-day quantities. A meter with no usage
// is absent from the map and yields nil, which the curve reads as no usage —
// distinct from "not pre-read", which only happens outside the rollup.
func (in *rollupInputs) usage(meterID string) []events.DailyUsagePoint {
	if in == nil || in.usageByMeter == nil {
		return nil
	}
	return in.usageByMeter[meterID]
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

func (s *revenueService) loadRollupInputs(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time, lineItems []dto.CreateInvoiceLineItemRequest) (*rollupInputs, error) {
	priceIDs := make([]string, 0, len(lineItems))
	meterIDs := make([]string, 0, len(lineItems))
	for i := range lineItems {
		li := &lineItems[i]
		// True-up price ids are generated fresh by the engine on every compute
		// and never persisted — fetching them would always miss, and their
		// rows use a stable synthetic id anyway. Overage lines keep their real
		// price/meter ids on split pairs, which the daily overage curve needs.
		if li.Metadata.GetBool(types.MetadataKeyIsCommitmentTrueup) {
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

	subscriptionService := service.NewSubscriptionService(s.ServiceParams)
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

	// One read for every meter on the subscription, over the widest window any
	// of its line items can ask for. Per-day (not cumulative) quantities come
	// back, so a line item starting mid-period accumulates from its own start.
	usageByMeter, err := s.MeterUsageRepo.GetDailyUsageByMeter(ctx, &events.DailyUsageParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		MeterIDs:            lo.Uniq(meterIDs),
		ExternalCustomerIDs: extCustomerIDs,
		StartTime:           periodStart,
		EndTime:             periodEnd,
		UseFinal:            true,
		Timezone:            sub.Timezone,
	})
	if err != nil {
		return nil, err
	}
	// A meter with no usage must still be present, as an empty slice: nil would
	// be read as "not pre-read" and fall back to a single-meter query, which is
	// the per-line-item behaviour this replaces.
	for _, id := range lo.Uniq(meterIDs) {
		if _, ok := usageByMeter[id]; !ok {
			usageByMeter[id] = []events.DailyUsagePoint{}
		}
	}

	return &rollupInputs{
		prices:            lo.KeyBy(prices, func(p *price.Price) string { return p.ID }),
		meters:            lo.KeyBy(meters, func(m *meter.Meter) string { return m.ID }),
		entitlementLimits: limits,
		grantsByMeterID:   grantsByMeterID,
		extCustomerIDs:    extCustomerIDs,
		usageByMeter:      usageByMeter,
	}, nil
}

// loadGrantsByMeterID returns the feature-scoped entitlement grants billing
// each meter this cycle — mirroring loadEntitlementGrantsByMeterID's scoping
// in the billing engine.
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
