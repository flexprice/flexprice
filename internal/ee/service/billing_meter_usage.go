package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/priceunit"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// meterUsageBaseChargeInfo holds per-line-item data collected during the main loop
// for deferred processing by the cumulative commitment path.
type meterUsageBaseChargeInfo struct {
	item                        *subscription.SubscriptionLineItem
	baseAmount                  decimal.Decimal
	quantityForCalculation      decimal.Decimal
	priceUnitAmount             decimal.Decimal
	displayName                 *string
	metadata                    types.Metadata
	adjustedEntitlementQuantity *decimal.Decimal
}

// elapsedLineItemWindow clips a line item's overlap with [periodStart, periodEnd)
// against asOf. ok is false when nothing has elapsed yet — the line starts in
// the future, has already ended, or is zero-length (EndDate == StartDate).
func elapsedLineItemWindow(item *subscription.SubscriptionLineItem, periodStart, periodEnd, asOf time.Time) (start, end time.Time, ok bool) {
	start = item.GetPeriodStart(periodStart)
	end = item.GetPeriodEnd(periodEnd)
	if start.After(asOf) {
		return start, end, false
	}
	if !item.EndDate.IsZero() && !item.EndDate.After(item.StartDate) {
		return start, end, false
	}
	if end.After(asOf) {
		end = asOf
	}
	return start, end, end.After(start) || start.Equal(asOf)
}

