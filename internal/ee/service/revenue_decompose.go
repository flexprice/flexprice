package service

// Splitting one billed charge into per-day revenue_facts rows is called
// "decomposition" (the revenue_facts.decomposition_mode column): marginal
// mode writes one row per day, period_only mode writes a single row for the
// whole billing period.

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// revenuePeriod is a billing window whose End is the inclusive last calendar
// day — the day-grain bounds revenue_facts rows are keyed on.
type revenuePeriod struct {
	Start time.Time
	End   time.Time
}

// exclusiveEnd returns the half-open upper bound (End + 1 day).
func (p revenuePeriod) exclusiveEnd() time.Time {
	return p.End.AddDate(0, 0, 1)
}

// previewLineItem is one line item as priced by the billing preview, carrying
// everything needed to turn its charge into revenue_facts rows.
type previewLineItem struct {
	TenantID      string
	EnvironmentID string
	CustomerID    string
	// SubscriptionID is the line item's own subscription (grouped invoicing
	// can bill several subscriptions on one invoice).
	SubscriptionID string
	SubLineItemID  string

	Price *price.Price
	Meter *meter.Meter

	Currency string
	Metadata types.Metadata

	// EngineAmount is the charge the billing engine computed for this line
	// item over its period, before discounts.
	EngineAmount decimal.Decimal

	// LineDiscount/InvoiceDiscount are this line item's own coupon discount
	// and its allocated share of invoice-level discounts. Row net amounts are
	// written net of both.
	LineDiscount    decimal.Decimal
	InvoiceDiscount decimal.Decimal

	// EntitlementQty is the free quantity the engine deducted from this line
	// item (nil-safe zero when no entitlement applied). Used to annotate
	// period_only usage rows; marginal rows derive it from the curve.
	EntitlementQty decimal.Decimal

	// PeriodStart/PeriodEnd bound the line item's billing period; PeriodEnd
	// is the inclusive last calendar day.
	PeriodStart time.Time
	PeriodEnd   time.Time
}

func (li previewLineItem) priceID() *string {
	if li.Price == nil || li.Price.ID == "" {
		return nil
	}
	return lo.ToPtr(li.Price.ID)
}

func (li previewLineItem) meterID() *string {
	if li.Meter == nil || li.Meter.ID == "" {
		return nil
	}
	return lo.ToPtr(li.Meter.ID)
}

func (li previewLineItem) aggregationType() *types.AggregationType {
	if li.Meter == nil {
		return nil
	}
	return lo.ToPtr(li.Meter.Aggregation.Type)
}

func (li previewLineItem) subLineItemID() *string {
	if li.SubLineItemID == "" {
		return nil
	}
	return lo.ToPtr(li.SubLineItemID)
}

// isCommitmentTrueupOrOverage reports whether li is the engine's synthetic
// true-up/overage line rather than a regular fixed charge.
func (li previewLineItem) isCommitmentTrueupOrOverage() bool {
	return li.Metadata.GetBool(types.MetadataKeyIsCommitmentTrueup) || li.Metadata.GetBool(types.MetadataKeyIsOverage)
}

// dayCharge holds one usage line item's running totals through the end of one
// calendar day. Every field is cumulative; a single day's value is the
// difference between consecutive entries.
type dayCharge struct {
	Day time.Time

	// CumulativeCharge is the engine charge on the billable quantity so far.
	CumulativeCharge decimal.Decimal
	// CumulativeGrossQty is the metered quantity so far, before the
	// entitlement limit is subtracted.
	CumulativeGrossQty decimal.Decimal
	// CumulativeBillableQty is max(0, gross - entitlement limit).
	CumulativeBillableQty decimal.Decimal
	// CumulativeEntitlementQty is min(gross, entitlement limit) — the free
	// quantity consumed so far.
	CumulativeEntitlementQty decimal.Decimal
	// UsageAtListRate is the gross quantity priced at the list rate.
	UsageAtListRate decimal.Decimal
	// TierDelta is the charge's deviation from a flat list-rate charge,
	// caused by graduated tiering. Zero for flat pricing.
	TierDelta decimal.Decimal
}

// decompositionMode picks period_only where a per-day split would misstate
// the charge: volume tiering re-rates all units on the final tier, LATEST/AVG/
// WEIGHTED_SUM aggregations are not additive across days, and week/month
// MAX buckets reset on boundaries coarser than a day.
func decompositionMode(p *price.Price, m *meter.Meter) types.DecompositionMode {
	if p != nil && p.TierMode == types.BILLING_TIER_VOLUME {
		return types.PeriodOnly
	}
	if m != nil {
		switch m.Aggregation.Type {
		case types.AggregationLatest, types.AggregationAvg, types.AggregationWeightedSum:
			return types.PeriodOnly
		}
	}
	if price.IsBucketedMax(p, m) {
		switch price.ResolveBucketSize(p, m) {
		case types.WindowSizeWeek, types.WindowSizeMonth:
			return types.PeriodOnly
		}
	}
	return types.Marginal
}

