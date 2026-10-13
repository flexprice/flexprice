package revenue

// Splitting one billed charge into per-day revenue_facts rows is called
// "decomposition" (the revenue_facts.decomposition_mode column): marginal
// mode writes one row per day, period_only mode writes a single row for the
// whole billing period.

import (
	"github.com/flexprice/flexprice/internal/ee/service"
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

// locationOf resolves a subscription's IANA timezone the same way
// buildUsageCurve does: an empty or unknown name falls back to UTC rather
// than failing the rollup.
func locationOf(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}
	if l, err := time.LoadLocation(tz); err == nil {
		return l
	}
	return time.UTC
}

// dayOf is t's calendar day in loc, stamped as UTC midnight — the shape
// buildUsageCurve writes to the day column. period_start, period_end and day
// are date columns, so every value written to or matched against them is
// truncated here; a timestamp compares as midnight and would miss its own row.
func dayOf(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
}

// inclusiveLastDay is the last day a period covers, matching the day-walk in
// buildUsageCurve: it emits days strictly before the end's own local date,
// folding a mid-day end's usage back into the previous day. Taking the end's
// own date instead would reach a day into the next period, whose first day it
// is.
func inclusiveLastDay(exclusiveEnd time.Time, loc *time.Location) time.Time {
	return dayOf(exclusiveEnd, loc).AddDate(0, 0, -1)
}

// periodDays keys a fact row to the days its billing window covers. Start
// stays exact because usage reads bound on it; only the stored and matched
// ends are day-grained. A window opening and closing on one date has no
// preceding day to end on, so it is clamped to that date rather than
// inverting.
func periodDays(start, exclusiveEnd time.Time, loc *time.Location) revenuePeriod {
	end := inclusiveLastDay(exclusiveEnd, loc)
	if end.Before(dayOf(start, loc)) {
		end = dayOf(start, loc)
	}
	return revenuePeriod{Start: start, End: end}
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

	// Source overrides the revenue source of marginal rows; empty means
	// usage. The daily overage split sets it so its rows book as overage.
	Source types.RevenueSource

	// PeriodStart/PeriodEnd bound the line item's billing period; PeriodEnd
	// is the inclusive last calendar day.
	PeriodStart time.Time
	PeriodEnd   time.Time

	// Timezone is the subscription's IANA name. Day bounds are computed in it
	// so they line up with the curve, which splits usage into local days.
	Timezone string
}