// CalculateMeterUsageCharges computes usage-based invoice line items from the meter_usage table.
// All queries (bucketed meters, windowed entitlements, windowed commitments) read from
// MeterUsageRepo — never from raw events.
func (s *billingService) CalculateMeterUsageCharges(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	periodStart, periodEnd time.Time,
	source types.UsageSource,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
	if usage == nil {
		return nil, decimal.Zero, nil
	}

	querySource := source

	asOf := time.Now().UTC()

	// --- Setup: resolve meters, entitlements, customer IDs ---

	commitmentAmount := lo.FromPtr(sub.CommitmentAmount)
	overageFactor := lo.FromPtr(sub.OverageFactor)

	// Cumulative commitment detection
	var useCumulativePath bool
	var totalPriorBase decimal.Decimal
	var commitmentEnd time.Time
	if sub.HasCommitment() && sub.CommitmentDuration != nil &&
		types.BillingPeriod(*sub.CommitmentDuration) != sub.BillingPeriod &&
		commitmentAmount.GreaterThan(decimal.Zero) && overageFactor.GreaterThan(decimal.NewFromInt(1)) {
		cStart, cEnd, ok := getSubscriptionCommitmentPeriodBounds(sub, periodStart)
		if ok {
			commitmentEnd = cEnd
			priorBase, hasPrior, err := s.getCumulativePriorBaseFromInvoices(ctx, sub.ID, cStart, periodStart, overageFactor)
			if err != nil {
				return nil, decimal.Zero, err
			}
			if hasPrior {
				useCumulativePath = true
				totalPriorBase = priorBase
			}
		}
	}

	subscriptionService := NewSubscriptionService(s.ServiceParams)
	aggregatedEntitlements, err := subscriptionService.GetAggregatedSubscriptionEntitlementsForSubscription(ctx, sub, nil)
	if err != nil {
		return nil, decimal.Zero, err
	}
	entitlementsByMeterID := make(map[string]*dto.AggregatedEntitlement)
	for _, feature := range aggregatedEntitlements.Features {
		if feature.Feature != nil && types.FeatureType(feature.Feature.Type) == types.FeatureTypeMetered &&
			feature.Feature.MeterID != "" && feature.Entitlement != nil {
			entitlementsByMeterID[feature.Feature.MeterID] = feature.Entitlement
		}
	}

	// When a line item's meter has grants here, adjustMeterUsageGrants below takes precedence
	// over adjustMeterUsageEntitlement (grants replace legacy entitlement quota semantics for that feature).
	// Empty map means "no grants configured on this subscription" — the loop stays on the legacy path.
	grantsByMeterID, err := s.loadEntitlementGrantsByMeterID(ctx, sub, aggregatedEntitlements.Features, periodStart, periodEnd)
	if err != nil {
		return nil, decimal.Zero, err
	}

	priceService := NewPriceService(s.ServiceParams)

	meterIDs := make([]string, 0)
	for _, item := range sub.LineItems {
		if item.PriceType == types.PRICE_TYPE_USAGE && item.MeterID != "" {
			meterIDs = append(meterIDs, item.MeterID)
		}
	}
	meterIDs = lo.Uniq(meterIDs)

	meters, err := s.MeterRepo.ListByIDs(ctx, meterIDs)
	if err != nil {
		return nil, decimal.Zero, err
	}
	meterMap := make(map[string]*meter.Meter)
	for _, m := range meters {
		meterMap[m.ID] = m
	}

	extCustomerIDs, err := subscriptionService.ExternalCustomerIDsForSubscription(ctx, sub)
	if err != nil {
		return nil, decimal.Zero, err
	}

	// A line item's usage may be split into multiple charges sharing the same
	// SubscriptionLineItemID (e.g. a normal/base-slab charge and an overage charge
	// when commitment pricing is active) — keep all of them, not just the last one.
	chargesByLineItemID := make(map[string][]*dto.SubscriptionUsageByMetersResponse)
	for _, charge := range usage.Charges {
		chargesByLineItemID[charge.SubscriptionLineItemID] = append(chargesByLineItemID[charge.SubscriptionLineItemID], charge)
	}

	// --- Per-line-item processing ---

	usageCharges := make([]dto.CreateInvoiceLineItemRequest, 0)
	baseChargesForCumulative := make([]meterUsageBaseChargeInfo, 0)
	totalUsageCost := decimal.Zero

	for _, item := range sub.LineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}

		// Clip the line's period to elapsed time so a version that has not started yet (or a zero-length EndDate==StartDate leftover)
		// cannot contribute commitment to wallet/invoice totals.
		if _, _, active := elapsedLineItemWindow(item, periodStart, periodEnd, asOf); !active {
			s.Logger.Debug(ctx, "skipping meter-usage line item: item inactive during elapsed window",
				"subscription_id", sub.ID,
				"line_item_id", item.ID,
				"price_id", item.PriceID,
				"invoice_period_start", periodStart,
				"invoice_period_end", periodEnd,
				"item_start_date", item.StartDate,
				"item_end_date", item.EndDate,
				"as_of", asOf,
			)
			continue
		}

		matchingCharges, ok := chargesByLineItemID[item.ID]
		if !ok {
			continue
		}

		m, meterOk := meterMap[item.MeterID]
		if !meterOk {
			return nil, decimal.Zero, ierr.NewError("meter not found").
				WithHint(fmt.Sprintf("Meter with ID %s not found", item.MeterID)).
				WithReportableDetails(map[string]interface{}{"meter_id": item.MeterID}).
				Mark(ierr.ErrNotFound)
		}

		// Accumulated once per LINE ITEM (not per charge) for the cumulative-commitment
		// path below: a base+overage split must contribute exactly one combined entry
		// to baseChargesForCumulative, otherwise the item gets two allocated invoice
		// lines and its contribution is misattributed between them (see step 3).
		var cumulativeBaseAmount decimal.Decimal
		var cumulativeQuantity decimal.Decimal
		var cumulativePriceUnitAmount decimal.Decimal
		var cumulativeDisplayName *string
		var cumulativeMetadata types.Metadata
		var cumulativeAdjustedEntitlementQty *decimal.Decimal
		var cumulativePreferredCharge bool // true once a non-overage charge has set the fields above

		for _, matchingCharge := range matchingCharges {
			quantityForCalculation := matchingCharge.QuantityDecimal()

			// 1. Bucketed meter cost — use pre-fetched result or fallback to direct query
			var cachedBucketedUsageResult *events.AggregationResult
			if matchingCharge.Price != nil && (price.IsBucketedMax(matchingCharge.Price, m) || price.IsBucketedSum(matchingCharge.Price, m)) {
				usageResult := matchingCharge.BucketedUsageResult
				if usageResult == nil {
					usageResult, err = s.queryBucketedMeterUsageDirect(ctx, m, matchingCharge.Price, item, sub, extCustomerIDs, periodStart, periodEnd, querySource)
					if err != nil {
						return nil, decimal.Zero, err
					}
				}
				cachedBucketedUsageResult = usageResult

				hasGroupBy := price.BucketedGroupBy(matchingCharge.Price, m) != ""
				cost := calculateBucketedMeterCost(ctx, priceService, matchingCharge.Price, usageResult, hasGroupBy)
				matchingCharge.SetAmountWithCurrencyPrecision(cost.Amount, matchingCharge.Price.Currency)
				matchingCharge.SetQuantityDecimal(cost.Quantity)
				quantityForCalculation = cost.Quantity
			}

			// 2. Entitlement adjustment — reads windowed usage from meter_usage (not raw events)
			rawQtyBeforeEntitlement := quantityForCalculation
			var entitlementAdjustedQty *decimal.Decimal
			entitlement := entitlementsByMeterID[item.MeterID]

			grantResult, grantsApplied := adjustMeterUsageGrantsResult{}, false
			if !matchingCharge.IsOverage {
				if grantsForMeter := grantsByMeterID[item.MeterID]; len(grantsForMeter) > 0 {
					grantResult, grantsApplied, err = s.adjustMeterUsageGrants(ctx, item, matchingCharge, grantsForMeter, priceService, m, sub, extCustomerIDs)
					if err != nil {
						return nil, decimal.Zero, err
					}
				}
			}

			switch {
			case grantsApplied:
				// Grants replaced the legacy entitlement — adjustMeterUsageGrants
				// already updated matchingCharge (Amount/Quantity). For the
				// pricer, use the grant-derived quantity; amount-lane grants
				// return zero here on purpose (already priced).
				quantityForCalculation = matchingCharge.QuantityDecimal()
				if grantResult.Measure == types.EntitlementGrantMeasureQuantity {
					adj := rawQtyBeforeEntitlement.Sub(quantityForCalculation)
					entitlementAdjustedQty = lo.ToPtr(decimal.Max(adj, decimal.Zero))
				}
			case !matchingCharge.IsOverage && entitlement != nil && entitlement.IsEnabled:
				quantityForCalculation, err = s.adjustMeterUsageEntitlement(
					ctx, item, m, matchingCharge, entitlement, sub, extCustomerIDs,
					periodStart, periodEnd, priceService, querySource,
				)
				if err != nil {
					return nil, decimal.Zero, err
				}
				adj := rawQtyBeforeEntitlement.Sub(quantityForCalculation)
				entitlementAdjustedQty = lo.ToPtr(decimal.Max(adj, decimal.Zero))
			case !matchingCharge.IsOverage && matchingCharge.Price != nil && !price.IsBucketedMax(matchingCharge.Price, m) && !price.IsBucketedSum(matchingCharge.Price, m):
				// No grant, no entitlement — recalculate cost for non-bucketed meters.
				adjustedAmount := priceService.CalculateCost(ctx, matchingCharge.Price, quantityForCalculation)
				matchingCharge.SetAmountWithCurrencyPrecision(adjustedAmount, matchingCharge.Price.Currency)
			}

			lineItemAmount := matchingCharge.AmountDecimal()

			// 3. Cumulative commitment — accumulate into ONE combined entry per line
			// item (appended after the charge loop below), not one per charge.
			if useCumulativePath {
				baseAmount := lineItemAmount
				if matchingCharge.IsOverage && overageFactor.GreaterThan(decimal.Zero) {
					baseAmount = lineItemAmount.Div(overageFactor)
				}
				cumulativeBaseAmount = cumulativeBaseAmount.Add(baseAmount)
				cumulativeQuantity = cumulativeQuantity.Add(quantityForCalculation)
				if item.PriceUnit != nil {
					priceUnit, puErr := s.PriceUnitRepo.GetByCode(ctx, lo.FromPtr(item.PriceUnit))
					if puErr == nil {
						converted, convErr := priceunit.ConvertToPriceUnitAmount(ctx, lineItemAmount, priceUnit.ConversionRate, priceUnit.BaseCurrency)
						if convErr == nil {
							cumulativePriceUnitAmount = cumulativePriceUnitAmount.Add(converted)
						}
					}
				}
				// Prefer the non-overage charge's metadata/display name/entitlement
				// info for the combined line; only an entirely-overage item (no
				// non-overage charge at all) keeps the overage charge's info.
				if !cumulativePreferredCharge || !matchingCharge.IsOverage {
					cumulativeMetadata = s.buildChargeMetadata(item, matchingCharge, entitlement)
					cumulativeDisplayName = lo.ToPtr(item.DisplayName)
					if matchingCharge.IsOverage {
						cumulativeDisplayName = lo.ToPtr(fmt.Sprintf("%s (Overage)", item.DisplayName))
					}
					cumulativeAdjustedEntitlementQty = entitlementAdjustedQty
					cumulativePreferredCharge = !matchingCharge.IsOverage
				}
				continue
			}

			// 4. Line-item commitment (windowed or flat). Applied once per line item:
			// when this item's usage was split into a normal + overage charge, only the
			// non-overage charge goes through it — otherwise a floor/true-up would be
			// enforced independently on each half instead of once for the item. A
			// single (unsplit) charge is unaffected, overage or not.
			var commitmentInfo *types.CommitmentInfo
			applyItemCommitment := item.HasAnyCommitment() && matchingCharge.Price != nil &&
				(!matchingCharge.IsOverage || len(matchingCharges) == 1)
			if applyItemCommitment {
				lineItemAmount, commitmentInfo, err = s.applyMeterUsageCommitment(
					ctx, item, m, matchingCharge, cachedBucketedUsageResult,
					sub, extCustomerIDs, periodStart, periodEnd, asOf,
					priceService, querySource, meterMap,
				)
				if err != nil {
					return nil, decimal.Zero, err
				}
			}

			totalUsageCost = totalUsageCost.Add(lineItemAmount)

			metadata := s.buildChargeMetadata(item, matchingCharge, entitlement)
			displayName := lo.ToPtr(item.DisplayName)
			if matchingCharge.IsOverage {
				displayName = lo.ToPtr(fmt.Sprintf("%s (Overage)", item.DisplayName))
			}

			s.Logger.Debug(ctx, "meter usage charges for line item",
				"amount", matchingCharge.Amount, "quantity", matchingCharge.Quantity,
				"is_overage", matchingCharge.IsOverage,
				"subscription_id", sub.ID, "line_item_id", item.ID, "price_id", item.PriceID)

			var priceUnitAmount decimal.Decimal
			if item.PriceUnit != nil {
				priceUnit, puErr := s.PriceUnitRepo.GetByCode(ctx, lo.FromPtr(item.PriceUnit))
				if puErr != nil {
					return nil, decimal.Zero, puErr
				}
				priceUnitAmount, err = priceunit.ConvertToPriceUnitAmount(ctx, lineItemAmount, priceUnit.ConversionRate, priceUnit.BaseCurrency)
				if err != nil {
					return nil, decimal.Zero, err
				}
			}

			psStart := item.GetPeriodStart(periodStart)
			psEnd := item.GetPeriodEnd(periodEnd)
			usageCharges = append(usageCharges, dto.CreateInvoiceLineItemRequest{
				EntityID:                    lo.ToPtr(item.EntityID),
				EntityType:                  lo.ToPtr(string(item.EntityType)),
				PlanDisplayName:             lo.ToPtr(item.PlanDisplayName),
				PriceType:                   lo.ToPtr(string(item.PriceType)),
				PriceID:                     lo.ToPtr(item.PriceID),
				MeterID:                     lo.ToPtr(item.MeterID),
				MeterDisplayName:            lo.ToPtr(item.MeterDisplayName),
				PriceUnit:                   item.PriceUnit,
				PriceUnitAmount:             lo.ToPtr(priceUnitAmount),
				DisplayName:                 displayName,
				Amount:                      lineItemAmount,
				Quantity:                    quantityForCalculation,
				AdjustedEntitlementQuantity: entitlementAdjustedQty,
				PeriodStart:                 lo.ToPtr(psStart),
				PeriodEnd:                   lo.ToPtr(psEnd),
				SubscriptionLineItemID:      lo.ToPtr(item.ID),
				Metadata:                    metadata,
				CommitmentInfo:              commitmentInfo,
			})
		}

		if useCumulativePath {
			baseChargesForCumulative = append(baseChargesForCumulative, meterUsageBaseChargeInfo{
				item: item, baseAmount: cumulativeBaseAmount,
				quantityForCalculation: cumulativeQuantity, priceUnitAmount: cumulativePriceUnitAmount,
				displayName: cumulativeDisplayName, metadata: cumulativeMetadata,
				adjustedEntitlementQuantity: cumulativeAdjustedEntitlementQty,
			})
		}
	}

	// --- Post-loop: cumulative commitment or non-cumulative true-up ---

	if useCumulativePath {
		return s.buildCumulativeCommitmentCharges(sub, baseChargesForCumulative, usageCharges,
			commitmentAmount, overageFactor, totalPriorBase, commitmentEnd,
			periodStart, periodEnd)
	}

	hasCommitment := commitmentAmount.GreaterThan(decimal.Zero) && overageFactor.GreaterThan(decimal.NewFromInt(1))
	if hasCommitment && !usage.HasOverage && sub.EnableTrueUp {
		remainingCommitment := s.calculateRemainingCommitment(usage, commitmentAmount)
		if remainingCommitment.GreaterThan(decimal.Zero) {
			planDisplayName := s.getPlanDisplayName(sub)
			precision := types.GetCurrencyPrecision(sub.Currency)
			rounded := remainingCommitment.Round(precision)
			utilized := commitmentAmount.Sub(rounded)
			usageCharges = append(usageCharges, dto.CreateInvoiceLineItemRequest{
				EntityID:        lo.ToPtr(sub.PlanID),
				EntityType:      lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
				PriceType:       lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
				PlanDisplayName: lo.ToPtr(planDisplayName),
				DisplayName:     lo.ToPtr(fmt.Sprintf("%s True Up", planDisplayName)),
				Amount:          rounded,
				Quantity:        decimal.NewFromInt(1),
				PeriodStart:     &periodStart,
				PeriodEnd:       &periodEnd,
				PriceID:         lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)),
				Metadata: types.Metadata{
					"is_commitment_trueup": "true",
					"description":          "Remaining commitment amount for billing period",
					"commitment_amount":    commitmentAmount.String(),
					"commitment_utilized":  utilized.String(),
				},
			})
			totalUsageCost = totalUsageCost.Add(rounded)
		}
	}

	return usageCharges, totalUsageCost, nil
}

