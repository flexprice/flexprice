package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
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
	item                   *subscription.SubscriptionLineItem
	matchingCharge         *dto.SubscriptionUsageByMetersResponse
	baseAmount             decimal.Decimal
	quantityForCalculation decimal.Decimal
	priceUnitAmount        decimal.Decimal
	displayName            *string
	metadata               types.Metadata
}

// CalculateMeterUsageCharges computes usage-based invoice line items from the meter_usage table.
// Unlike CalculateFeatureUsageCharges, all queries (bucketed meters, windowed entitlements,
// windowed commitments) read from MeterUsageRepo — never from raw events or feature_usage.
func (s *billingService) CalculateMeterUsageCharges(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	periodStart, periodEnd time.Time,
	opts *CalculateFeatureUsageChargesOpts,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
	if usage == nil {
		return nil, decimal.Zero, nil
	}

	var querySource types.UsageSource
	if opts != nil {
		querySource = opts.Source
	}

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
	aggregatedEntitlements, err := subscriptionService.GetAggregatedSubscriptionEntitlements(ctx, sub.ID, nil)
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

	priceService := NewPriceService(s.ServiceParams)

	meterIDs := make([]string, 0)
	for _, item := range sub.LineItems {
		if item.PriceType == types.PRICE_TYPE_USAGE && item.MeterID != "" {
			meterIDs = append(meterIDs, item.MeterID)
		}
	}
	meterIDs = lo.Uniq(meterIDs)

	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.MeterIDs = meterIDs
	meters, err := s.MeterRepo.List(ctx, meterFilter)
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

	chargesByLineItemID := make(map[string]*dto.SubscriptionUsageByMetersResponse)
	for _, charge := range usage.Charges {
		chargesByLineItemID[charge.SubscriptionLineItemID] = charge
	}

	// --- Per-line-item processing ---

	usageCharges := make([]dto.CreateInvoiceLineItemRequest, 0)
	baseChargesForCumulative := make([]meterUsageBaseChargeInfo, 0)
	totalUsageCost := decimal.Zero

	for _, item := range sub.LineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}

		matchingCharge, ok := chargesByLineItemID[item.ID]
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

		quantityForCalculation := decimal.NewFromFloat(matchingCharge.Quantity)

		// 1. Bucketed meter cost — use pre-fetched result or fallback to direct query
		var cachedBucketedUsageResult *events.AggregationResult
		if (m.IsBucketedMaxMeter() || m.IsBucketedSumMeter()) && matchingCharge.Price != nil {
			usageResult := matchingCharge.BucketedUsageResult
			if usageResult == nil {
				usageResult, err = s.queryBucketedMeterUsageDirect(ctx, m, item, sub, extCustomerIDs, periodStart, periodEnd, querySource)
				if err != nil {
					return nil, decimal.Zero, err
				}
			}
			cachedBucketedUsageResult = usageResult

			hasGroupBy := m.IsBucketedMaxMeter() && m.Aggregation.GroupBy != ""
			cost := calculateBucketedMeterCost(ctx, priceService, matchingCharge.Price, usageResult, hasGroupBy)
			matchingCharge.Amount = priceDomain.FormatAmountToFloat64WithPrecision(cost.Amount, matchingCharge.Price.Currency)
			matchingCharge.Quantity = cost.Quantity.InexactFloat64()
			quantityForCalculation = cost.Quantity
		}

		// 2. Entitlement adjustment — reads windowed usage from meter_usage (not raw events)
		entitlement := entitlementsByMeterID[item.MeterID]
		if !matchingCharge.IsOverage && entitlement != nil && entitlement.IsEnabled {
			quantityForCalculation, err = s.adjustMeterUsageEntitlement(
				ctx, item, m, matchingCharge, entitlement, sub, extCustomerIDs,
				periodStart, periodEnd, priceService, querySource,
			)
			if err != nil {
				return nil, decimal.Zero, err
			}
		} else if !matchingCharge.IsOverage && !m.IsBucketedMaxMeter() && !m.IsBucketedSumMeter() && matchingCharge.Price != nil {
			// No entitlement — recalculate cost for non-bucketed meters
			adjustedAmount := priceService.CalculateCost(ctx, matchingCharge.Price, quantityForCalculation)
			matchingCharge.Amount = priceDomain.FormatAmountToFloat64WithPrecision(adjustedAmount, matchingCharge.Price.Currency)
		}

		lineItemAmount := decimal.NewFromFloat(matchingCharge.Amount)

		// 3. Cumulative commitment — collect for batch processing after the loop
		if useCumulativePath {
			baseAmount := lineItemAmount
			if matchingCharge.IsOverage && overageFactor.GreaterThan(decimal.Zero) {
				baseAmount = lineItemAmount.Div(overageFactor)
			}
			metadata := s.buildChargeMetadata(item, matchingCharge, entitlement)
			displayName := lo.ToPtr(item.DisplayName)
			if matchingCharge.IsOverage {
				displayName = lo.ToPtr(fmt.Sprintf("%s (Overage)", item.DisplayName))
			}
			var priceUnitAmount decimal.Decimal
			if item.PriceUnit != nil {
				priceUnit, puErr := s.PriceUnitRepo.GetByCode(ctx, lo.FromPtr(item.PriceUnit))
				if puErr == nil {
					converted, convErr := priceunit.ConvertToPriceUnitAmount(ctx, lineItemAmount, priceUnit.ConversionRate, priceUnit.BaseCurrency)
					if convErr == nil {
						priceUnitAmount = converted
					}
				}
			}
			baseChargesForCumulative = append(baseChargesForCumulative, meterUsageBaseChargeInfo{
				item: item, matchingCharge: matchingCharge, baseAmount: baseAmount,
				quantityForCalculation: quantityForCalculation, priceUnitAmount: priceUnitAmount,
				displayName: displayName, metadata: metadata,
			})
			continue
		}

		// 4. Line-item commitment (windowed or flat)
		var commitmentInfo *types.CommitmentInfo
		if item.HasCommitment() && matchingCharge.Price != nil {
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

		s.Logger.Debugw("meter usage charges for line item",
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

		usageCharges = append(usageCharges, dto.CreateInvoiceLineItemRequest{
			EntityID:         lo.ToPtr(item.EntityID),
			EntityType:       lo.ToPtr(string(item.EntityType)),
			PlanDisplayName:  lo.ToPtr(item.PlanDisplayName),
			PriceType:        lo.ToPtr(string(item.PriceType)),
			PriceID:          lo.ToPtr(item.PriceID),
			MeterID:          lo.ToPtr(item.MeterID),
			MeterDisplayName: lo.ToPtr(item.MeterDisplayName),
			PriceUnit:        item.PriceUnit,
			PriceUnitAmount:  lo.ToPtr(priceUnitAmount),
			DisplayName:      displayName,
			Amount:           lineItemAmount,
			Quantity:         quantityForCalculation,
			PeriodStart:      lo.ToPtr(item.GetPeriodStart(periodStart)),
			PeriodEnd:        lo.ToPtr(item.GetPeriodEnd(periodEnd)),
			Metadata:         metadata,
			CommitmentInfo:   commitmentInfo,
		})
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
	quantity := decimal.NewFromFloat(matchingCharge.Quantity)

	// Bucketed meters: simple limit subtraction
	if m.IsBucketedMaxMeter() || m.IsBucketedSumMeter() {
		if ent.UsageLimit != nil {
			allowed := decimal.NewFromFloat(float64(*ent.UsageLimit))
			adjusted := decimal.Max(quantity.Sub(allowed), decimal.Zero)
			if !adjusted.Equal(quantity) && matchingCharge.Price != nil {
				matchingCharge.Amount = priceDomain.FormatAmountToFloat64WithPrecision(
					priceService.CalculateCost(ctx, matchingCharge.Price, adjusted), matchingCharge.Price.Currency)
			}
			return adjusted, nil
		}
		matchingCharge.Amount = 0
		return decimal.Zero, nil
	}

	// Non-bucketed meters
	if ent.UsageLimit == nil {
		matchingCharge.Amount = 0
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
		matchingCharge.Amount = priceDomain.FormatAmountToFloat64WithPrecision(
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
	lineItemAmount := decimal.NewFromFloat(matchingCharge.Amount)
	commitmentCalc := newCommitmentCalculator(s.Logger, priceService)

	if !item.CommitmentWindowed {
		adjustedAmount, info, err := commitmentCalc.applyCommitmentToLineItem(ctx, item, lineItemAmount, matchingCharge.Price)
		if err != nil {
			return decimal.Zero, nil, err
		}
		matchingCharge.Amount = adjustedAmount.InexactFloat64()
		return adjustedAmount, info, nil
	}

	// Windowed commitment — needs bucketed values from meter_usage
	linePeriodStart := item.GetPeriodStart(periodStart)
	linePeriodEnd := item.GetPeriodEnd(periodEnd)
	effectiveEnd := asOf
	if effectiveEnd.Before(linePeriodStart) {
		effectiveEnd = linePeriodStart
	}
	if effectiveEnd.After(linePeriodEnd) {
		effectiveEnd = linePeriodEnd
	}

	usageResult := cachedBucketedResult
	if usageResult == nil {
		var err error
		usageResult, err = s.queryBucketedMeterUsageDirect(ctx, m, item, sub, extCustomerIDs, periodStart, periodEnd, querySource)
		if err != nil {
			return decimal.Zero, nil, err
		}
	}

	bucketedValues := s.fillBucketedValuesForWindowedCommitment(
		item, usageResult, linePeriodStart, effectiveEnd,
		m.Aggregation.BucketSize, &sub.BillingAnchor, m.Aggregation.Type,
	)

	adjustedAmount, info, err := commitmentCalc.applyWindowCommitmentToLineItem(ctx, item, bucketedValues, matchingCharge.Price)
	if err != nil {
		return decimal.Zero, nil, err
	}
	matchingCharge.Amount = adjustedAmount.InexactFloat64()
	return adjustedAmount, info, nil
}

// queryBucketedMeterUsageDirect is a fallback that queries bucketed meter usage directly
// when the pre-fetched BucketedUsageResult is not available on the charge.
func (s *billingService) queryBucketedMeterUsageDirect(
	ctx context.Context,
	m *meter.Meter,
	item *subscription.SubscriptionLineItem,
	sub *subscription.Subscription,
	extCustomerIDs []string,
	periodStart, periodEnd time.Time,
	querySource types.UsageSource,
) (*events.AggregationResult, error) {
	aggType := types.AggregationMax
	groupBy := m.Aggregation.GroupBy
	if m.IsBucketedSumMeter() {
		aggType = types.AggregationSum
		groupBy = ""
	}
	return s.MeterUsageRepo.GetUsageForBucketedMeters(ctx, &events.MeterUsageQueryParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: extCustomerIDs,
		MeterID:             item.MeterID,
		StartTime:           item.GetPeriodStart(periodStart),
		EndTime:             item.GetPeriodEnd(periodEnd),
		AggregationType:     aggType,
		WindowSize:          m.Aggregation.BucketSize,
		BillingAnchor:       &sub.BillingAnchor,
		GroupByProperty:     groupBy,
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
			EntityID:         lo.ToPtr(bc.item.EntityID),
			EntityType:       lo.ToPtr(string(bc.item.EntityType)),
			PlanDisplayName:  lo.ToPtr(bc.item.PlanDisplayName),
			PriceType:        lo.ToPtr(string(bc.item.PriceType)),
			PriceID:          lo.ToPtr(bc.item.PriceID),
			MeterID:          lo.ToPtr(bc.item.MeterID),
			MeterDisplayName: lo.ToPtr(bc.item.MeterDisplayName),
			PriceUnit:        bc.item.PriceUnit,
			PriceUnitAmount:  lo.ToPtr(bc.priceUnitAmount),
			DisplayName:      bc.displayName,
			Amount:           rounded,
			Quantity:         displayQty,
			PeriodStart:      lo.ToPtr(bc.item.GetPeriodStart(periodStart)),
			PeriodEnd:        lo.ToPtr(bc.item.GetPeriodEnd(periodEnd)),
			Metadata:         bc.metadata,
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
	params CalculateAllChargesParams,
) (*BillingCalculationResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}
	sub := params.Subscription
	fixedCharges, fixedTotal, err := s.CalculateFixedCharges(ctx, CalculateFixedChargesParams{
		Subscription:          sub,
		PeriodStart:           params.PeriodStart,
		PeriodEnd:             params.PeriodEnd,
		FixedChargeAdjustment: params.FixedChargeAdjustment,
	})
	if err != nil {
		return nil, err
	}

	usageCharges, usageTotal, err := s.CalculateMeterUsageCharges(ctx, sub, params.Usage, params.PeriodStart, params.PeriodEnd,
		&CalculateFeatureUsageChargesOpts{Source: types.UsageSourceInvoiceCreation})
	if err != nil {
		return nil, err
	}

	return &BillingCalculationResult{
		FixedCharges: fixedCharges,
		UsageCharges: usageCharges,
		TotalAmount:  fixedTotal.Add(usageTotal),
		Currency:     sub.Currency,
	}, nil
}