// location resolves Timezone, defaulting to UTC.
func (li previewLineItem) location() *time.Location {
	return locationOf(li.Timezone)
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

// factGrain is the provisional upsert key: two rows with the same grain are the
// same row, so a recompute updates in place rather than inserting.
type factGrain struct {
	priceID       string
	subLineItemID string
	day           string
	revenueSource types.RevenueSource
}

func grainOf(f *revenuefact.RevenueFact) factGrain {
	return factGrain{
		priceID:       lo.FromPtr(f.PriceID),
		subLineItemID: lo.FromPtr(f.SubLineItemID),
		day:           f.Day.Format(dayKeyLayout),
		revenueSource: f.RevenueSource,
	}
}

// carriesNothing reports a row with no money and no quantity behind it: a day
// on which a line item existed and delivered nothing. A row with zero net but a
// consumed entitlement is NOT empty — that is how free usage stays visible.
func carriesNothing(f *revenuefact.RevenueFact) bool {
	return f.NetAmount.IsZero() &&
		f.UsageAtListRate.IsZero() &&
		f.TierDelta.IsZero() &&
		f.EntitlementAmount.IsZero() &&
		f.LineDiscount.IsZero() &&
		f.InvoiceDiscount.IsZero() &&
		f.BillableQty.IsZero() &&
		f.EntitlementQty.IsZero()
}

// sameValues reports whether a recomputed row would write nothing new. It
// compares every field the upsert would set; id, computed_at and version are
// excluded because they differ on every recompute by construction, and status
// because both sides are provisional here.
//
// A value returning to zero counts as a change: the upsert never deletes, so
// skipping that write would leave the old non-zero row standing as stale
// revenue that no reconciliation catches.
func sameValues(a, b *revenuefact.RevenueFact) bool {
	return a.CustomerID == b.CustomerID &&
		lo.FromPtr(a.MeterID) == lo.FromPtr(b.MeterID) &&
		a.PeriodStart.Equal(b.PeriodStart) &&
		a.PeriodEnd.Equal(b.PeriodEnd) &&
		a.UsageAtListRate.Equal(b.UsageAtListRate) &&
		a.TierDelta.Equal(b.TierDelta) &&
		a.EntitlementAmount.Equal(b.EntitlementAmount) &&
		a.LineDiscount.Equal(b.LineDiscount) &&
		a.InvoiceDiscount.Equal(b.InvoiceDiscount) &&
		a.NetAmount.Equal(b.NetAmount) &&
		a.BillableQty.Equal(b.BillableQty) &&
		a.EntitlementQty.Equal(b.EntitlementQty) &&
		a.DecompositionMode == b.DecompositionMode &&
		a.Currency == b.Currency &&
		a.IsRevert == b.IsRevert &&
		lo.FromPtr(a.InvoiceID) == lo.FromPtr(b.InvoiceID) &&
		lo.FromPtr(a.InvoiceLineItemID) == lo.FromPtr(b.InvoiceLineItemID) &&
		// The remaining mutable columns. They are unwritten today, but the
		// upsert updates them on conflict, so leaving them out of the
		// comparison would strand stale metadata the moment anything sets them.
		lo.FromPtr(a.AggregationType) == lo.FromPtr(b.AggregationType) &&
		lo.FromPtr(a.RecognitionMethod) == lo.FromPtr(b.RecognitionMethod) &&
		sameTime(a.ServiceStart, b.ServiceStart) &&
		sameTime(a.ServiceEnd, b.ServiceEnd) &&
		sameTime(a.LockAdjustedDay, b.LockAdjustedDay)
}

// sameTime compares two nullable instants.
func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// changedRows keeps only the rows that would actually change something. Most of
// a nightly pass recomputes days that are already stored and identical: on a
// subscription with hundreds of line items, that was the bulk of the writes.
//
// This narrows the WRITE only. Reconciliation must still run on the full set,
// or an invoice whose rows are mostly unchanged would look like it had lost
// them.
func changedRows(computed []*revenuefact.RevenueFact, stored []*revenuefact.RevenueFact) []*revenuefact.RevenueFact {
	// No early return for an empty `stored`: a first roll is exactly when the
	// empty-row filter below matters most.
	byGrain := make(map[factGrain]*revenuefact.RevenueFact, len(stored))
	for _, row := range stored {
		byGrain[grainOf(row)] = row
	}

	changed := make([]*revenuefact.RevenueFact, 0, len(computed))
	for _, row := range computed {
		prior, exists := byGrain[grainOf(row)]
		if exists && sameValues(row, prior) {
			continue
		}
		// A row carrying nothing is worth writing only to correct one that
		// previously carried something — the upsert never deletes, so that
		// write is what stops a stale non-zero row standing as revenue. With
		// no stored row there is nothing to correct, and creating one records
		// that a line item existed and delivered nothing, which no query asks.
		//
		// This is not a marginal saving: on a subscription with hundreds of
		// line items, a row per line item per elapsed day is the whole table.
		// Measured on production, 13,464,148 of 13,465,791 rows carried
		// nothing.
		if !exists && !row.IsRevert && carriesNothing(row) {
			continue
		}
		changed = append(changed, row)
	}
	return changed
}

// decompositionMode picks period_only where a per-day split would misstate
// the charge: volume tiering re-rates all units on the final tier, and
// LATEST/AVG/WEIGHTED_SUM/MAX aggregations are not additive across days —
// the daily curve reads a cumulative SUM of quantities, which only prices
// sum-shaped billing correctly. MAX pricing (plain or bucketed at any size)
// stays whole-period until the curve can read per-day maxes.
func decompositionMode(p *price.Price, m *meter.Meter) types.DecompositionMode {
	// Bucketed pricing charges each window on its own quantity, so the
	// cumulative curve cannot reproduce it — buildBucketedCurve prices the
	// windows instead and groups them into days. That works for any window
	// that nests inside a day, whatever the aggregation or tier mode; week
	// and month windows span days and cannot be attributed to one.
	if price.IsBucketed(p, m) {
		if bucketedDayGrain(p, m) {
			return types.Marginal
		}
		return types.PeriodOnly
	}
	if p != nil && p.TierMode == types.BILLING_TIER_VOLUME {
		return types.PeriodOnly
	}
	if m != nil {
		switch m.Aggregation.Type {
		case types.AggregationLatest, types.AggregationAvg, types.AggregationWeightedSum, types.AggregationMax:
			return types.PeriodOnly
		case types.AggregationCountUnique:
			// The curve reads SUM(qty_total); a distinct count is
			// COUNT(DISTINCT unique_hash), which no running sum reproduces —
			// the same event repeating across days must not add twice.
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
func cadenceDay(cadence types.InvoiceCadence, period revenuePeriod, loc *time.Location) time.Time {
	if cadence == types.InvoiceCadenceAdvance {
		return dayOf(period.Start, loc)
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
		PeriodStart:       dayOf(period.Start, li.location()),
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
	return newPeriodOnlyFact(li, period, cadenceDay(invoiceCadence(li), period, li.location()), types.RevenueSourceFixed)
}

// decomposeUsagePeriodOnly writes one period_only row for a usage line item
// whose price/meter cannot be split per day (see decompositionMode). The
// engine's entitlement deduction is annotated when known.
func decomposeUsagePeriodOnly(li previewLineItem, period revenuePeriod) *revenuefact.RevenueFact {
	f := newPeriodOnlyFact(li, period, cadenceDay(invoiceCadence(li), period, li.location()), types.RevenueSourceUsage)
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

	source := li.Source
	if source == "" {
		source = types.RevenueSourceUsage
	}
	tier1Rate := listRate(li.Price)
	rows := make([]*revenuefact.RevenueFact, 0, len(curve))

	marginalCharges := make([]decimal.Decimal, len(curve))
	prevCharge := decimal.Zero
	for i, dc := range curve {
		marginalCharges[i] = dc.CumulativeCharge.Sub(prevCharge)
		prevCharge = dc.CumulativeCharge
	}
	dayLineDiscounts := service.SpreadAmount(li.LineDiscount, marginalCharges)
	dayInvoiceDiscounts := service.SpreadAmount(li.InvoiceDiscount, marginalCharges)

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
			RevenueSource:     source,
			PeriodStart:       dayOf(li.PeriodStart, li.location()),
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

// listRate is the price's first-tier unit rate, or the flat rate when the
// price has no tiers.
func listRate(p *price.Price) decimal.Decimal {
	if len(p.Tiers) > 0 {
		return p.Tiers[0].UnitAmount
	}
	// A package price's Amount buys a whole block of units, so the per-unit
	// list rate is that amount spread over the block. TierDelta then carries
	// the step-rounding the block pricing applies.
	if p.BillingModel == types.BILLING_MODEL_PACKAGE && p.TransformQuantity.DivideBy > 0 {
		return p.Amount.Div(decimal.NewFromInt(int64(p.TransformQuantity.DivideBy)))
	}
	return p.Amount
}