// adjustMeterUsageEntitlement adjusts quantity for entitlement limits, reading windowed
// usage from meter_usage (not raw events). Handles bucketed and non-bucketed meters,
// and all reset periods (billing-period, daily, monthly, never).
// Returns the adjusted billable quantity. Mutates matchingCharge.Amount as a side effect.
func (s *billingService) adjustMeterUsageEntitlement(
	ctx context.Context,
	item *subscription.SubscriptionLineItem,
	m *meter.Meter,
	matchingCharge *dto.SubscriptionUsageByMetersResponse,
	ent *dto.AggregatedEntitlement,
	sub *subscription.Subscription,
	extCustomerIDs []string,
	periodStart, periodEnd time.Time,
	priceService PriceService,
	querySource types.UsageSource,
) (decimal.Decimal, error) {
	quantity := matchingCharge.QuantityDecimal()

	// Bucketed meters: simple limit subtraction
	if price.IsBucketedMax(matchingCharge.Price, m) || price.IsBucketedSum(matchingCharge.Price, m) {
		if ent.UsageLimit != nil {
			allowed := decimal.NewFromFloat(float64(*ent.UsageLimit))
			adjusted := decimal.Max(quantity.Sub(allowed), decimal.Zero)
			if !adjusted.Equal(quantity) && matchingCharge.Price != nil {
				matchingCharge.SetAmountWithCurrencyPrecision(
					priceService.CalculateCost(ctx, matchingCharge.Price, adjusted), matchingCharge.Price.Currency)
			}
			return adjusted, nil
		}
		matchingCharge.SetAmountDecimal(decimal.Zero)
		return decimal.Zero, nil
	}

	// Non-bucketed meters
	if ent.UsageLimit == nil {
		matchingCharge.SetAmountDecimal(decimal.Zero)
		return decimal.Zero, nil
	}

	allowed := decimal.NewFromFloat(float64(*ent.UsageLimit))
	itemStart := item.GetPeriodStart(periodStart)
	itemEnd := item.GetPeriodEnd(periodEnd)

	var adjusted decimal.Decimal

	switch ent.UsageResetPeriod {
	case types.EntitlementUsageResetPeriod(sub.BillingPeriod):
		// Simple subtraction — same reset period as billing
		adjusted = decimal.Max(quantity.Sub(allowed), decimal.Zero)

	case types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY:
		result, err := s.MeterUsageRepo.GetUsage(ctx, &events.MeterUsageQueryParams{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerIDs: extCustomerIDs,
			MeterID:             item.MeterID,
			StartTime:           itemStart,
			EndTime:             itemEnd,
			AggregationType:     m.Aggregation.Type,
			WindowSize:          types.WindowSizeDay,
			UseFinal:            querySource.UseFinal(),
		})
		if err != nil {
			return decimal.Zero, err
		}
		adjusted = s.sumWindowedOverage(result.Points, allowed)

	case types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY:
		result, err := s.MeterUsageRepo.GetUsage(ctx, &events.MeterUsageQueryParams{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerIDs: extCustomerIDs,
			MeterID:             item.MeterID,
			StartTime:           itemStart,
			EndTime:             itemEnd,
			AggregationType:     m.Aggregation.Type,
			WindowSize:          types.WindowSizeMonth,
			BillingAnchor:       &sub.BillingAnchor,
			UseFinal:            querySource.UseFinal(),
		})
		if err != nil {
			return decimal.Zero, err
		}
		adjusted = s.sumWindowedOverage(result.Points, allowed)

	case types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER:
		adjusted = decimal.Zero
		totalResult, err := s.MeterUsageRepo.GetUsage(ctx, &events.MeterUsageQueryParams{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerIDs: extCustomerIDs,
			MeterID:             item.MeterID,
			StartTime:           sub.StartDate,
			EndTime:             itemEnd,
			AggregationType:     m.Aggregation.Type,
			UseFinal:            querySource.UseFinal(),
		})
		if err != nil {
			return decimal.Zero, err
		}
		prevResult, err := s.MeterUsageRepo.GetUsage(ctx, &events.MeterUsageQueryParams{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerIDs: extCustomerIDs,
			MeterID:             item.MeterID,
			StartTime:           sub.StartDate,
			EndTime:             itemStart,
			AggregationType:     m.Aggregation.Type,
			UseFinal:            querySource.UseFinal(),
		})
		if err != nil {
			return decimal.Zero, err
		}
		periodUsage := totalResult.TotalValue.Sub(prevResult.TotalValue)
		adjusted = decimal.Max(periodUsage.Sub(allowed), decimal.Zero)

	default:
		adjusted = decimal.Max(quantity.Sub(allowed), decimal.Zero)
	}

	if matchingCharge.Price != nil {
		matchingCharge.SetAmountWithCurrencyPrecision(
			priceService.CalculateCost(ctx, matchingCharge.Price, adjusted), matchingCharge.Price.Currency)
	}
	return adjusted, nil
}