// isMultiPeriodCommitment reports a commitment spanning more than one billing
// period (e.g. ANNUAL commitment on a MONTHLY subscription). The rollup skips
// these: their true-up cannot be attributed to a single period. Only the
// subscription carries a commitment duration — line-item commitments settle
// within their own billing period.
func isMultiPeriodCommitment(sub *subscription.Subscription) bool {
	if sub.CommitmentAmount == nil || !sub.CommitmentAmount.GreaterThan(decimal.Zero) {
		return false
	}
	if sub.OverageFactor == nil || !sub.OverageFactor.GreaterThan(decimal.NewFromInt(1)) {
		return false
	}
	if sub.CommitmentDuration == nil || *sub.CommitmentDuration == sub.BillingPeriod {
		return false
	}
	return true
}

// cadenceDay is the day a period_only row is dated on: period start for
// ADVANCE billing, period end for ARREAR.
func cadenceDay(cadence types.InvoiceCadence, period revenuePeriod) time.Time {
	if cadence == types.InvoiceCadenceAdvance {
		return period.Start
	}
	return period.End
}

// invoiceCadence reads the price's cadence, defaulting to ARREAR without one.
func invoiceCadence(li previewLineItem) types.InvoiceCadence {
	if li.Price == nil {
		return types.InvoiceCadenceArrear
	}
	return li.Price.InvoiceCadence
}

// newPeriodOnlyFact builds the fields shared by every period_only row.
// NetAmount is the engine charge net of the line's discounts.
func newPeriodOnlyFact(li previewLineItem, period revenuePeriod, day time.Time, source types.RevenueSource) *revenuefact.RevenueFact {
	return &revenuefact.RevenueFact{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_REVENUE_FACT),
		TenantID:          li.TenantID,
		EnvironmentID:     li.EnvironmentID,
		CustomerID:        li.CustomerID,
		SubscriptionID:    li.SubscriptionID,
		SubLineItemID:     li.subLineItemID(),
		PriceID:           li.priceID(),
		RevenueSource:     source,
		PeriodStart:       period.Start,
		PeriodEnd:         period.End,
		Day:               day,
		LineDiscount:      li.LineDiscount,
		InvoiceDiscount:   li.InvoiceDiscount,
		NetAmount:         li.EngineAmount.Sub(li.LineDiscount).Sub(li.InvoiceDiscount),
		DecompositionMode: types.PeriodOnly,
		Currency:          li.Currency,
		Status:            types.FactProvisional,
		Version:           1,
		ComputedAt:        time.Now().UTC(),
	}
}

// decomposeFixed writes one period_only row for a FIXED-price line item.
// Returns nil for the engine's synthetic true-up/overage lines — those belong
// to decomposeCommitmentTrueup.
func decomposeFixed(li previewLineItem, period revenuePeriod) *revenuefact.RevenueFact {
	if li.isCommitmentTrueupOrOverage() {
		return nil
	}
	return newPeriodOnlyFact(li, period, cadenceDay(invoiceCadence(li), period), types.RevenueSourceFixed)
}

// decomposeUsagePeriodOnly writes one period_only row for a usage line item
// whose price/meter cannot be split per day (see decompositionMode). The
// engine's entitlement deduction is annotated when known.
func decomposeUsagePeriodOnly(li previewLineItem, period revenuePeriod) *revenuefact.RevenueFact {
	f := newPeriodOnlyFact(li, period, cadenceDay(invoiceCadence(li), period), types.RevenueSourceUsage)
	f.MeterID = li.meterID()
	f.AggregationType = li.aggregationType()
	f.EntitlementQty = li.EntitlementQty
	if li.Price != nil && li.EntitlementQty.IsPositive() {
		f.EntitlementAmount = li.EntitlementQty.Mul(listRate(li.Price))
	}
	return f
}

// decomposeCommitmentTrueup writes one row for the commitment true-up charge,
// dated at period end — the amount is only known once usage is final.
func decomposeCommitmentTrueup(li previewLineItem, period revenuePeriod) *revenuefact.RevenueFact {
	return newPeriodOnlyFact(li, period, period.End, types.RevenueSourceCommitmentTrueup)
}

