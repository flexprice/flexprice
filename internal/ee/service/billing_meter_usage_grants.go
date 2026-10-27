package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/priceunit"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// adjustMeterUsageGrantsResult is the per-line-item output of the grant folding.
// Overage is in meter units for the quantity measure and in currency for the
// amount measure; PerECOverage decomposes it per entitlement config.
type adjustMeterUsageGrantsResult struct {
	Measure      types.EntitlementGrantMeasure
	Overage      decimal.Decimal
	PerECOverage map[string]decimal.Decimal
}

// loadEntitlementGrantsByMeterID returns grants overlapping the billing cycle
// for this subscription, bucketed by meter id.
//
// Feature scope only: this map is consumed per line item, and a grant spanning
// several meters would count its overage once per meter. Subscription grants are
// applied across lines by applySubscriptionGrants; GROUP grants are not billed.
func (s *billingService) loadEntitlementGrantsByMeterID(
	ctx context.Context,
	sub *subscription.Subscription,
	aggregatedFeatures []*dto.AggregatedFeature,
	periodStart, periodEnd time.Time,
) (map[string][]*entitlementgrant.EntitlementGrant, error) {
	if s.EntitlementGrantRepo == nil || sub == nil {
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
	if len(grants) == 0 {
		return nil, nil
	}

	meterByFeatureID := make(map[string]string, len(aggregatedFeatures))
	for _, f := range aggregatedFeatures {
		if f == nil || f.Feature == nil || f.Feature.MeterID == "" {
			continue
		}
		meterByFeatureID[f.Feature.ID] = f.Feature.MeterID
	}

	// A grant outlives the config that funded it, so a feature whose last live
	// entitlement is gone is missing from aggregatedFeatures. Resolve those
	// features directly — the lookup the evaluator already does — or the grant
	// silently stops discounting while the evaluator keeps maintaining it.
	missing := make([]string, 0)
	for _, g := range grants {
		if g == nil || !g.IsFeatureScoped() {
			continue
		}
		if _, ok := meterByFeatureID[g.ScopeEntityID]; !ok {
			missing = append(missing, g.ScopeEntityID)
		}
	}
	if len(missing) > 0 {
		featureFilter := types.NewNoLimitFeatureFilter()
		featureFilter.FeatureIDs = lo.Uniq(missing)
		orphanFeatures, err := s.FeatureRepo.List(ctx, featureFilter)
		if err != nil {
			return nil, err
		}
		for _, f := range orphanFeatures {
			if f != nil && f.MeterID != "" {
				meterByFeatureID[f.ID] = f.MeterID
			}
		}
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

// adjustMeterUsageGrants folds the meter's EG overage into the line item.
// Quantity measure: overage re-enters the pricer as the billable quantity.
// Amount measure: overage is already money and bypasses the pricer.
// applied=false when the pricing guard rejects the line item; a measurement
// error propagates — billing a knowingly wrong number is never an option.
func (s *billingService) adjustMeterUsageGrants(
	ctx context.Context,
	item *subscription.SubscriptionLineItem,
	matchingCharge *dto.SubscriptionUsageByMetersResponse,
	grants []*entitlementgrant.EntitlementGrant,
	priceService PriceService,
	m *meter.Meter,
	sub *subscription.Subscription,
	extCustomerIDs []string,
) (adjustMeterUsageGrantsResult, bool, error) {
	if len(grants) == 0 {
		return adjustMeterUsageGrantsResult{}, false, nil
	}

	// Measure is copied from the EC to every grant; EC-write validation keeps
	// it consistent per feature, so the first row is authoritative.
	measure := grants[0].Measure
	if measure == "" {
		return adjustMeterUsageGrantsResult{}, false, nil
	}

	if guardErr := GrantPricingGuard(measure, item, matchingCharge.Price, m); guardErr != nil {
		s.Logger.Error(ctx, "entitlement grant overage: line item rejected, skipping grants",
			"meter_id", item.MeterID,
			"line_item_id", item.ID,
			"measure", measure,
			"error", guardErr,
		)
		return adjustMeterUsageGrantsResult{}, false, nil
	}

	// Per-EC violation totals (usage − quota). Overlapping windows share the usage
	// stream, so these can double count — attribution only, never summed blindly.
	perECOverage := make(map[string]decimal.Decimal)
	for _, g := range grants {
		if g == nil {
			continue
		}
		if overage := g.Overage(); overage.IsPositive() {
			perECOverage[g.EntitlementConfigID] = perECOverage[g.EntitlementConfigID].Add(overage)
		}
	}

	res := adjustMeterUsageGrantsResult{Measure: measure, PerECOverage: perECOverage}
	if !grantWindowsOverlap(grants) {
		// Disjoint => an event lands in exactly one window, so the overages just add up.
		// Two ECs is NOT overlap: a detach re-keys the pooled slot and the cycle tiles.
		//   EG1 [t0,t1) EC1 Q=1000 U=1200 => over 200
		//   EG2 [t1,t2) EC2 Q= 800 U= 900 => over 100
		//   bill 300 — the 1200 and the 900 are different events
		for _, total := range perECOverage {
			res.Overage = res.Overage.Add(total)
		}
	} else {
		// Overlapping => parallel ECs meter the SAME events against their own quotas,
		// so adding the overages bills one unit twice.
		//   EG1 [t0,t2) EC1 Q=500 U=900 => over 400
		//   EG2 [t0,t2) EC2 Q=400 U=900 => over 500
		//   adding gives 900 for 900 units of usage — every unit billed twice.
		// Measure the merged overage window instead: 900 units, crossing at 600,
		// bills the 300 spent past it, once.
		overage, err := s.mergedOverage(ctx, m, sub, extCustomerIDs, grants, measure)
		if err != nil {
			return adjustMeterUsageGrantsResult{}, false, err
		}
		res.Overage = overage
	}

	switch measure {
	case types.EntitlementGrantMeasureQuantity:
		matchingCharge.SetQuantityDecimal(res.Overage)
		if matchingCharge.Price != nil {
			cost := priceService.CalculateCost(ctx, matchingCharge.Price, res.Overage)
			matchingCharge.SetAmountWithCurrencyPrecision(cost, matchingCharge.Price.Currency)
		} else {
			matchingCharge.SetAmountDecimal(decimal.Zero)
		}
	case types.EntitlementGrantMeasureAmount:
		// Already money — zero the qty so aggregation doesn't double-count.
		if matchingCharge.Price != nil {
			matchingCharge.SetAmountWithCurrencyPrecision(res.Overage, matchingCharge.Price.Currency)
		} else {
			matchingCharge.SetAmountDecimal(res.Overage)
		}
		matchingCharge.SetQuantityDecimal(decimal.Zero)
	}
	return res, true, nil
}

// mergedOverage bills usage inside the unique overage windows across the
// meter's ECs. Each quota-crossed EG contributes [quota_crossed_at, valid_to);
// overlapping windows merge, so a unit bills once no matter how many EGs were in overage.
//
// Quantity measure: one ClickHouse query over all windows (TimeRanges)
// Amount measure: one billing-path pricing per window.
// Zero queries when nothing crossed.
func (s *billingService) mergedOverage(
	ctx context.Context,
	m *meter.Meter,
	sub *subscription.Subscription,
	extCustomerIDs []string,
	grants []*entitlementgrant.EntitlementGrant,
	measure types.EntitlementGrantMeasure,
) (decimal.Decimal, error) {
	overageIntervals := make([]TimeInterval, 0, len(grants))
	for _, g := range grants {
		if g != nil && g.QuotaCrossedAt != nil {
			overageIntervals = append(overageIntervals, TimeInterval{Start: *g.QuotaCrossedAt, End: g.ValidTo})
		}
	}
	if len(overageIntervals) == 0 {
		return decimal.Zero, nil
	}
	if m == nil {
		return decimal.Zero, errGrantDepsMissing
	}

	overageWindows := MergeIntervals(overageIntervals)
	billableRanges := make([]events.TimeRange, 0, len(overageWindows))
	for _, w := range overageWindows {
		billableRanges = append(billableRanges, events.TimeRange{Start: w.Start, End: w.End})
	}

	meterUsageSvc := NewMeterUsageService(s.ServiceParams)
	total := decimal.Zero
	switch measure {
	case types.EntitlementGrantMeasureQuantity:
		usage, err := meterUsageSvc.GetUsageTotal(ctx, &dto.UsageTotalRequest{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerIDs: extCustomerIDs,
			MeterID:             m.ID,
			AggregationType:     m.Aggregation.Type,
			TimeRanges:          billableRanges,
		})
		if err != nil {
			return decimal.Zero, err
		}
		total = usage
	case types.EntitlementGrantMeasureAmount:
		if sub == nil {
			return decimal.Zero, errGrantDepsMissing
		}
		for _, r := range billableRanges {
			cost, err := meterUsageSvc.GetMeterWindowCost(ctx, sub, m.ID, r.Start, r.End)
			if err != nil {
				return decimal.Zero, err
			}
			total = total.Add(cost)
		}
	}
	return total, nil
}

// grantWindowsOverlap reports whether any two grant windows share an instant — the
// only case where one event can be counted against two quotas.
// [t0,t1) + [t1,t2) => false, a re-keyed pool tiling the cycle.
// [t0,t2) + [t0,t2) => true, parallel ECs on one feature.
func grantWindowsOverlap(grants []*entitlementgrant.EntitlementGrant) bool {
	windows := make([]TimeInterval, 0, len(grants))
	for _, g := range grants {
		if g != nil && g.ValidTo.After(g.ValidFrom) {
			windows = append(windows, TimeInterval{Start: g.ValidFrom, End: g.ValidTo})
		}
	}
	if len(windows) < 2 {
		return false
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].Start.Before(windows[j].Start) })

	maxEnd := windows[0].End
	for _, w := range windows[1:] {
		if w.Start.Before(maxEnd) {
			return true
		}
		if w.End.After(maxEnd) {
			maxEnd = w.End
		}
	}
	return false
}

// TimeInterval is a half-open [Start, End) range.
type TimeInterval struct {
	Start, End time.Time
}

// MergeIntervals coalesces overlapping/touching intervals; empty ones drop.
func MergeIntervals(in []TimeInterval) []TimeInterval {
	valid := make([]TimeInterval, 0, len(in))
	for _, iv := range in {
		if iv.End.After(iv.Start) {
			valid = append(valid, iv)
		}
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].Start.Before(valid[j].Start) })

	out := make([]TimeInterval, 0, len(valid))
	for _, iv := range valid {
		if n := len(out); n > 0 && !iv.Start.After(out[n-1].End) {
			if iv.End.After(out[n-1].End) {
				out[n-1].End = iv.End
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}

// GrantPricingGuard returns nil when grants may fold into this line item.
// The fold assumes usage is additive over disjoint time windows (snapshot sums
// per window, merged-window measurement) — so non-additive aggregations
// (MAX, LATEST, AVG, ...) and bucketed meters are rejected outright.
// Tiered prices are rejected for both measures (tiers walk with cumulative
// cycle quantity; overage priced standalone would land in the wrong tier).
// Commitment/true-up reject only the amount measure — they reconcile the
// whole cycle, which pre-priced overage bypasses; the quantity measure feeds
// its qty back into the normal pipeline where they compose correctly.
func GrantPricingGuard(measure types.EntitlementGrantMeasure, item *subscription.SubscriptionLineItem, price *priceDomain.Price, m *meter.Meter) error {
	if m != nil {
		switch m.Aggregation.Type {
		case types.AggregationSum, types.AggregationCount, types.AggregationSumWithMultiplier:
		default:
			return errGrantNonAdditiveAggregation
		}
		if priceDomain.IsBucketed(price, m) {
			return errGrantBucketedMeter
		}
	}
	if price != nil && price.BillingModel == types.BILLING_MODEL_TIERED {
		return errGrantTiered
	}
	if measure != types.EntitlementGrantMeasureAmount || item == nil {
		return nil
	}
	if item.HasAnyCommitment() {
		return errGrantAmountCommitment
	}
	if item.HasTrueUpEnabled() {
		return errGrantAmountTrueUp
	}
	return nil
}

var (
	errGrantAmountCommitment       = grantGuardError("line item carries a commitment")
	errGrantAmountTrueUp           = grantGuardError("line item enables true-up")
	errGrantTiered                 = grantGuardError("price uses tiered billing")
	errGrantDepsMissing            = grantGuardError("measurement dependencies unavailable")
	errGrantNonAdditiveAggregation = grantGuardError("meter aggregation is not additive over time windows")
	errGrantBucketedMeter          = grantGuardError("meter uses bucketed aggregation")
)

type grantGuardError string

func (e grantGuardError) Error() string { return string(e) }

// metadataKeySubscriptionGrantAllowance records how much of an invoice line a subscription grant covered.
const metadataKeySubscriptionGrantAllowance = "subscription_grant_allowance"

var (
	errSubGrantCommitment       = grantGuardError("subscription has a minimum spend")
	errSubGrantCadence          = grantGuardError("usage line billed at a different cadence than the subscription")
	errSubGrantWithFeatureGrant = grantGuardError("subscription also has feature grants")
)

// loadSubscriptionGrants returns the subscription grant windows overlapping the period. It returns
// none, and logs why, when the subscription has a setup the grants cannot be billed on.
func (s *billingService) loadSubscriptionGrants(
	ctx context.Context,
	sub *subscription.Subscription,
	featureGrants map[string][]*entitlementgrant.EntitlementGrant,
	chargesByLineItemID map[string][]*dto.SubscriptionUsageByMetersResponse,
	commitmentLineIDs map[string]bool,
	meters map[string]*meter.Meter,
	periodStart, periodEnd time.Time,
) ([]*entitlementgrant.EntitlementGrant, error) {
	if s.EntitlementGrantRepo == nil || sub == nil {
		return nil, nil
	}
	filter := types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(sub.CustomerID).
		WithSubscriptionIDs(sub.ID).
		WithScopeEntityType(types.EntitlementGrantScopeSubscription).
		WithCycleOverlap(periodStart, periodEnd)
	grants, err := s.EntitlementGrantRepo.List(ctx, filter)
	if err != nil || len(grants) == 0 {
		return nil, err
	}

	items, err := s.SubscriptionLineItemRepo.ListBySubscription(ctx, sub)
	if err != nil {
		return nil, err
	}
	if reason := subscriptionGrantBlocker(sub, items, featureGrants, chargesByLineItemID, commitmentLineIDs, meters, periodStart, periodEnd); reason != nil {
		s.Logger.Error(ctx, "subscription grant overage: subscription rejected, grants not applied",
			"subscription_id", sub.ID,
			"error", reason,
		)
		return nil, nil
	}
	return grants, nil
}

// subscriptionGrantBlocker returns why subscription grants cannot be billed on this subscription,
// or nil. The grant must cover every usage line once, so one bad line blocks it everywhere.
func subscriptionGrantBlocker(
	sub *subscription.Subscription,
	items []*subscription.SubscriptionLineItem,
	featureGrants map[string][]*entitlementgrant.EntitlementGrant,
	chargesByLineItemID map[string][]*dto.SubscriptionUsageByMetersResponse,
	commitmentLineIDs map[string]bool,
	meters map[string]*meter.Meter,
	periodStart, periodEnd time.Time,
) error {
	if sub.HasCommitment() || lo.FromPtr(sub.CommitmentAmount).IsPositive() {
		return errSubGrantCommitment
	}
	if len(featureGrants) > 0 {
		return errSubGrantWithFeatureGrant
	}
	for _, item := range items {
		if item == nil || item.PriceType != types.PRICE_TYPE_USAGE || !overlapsPeriod(item, periodStart, periodEnd) {
			continue
		}
		if commitmentLineIDs[item.ID] {
			return errSubGrantCommitment
		}
		if item.BillingPeriod != sub.BillingPeriod || max(item.BillingPeriodCount, 1) != max(sub.BillingPeriodCount, 1) {
			return errSubGrantCadence
		}
		var p *priceDomain.Price
		if charges := chargesByLineItemID[item.ID]; len(charges) > 0 {
			p = charges[0].Price
		}
		if reason := GrantPricingGuard(types.EntitlementGrantMeasureAmount, item, p, meters[item.MeterID]); reason != nil {
			return reason
		}
	}
	return nil
}

func overlapsPeriod(item *subscription.SubscriptionLineItem, periodStart, periodEnd time.Time) bool {
	startsInTime := item.StartDate.IsZero() || item.StartDate.Before(periodEnd)
	endsInTime := item.EndDate.IsZero() || item.EndDate.After(periodStart)
	return startsInTime && endsInTime
}

// applySubscriptionGrants lowers each usage line to what it is billed after the subscription grants
// and returns the total taken off.
func (s *billingService) applySubscriptionGrants(
	ctx context.Context,
	sub *subscription.Subscription,
	grants []*entitlementgrant.EntitlementGrant,
	lines []dto.CreateInvoiceLineItemRequest,
	periodStart, periodEnd time.Time,
) (decimal.Decimal, error) {
	if len(grants) == 0 || len(lines) == 0 {
		return decimal.Zero, nil
	}
	billed, err := s.subscriptionGrantBilled(ctx, sub, grants, lines, periodStart, periodEnd)
	if err != nil {
		return decimal.Zero, err
	}

	reduction := decimal.Zero
	for i := range lines {
		allowance := lines[i].Amount.Sub(billed[i])
		if allowance.IsZero() {
			continue
		}
		if err := s.lowerLineAmount(ctx, &lines[i], billed[i], allowance); err != nil {
			return decimal.Zero, err
		}
		reduction = reduction.Add(allowance)
	}
	return reduction, nil
}

// subscriptionGrantBilled returns what each line is billed after the grants, in line order.
//
//   - Windows never overlap: overage = sum of (usage - quota), split by each line's share.
//     $30 grant, X $70 + Y $50: charge $90, X $52.50, Y $37.50.
//   - Parallel windows overlap: each line is billed its own spend after a crossing.
func (s *billingService) subscriptionGrantBilled(
	ctx context.Context,
	sub *subscription.Subscription,
	grants []*entitlementgrant.EntitlementGrant,
	lines []dto.CreateInvoiceLineItemRequest,
	periodStart, periodEnd time.Time,
) ([]decimal.Decimal, error) {
	precision := types.GetCurrencyPrecision(sub.Currency)
	amounts := lo.Map(lines, func(l dto.CreateInvoiceLineItemRequest, _ int) decimal.Decimal { return l.Amount })

	if !grantWindowsOverlap(grants) {
		overage := decimal.Zero
		for _, g := range grants {
			overage = overage.Add(decimal.Max(g.Overage(), decimal.Zero))
		}
		return splitByShare(amounts, overage, precision), nil
	}

	spend, err := s.lineSpendAfterCrossing(ctx, sub, grants, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	billed := make([]decimal.Decimal, len(lines))
	for i, l := range lines {
		own := decimal.Max(spend[lo.FromPtr(l.SubscriptionLineItemID)], decimal.Zero)
		billed[i] = decimal.Min(l.Amount, own).Round(precision)
	}
	return billed, nil
}

// lowerLineAmount sets the line's billed amount and records the allowance taken off it.
func (s *billingService) lowerLineAmount(ctx context.Context, line *dto.CreateInvoiceLineItemRequest, billed, allowance decimal.Decimal) error {
	line.Amount = billed
	if line.Metadata == nil {
		line.Metadata = types.Metadata{}
	}
	line.Metadata[metadataKeySubscriptionGrantAllowance] = allowance.String()
	if line.PriceUnit == nil {
		return nil
	}
	unit, err := s.PriceUnitRepo.GetByCode(ctx, lo.FromPtr(line.PriceUnit))
	if err != nil {
		return err
	}
	converted, err := priceunit.ConvertToPriceUnitAmount(ctx, billed, unit.ConversionRate, unit.BaseCurrency)
	if err != nil {
		return err
	}
	line.PriceUnitAmount = lo.ToPtr(converted)
	return nil
}

// lineSpendAfterCrossing prices every usage line inside the merged [quota_crossed_at, valid_to)
// windows, clipped to the period, and returns the spend per subscription line item.
func (s *billingService) lineSpendAfterCrossing(
	ctx context.Context,
	sub *subscription.Subscription,
	grants []*entitlementgrant.EntitlementGrant,
	periodStart, periodEnd time.Time,
) (map[string]decimal.Decimal, error) {
	crossed := make([]TimeInterval, 0, len(grants))
	for _, g := range grants {
		if g.QuotaCrossedAt != nil {
			crossed = append(crossed, TimeInterval{
				Start: types.LatestOf(*g.QuotaCrossedAt, periodStart),
				End:   types.EarliestOf(g.ValidTo, periodEnd),
			})
		}
	}

	spend := make(map[string]decimal.Decimal)
	for _, w := range MergeIntervals(crossed) {
		_, perLine, err := subscriptionLineSpend(ctx, s.ServiceParams, sub, w.Start, w.End)
		if err != nil {
			return nil, err
		}
		for lineID, amount := range perLine {
			spend[lineID] = spend[lineID].Add(amount)
		}
	}
	return spend, nil
}

// subscriptionLineSpend prices every usage line on the subscription over [start, end), returning
// the total and the amount per subscription line item.
func subscriptionLineSpend(
	ctx context.Context,
	params ServiceParams,
	sub *subscription.Subscription,
	start, end time.Time,
) (decimal.Decimal, map[string]decimal.Decimal, error) {
	perLine := make(map[string]decimal.Decimal)
	if !end.After(start) {
		return decimal.Zero, perLine, nil
	}

	meterUsageSvc := NewMeterUsageService(params)
	subUsage, err := meterUsageSvc.GetSubscriptionMeterUsageWithSub(ctx, sub, &GetSubscriptionMeterUsageRequest{
		SubscriptionID:  sub.ID,
		StartTime:       start,
		EndTime:         end,
		UseFinal:        true,
		IncludeChildren: true,
	})
	if err != nil {
		return decimal.Zero, nil, err
	}
	charges, _, err := meterUsageSvc.ConvertToBillingCharges(ctx, subUsage)
	if err != nil {
		return decimal.Zero, nil, err
	}

	total := decimal.Zero
	for _, c := range charges {
		if c == nil {
			continue
		}
		amount := decimal.NewFromFloat(c.Amount)
		total = total.Add(amount)
		perLine[c.SubscriptionLineItemID] = perLine[c.SubscriptionLineItemID].Add(amount)
	}
	return total, perLine, nil
}

// splitByShare spreads the overage over the lines in proportion to their gross amounts, rounded to
// the currency. Rounding drift goes to the largest line so the parts add up to min(overage, total gross).
func splitByShare(gross []decimal.Decimal, overage decimal.Decimal, precision int32) []decimal.Decimal {
	billed := make([]decimal.Decimal, len(gross))
	total, largest := decimal.Zero, 0
	for i, g := range gross {
		total = total.Add(decimal.Max(g, decimal.Zero))
		if g.GreaterThan(gross[largest]) {
			largest = i
		}
	}
	if !total.IsPositive() || !overage.IsPositive() {
		return billed
	}
	// Never bill more than was spent.
	overage = decimal.Min(overage, total).Round(precision)

	sum := decimal.Zero
	for i, g := range gross {
		if g.IsPositive() {
			billed[i] = g.Mul(overage).Div(total).Round(precision)
			sum = sum.Add(billed[i])
		}
	}
	billed[largest] = decimal.Min(gross[largest], billed[largest].Add(overage.Sub(sum)))
	return billed
}