// sumWindowedOverage computes total overage across time windows: sum(max(0, window_value - limit)).
func (s *billingService) sumWindowedOverage(points []events.MeterUsageResult, limit decimal.Decimal) decimal.Decimal {
	total := decimal.Zero
	for _, p := range points {
		overage := p.Value.Sub(limit)
		if overage.GreaterThan(decimal.Zero) {
			total = total.Add(overage)
		}
	}
	return total
}

// applyMeterUsageCommitment handles line-item commitment (windowed or flat).
// Returns the adjusted line item amount and commitment info.
func (s *billingService) applyMeterUsageCommitment(
	ctx context.Context,
	item *subscription.SubscriptionLineItem,
	m *meter.Meter,
	matchingCharge *dto.SubscriptionUsageByMetersResponse,
	cachedBucketedResult *events.AggregationResult,
	sub *subscription.Subscription,
	extCustomerIDs []string,
	periodStart, periodEnd time.Time,
	asOf time.Time,
	priceService PriceService,
	querySource types.UsageSource,
	meterMap map[string]*meter.Meter,
) (decimal.Decimal, *types.CommitmentInfo, error) {
	lineItemAmount := matchingCharge.AmountDecimal()
	commitmentCalc := newCommitmentCalculator(s.Logger, priceService)

	if !item.CommitmentWindowed {
		adjustedAmount, info, err := commitmentCalc.applyCommitmentToLineItem(ctx, item, lineItemAmount, matchingCharge.Price)
		if err != nil {
			return decimal.Zero, nil, err
		}
		adjustedAmount = types.RoundToCurrencyPrecision(adjustedAmount, matchingCharge.Price.Currency)
		matchingCharge.SetAmountDecimal(adjustedAmount)
		return adjustedAmount, info, nil
	}

	// Windowed commitment — needs bucketed values from meter_usage
	linePeriodStart := item.GetPeriodStart(periodStart)
	linePeriodEnd := item.GetPeriodEnd(periodEnd)
	if asOf.Before(linePeriodStart) {
		return decimal.Zero, nil, nil
	}
	effectiveEnd := asOf
	if effectiveEnd.After(linePeriodEnd) {
		effectiveEnd = linePeriodEnd
	}

	usageResult := cachedBucketedResult
	if usageResult == nil {
		var err error
		usageResult, err = s.queryBucketedMeterUsageDirect(ctx, m, matchingCharge.Price, item, sub, extCustomerIDs, periodStart, periodEnd, querySource)
		if err != nil {
			return decimal.Zero, nil, err
		}
	}

	bucketedValues, bucketStarts := s.fillBucketedValuesForWindowedCommitment(
		item, usageResult, linePeriodStart, effectiveEnd,
		price.ResolveBucketSize(matchingCharge.Price, m), &sub.BillingAnchor, m.Aggregation.Type,
	)

	adjustedAmount, info, err := commitmentCalc.applyWindowCommitmentToLineItem(ctx, item, bucketedValues, bucketStarts, matchingCharge.Price)
	if err != nil {
		return decimal.Zero, nil, err
	}
	adjustedAmount = types.RoundToCurrencyPrecision(adjustedAmount, matchingCharge.Price.Currency)
	matchingCharge.SetAmountDecimal(adjustedAmount)
	return adjustedAmount, info, nil
}