// decomposeOverage writes one row for the commitment overage charge, dated at
// period end like the true-up.
func decomposeOverage(li previewLineItem, period revenuePeriod) *revenuefact.RevenueFact {
	return newPeriodOnlyFact(li, period, period.End, types.RevenueSourceOverage)
}

// decomposeUsageMarginal writes one row per day, each carrying the day-over-day
// delta of the cumulative curve, with the line's discounts spread across days
// in proportion to each day's pre-discount charge. Every row satisfies
// NetAmount == UsageAtListRate + TierDelta - EntitlementAmount - LineDiscount - InvoiceDiscount.
func decomposeUsageMarginal(li previewLineItem, curve []dayCharge) []*revenuefact.RevenueFact {
	if len(curve) == 0 {
		return nil
	}

	tier1Rate := listRate(li.Price)
	rows := make([]*revenuefact.RevenueFact, 0, len(curve))

	marginalCharges := make([]decimal.Decimal, len(curve))
	prevCharge := decimal.Zero
	for i, dc := range curve {
		marginalCharges[i] = dc.CumulativeCharge.Sub(prevCharge)
		prevCharge = dc.CumulativeCharge
	}
	dayLineDiscounts := spreadAmount(li.LineDiscount, marginalCharges)
	dayInvoiceDiscounts := spreadAmount(li.InvoiceDiscount, marginalCharges)

	var prev dayCharge
	for i, dc := range curve {
		marginalBillableQty := dc.CumulativeBillableQty.Sub(prev.CumulativeBillableQty)
		marginalEntitlementQty := dc.CumulativeEntitlementQty.Sub(prev.CumulativeEntitlementQty)
		marginalUsageAtListRate := dc.UsageAtListRate.Sub(prev.UsageAtListRate)
		marginalTierDelta := dc.TierDelta.Sub(prev.TierDelta)
		entitlementAmount := marginalEntitlementQty.Mul(tier1Rate)

		rows = append(rows, &revenuefact.RevenueFact{
			ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_REVENUE_FACT),
			TenantID:          li.TenantID,
			EnvironmentID:     li.EnvironmentID,
			CustomerID:        li.CustomerID,
			SubscriptionID:    li.SubscriptionID,
			SubLineItemID:     li.subLineItemID(),
			PriceID:           li.priceID(),
			MeterID:           li.meterID(),
			AggregationType:   li.aggregationType(),
			RevenueSource:     types.RevenueSourceUsage,
			PeriodStart:       li.PeriodStart,
			PeriodEnd:         li.PeriodEnd,
			Day:               dc.Day,
			UsageAtListRate:   marginalUsageAtListRate,
			TierDelta:         marginalTierDelta,
			EntitlementAmount: entitlementAmount,
			LineDiscount:      dayLineDiscounts[i],
			InvoiceDiscount:   dayInvoiceDiscounts[i],
			NetAmount:         marginalCharges[i].Sub(dayLineDiscounts[i]).Sub(dayInvoiceDiscounts[i]),
			BillableQty:       marginalBillableQty,
			EntitlementQty:    marginalEntitlementQty,
			DecompositionMode: types.Marginal,
			Currency:          li.Currency,
			Status:            types.FactProvisional,
			Version:           1,
			ComputedAt:        time.Now().UTC(),
		})
		prev = dc
	}

	return rows
}

// spreadAmount splits total across parts in proportion to weights, assigning
// the rounding remainder to the last positive-weight part so the parts always
// sum exactly to total. All-zero weights put the whole total on the last part.
func spreadAmount(total decimal.Decimal, weights []decimal.Decimal) []decimal.Decimal {
	parts := make([]decimal.Decimal, len(weights))
	if len(weights) == 0 || total.IsZero() {
		return parts
	}

	weightSum := decimal.Zero
	lastPositive := -1
	for i, w := range weights {
		if w.IsPositive() {
			weightSum = weightSum.Add(w)
			lastPositive = i
		}
	}
	if lastPositive == -1 {
		parts[len(parts)-1] = total
		return parts
	}

	allocated := decimal.Zero
	for i, w := range weights {
		if !w.IsPositive() || i == lastPositive {
			continue
		}
		parts[i] = total.Mul(w).Div(weightSum)
		allocated = allocated.Add(parts[i])
	}
	parts[lastPositive] = total.Sub(allocated)
	return parts
}

// listRate is the price's first-tier unit rate, or the flat rate when the
// price has no tiers.
func listRate(p *price.Price) decimal.Decimal {
	if len(p.Tiers) > 0 {
		return p.Tiers[0].UnitAmount
	}
	return p.Amount
}