// queryBucketedMeterUsageDirect is a fallback that queries bucketed meter usage directly
// when the pre-fetched BucketedUsageResult is not available on the charge.
func (s *billingService) queryBucketedMeterUsageDirect(
	ctx context.Context,
	m *meter.Meter,
	p *price.Price,
	item *subscription.SubscriptionLineItem,
	sub *subscription.Subscription,
	extCustomerIDs []string,
	periodStart, periodEnd time.Time,
	querySource types.UsageSource,
) (*events.AggregationResult, error) {
	aggType := types.AggregationMax
	groupBy := price.BucketedGroupBy(p, m)
	if price.IsBucketedSum(p, m) {
		aggType = types.AggregationSum
		groupBy = ""
	}
	// Translate meter-level Aggregation.GroupBy (a single property name) to the
	// unified GroupBy []string convention: "properties.<X>".
	var paramsGroupBy []string
	if groupBy != "" {
		paramsGroupBy = []string{"properties." + groupBy}
	}
	return s.MeterUsageRepo.GetUsageForBucketedMeters(ctx, &events.MeterUsageQueryParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: extCustomerIDs,
		MeterID:             item.MeterID,
		StartTime:           item.GetPeriodStart(periodStart),
		EndTime:             item.GetPeriodEnd(periodEnd),
		AggregationType:     aggType,
		WindowSize:          price.ResolveBucketSize(p, m),
		BillingAnchor:       &sub.BillingAnchor,
		GroupBy:             paramsGroupBy,
		UseFinal:            querySource.UseFinal(),
	})
}

// buildChargeMetadata creates standard metadata for a usage charge line item.
func (s *billingService) buildChargeMetadata(
	item *subscription.SubscriptionLineItem,
	charge *dto.SubscriptionUsageByMetersResponse,
	entitlement *dto.AggregatedEntitlement,
) types.Metadata {
	metadata := types.Metadata{
		"description": fmt.Sprintf("%s (Usage Charge)", item.DisplayName),
	}
	if charge.IsOverage {
		metadata["is_overage"] = "true"
		metadata["overage_factor"] = fmt.Sprintf("%v", charge.OverageFactor)
		metadata["description"] = fmt.Sprintf("%s (Overage Charge)", item.DisplayName)
	}
	if !charge.IsOverage && entitlement != nil && entitlement.IsEnabled {
		switch entitlement.UsageResetPeriod {
		case types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY:
			metadata["usage_reset_period"] = "daily"
		case types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY:
			metadata["usage_reset_period"] = "monthly"
		case types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER:
			metadata["usage_reset_period"] = "never"
		}
	}
	return metadata
}

// getPlanDisplayName extracts the plan display name from subscription line items.
func (s *billingService) getPlanDisplayName(sub *subscription.Subscription) string {
	for _, item := range sub.LineItems {
		if item.PlanDisplayName != "" {
			return item.PlanDisplayName
		}
	}
	return ""
}

// buildCumulativeCommitmentCharges processes accumulated base charges through the
// cumulative commitment allocation logic, producing the final invoice line items
// including within-commitment, overage, and true-up charges.
func (s *billingService) buildCumulativeCommitmentCharges(
	sub *subscription.Subscription,
	baseCharges []meterUsageBaseChargeInfo,
	existingCharges []dto.CreateInvoiceLineItemRequest,
	commitmentAmount, overageFactor, totalPriorBase decimal.Decimal,
	commitmentEnd time.Time,
	periodStart, periodEnd time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
	totalCurrentBase := decimal.Zero
	for _, bc := range baseCharges {
		totalCurrentBase = totalCurrentBase.Add(bc.baseAmount)
	}

	isLastPeriod := isLastPeriodOfCommitmentPeriod(periodEnd, commitmentEnd)
	result := applyCumulativeSubscriptionCommitment(
		commitmentAmount, overageFactor, totalCurrentBase, totalPriorBase,
		sub.EnableTrueUp, isLastPeriod, s.Logger,
	)

	charges := existingCharges
	totalCost := decimal.Zero

	for _, bc := range baseCharges {
		// Skip items that weren't active during the invoice window
		psStart := bc.item.GetPeriodStart(periodStart)
		psEnd := bc.item.GetPeriodEnd(periodEnd)
		if !psEnd.After(psStart) {
			s.Logger.Debug(context.Background(), "skipping cumulative-commitment line item: item inactive during invoice window",
				"subscription_id", sub.ID,
				"line_item_id", bc.item.ID,
				"price_id", bc.item.PriceID,
				"invoice_period_start", periodStart,
				"invoice_period_end", periodEnd,
				"item_start_date", bc.item.StartDate,
				"item_end_date", bc.item.EndDate,
			)
			continue
		}
		var allocatedAmount decimal.Decimal
		if totalCurrentBase.GreaterThan(decimal.Zero) {
			allocatedAmount = bc.baseAmount.Div(totalCurrentBase).Mul(result.WithinCommitment)
		}
		rounded := types.RoundToCurrencyPrecision(allocatedAmount, sub.Currency)
		displayQty := bc.quantityForCalculation
		if bc.baseAmount.GreaterThan(decimal.Zero) {
			displayQty = bc.quantityForCalculation.Mul(allocatedAmount).Div(bc.baseAmount)
		}
		displayQty = types.RoundToCurrencyPrecision(displayQty, sub.Currency)
		charges = append(charges, dto.CreateInvoiceLineItemRequest{
			EntityID:                    lo.ToPtr(bc.item.EntityID),
			EntityType:                  lo.ToPtr(string(bc.item.EntityType)),
			PlanDisplayName:             lo.ToPtr(bc.item.PlanDisplayName),
			PriceType:                   lo.ToPtr(string(bc.item.PriceType)),
			PriceID:                     lo.ToPtr(bc.item.PriceID),
			MeterID:                     lo.ToPtr(bc.item.MeterID),
			MeterDisplayName:            lo.ToPtr(bc.item.MeterDisplayName),
			PriceUnit:                   bc.item.PriceUnit,
			PriceUnitAmount:             lo.ToPtr(bc.priceUnitAmount),
			DisplayName:                 bc.displayName,
			Amount:                      rounded,
			Quantity:                    displayQty,
			AdjustedEntitlementQuantity: bc.adjustedEntitlementQuantity,
			PeriodStart:                 lo.ToPtr(psStart),
			PeriodEnd:                   lo.ToPtr(psEnd),
			SubscriptionLineItemID:      lo.ToPtr(bc.item.ID),
			Metadata:                    bc.metadata,
		})
		totalCost = totalCost.Add(rounded)
	}

	planDisplayName := s.getPlanDisplayName(sub)

	if result.OverageAmount.GreaterThan(decimal.Zero) {
		rounded := types.RoundToCurrencyPrecision(result.OverageAmount, sub.Currency)
		overageQty := types.RoundToCurrencyPrecision(result.OverageBase, sub.Currency)
		charges = append(charges, dto.CreateInvoiceLineItemRequest{
			EntityID:        lo.ToPtr(sub.PlanID),
			EntityType:      lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
			PlanDisplayName: lo.ToPtr(planDisplayName),
			PriceType:       lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
			DisplayName:     lo.ToPtr(fmt.Sprintf("%s Overage", planDisplayName)),
			Amount:          rounded,
			Quantity:        overageQty,
			PeriodStart:     &periodStart,
			PeriodEnd:       &periodEnd,
			PriceID:         lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)),
			Metadata: types.Metadata{
				"is_overage":     "true",
				"overage_factor": overageFactor.String(),
				"description":    "Overage charge (cumulative commitment)",
			},
		})
		totalCost = totalCost.Add(rounded)
	}

	if result.TrueUpAmount.GreaterThan(decimal.Zero) {
		rounded := types.RoundToCurrencyPrecision(result.TrueUpAmount, sub.Currency)
		charges = append(charges, dto.CreateInvoiceLineItemRequest{
			EntityID:        lo.ToPtr(sub.PlanID),
			EntityType:      lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
			PriceType:       lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
			PlanDisplayName: lo.ToPtr(planDisplayName),
			DisplayName:     lo.ToPtr(fmt.Sprintf("%s True Up", planDisplayName)),
			Amount:          rounded,
			Quantity:        decimal.NewFromInt(1),
			PeriodStart:     &periodStart,
			PeriodEnd:       &periodEnd,
			PriceID:         lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)),
			Metadata: types.Metadata{
				"is_commitment_trueup": "true",
				"description":          "Remaining commitment amount for commitment period",
				"commitment_amount":    commitmentAmount.String(),
				"commitment_utilized":  result.CommitmentUtilized.String(),
			},
		})
		totalCost = totalCost.Add(rounded)
	}

	return charges, totalCost, nil
}

// calculateAllMeterUsageCharges computes fixed + usage charges, routing all queries
// through MeterUsageRepo.
func (s *billingService) calculateAllMeterUsageCharges(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	periodStart, periodEnd time.Time,
) (*dto.BillingCalculationResult, error) {
	fixedResult, err := s.CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{
		Subscription: sub,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
	})
	if err != nil {
		return nil, err
	}

	usageCharges, usageTotal, err := s.CalculateMeterUsageCharges(ctx, sub, usage, periodStart, periodEnd,
		types.UsageSourceInvoiceCreation)
	if err != nil {
		return nil, err
	}

	return &dto.BillingCalculationResult{
		FixedCharges: fixedResult.LineItems,
		UsageCharges: usageCharges,
		TotalAmount:  fixedResult.TotalAmount.Add(usageTotal),
		Currency:     sub.Currency,
	}, nil
}
