package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// BillingCalculationResult holds all calculated charges for a billing period
type BillingCalculationResult struct {
	FixedCharges []dto.CreateInvoiceLineItemRequest
	UsageCharges []dto.CreateInvoiceLineItemRequest
	TotalAmount  decimal.Decimal
	Currency     string
}

// LineItemClassification represents the classification of line items based on cadence and type
type LineItemClassification struct {
	CurrentPeriodAdvance []*subscription.SubscriptionLineItem
	CurrentPeriodArrear  []*subscription.SubscriptionLineItem
	NextPeriodAdvance    []*subscription.SubscriptionLineItem
	HasUsageCharges      bool
}

// BillingService handles all billing calculations
type BillingService interface {
	// CalculateFixedCharges calculates all fixed charges for a subscription
	CalculateFixedCharges(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error)

	// CalculateUsageCharges calculates all usage-based charges
	CalculateUsageCharges(ctx context.Context, sub *subscription.Subscription, usage *dto.GetUsageBySubscriptionResponse, periodStart, periodEnd time.Time) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error)

	// CalculateAllCharges calculates both fixed and usage charges
	CalculateAllCharges(ctx context.Context, sub *subscription.Subscription, usage *dto.GetUsageBySubscriptionResponse, periodStart, periodEnd time.Time) (*BillingCalculationResult, error)

	// PrepareSubscriptionInvoiceRequest prepares a complete invoice request for a subscription period
	// using the reference point to determine which charges to include
	PrepareSubscriptionInvoiceRequest(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time, referencePoint types.InvoiceReferencePoint) (*dto.CreateInvoiceRequest, error)

	// ClassifyLineItems classifies line items based on cadence and type
	ClassifyLineItems(sub *subscription.Subscription, currentPeriodStart, currentPeriodEnd time.Time, nextPeriodStart, nextPeriodEnd time.Time) *LineItemClassification

	// FilterLineItemsToBeInvoiced filters the line items to be invoiced for the given period
	FilterLineItemsToBeInvoiced(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time, lineItems []*subscription.SubscriptionLineItem) ([]*subscription.SubscriptionLineItem, error)

	// CalculateCharges calculates charges for the given line items and period
	CalculateCharges(ctx context.Context, sub *subscription.Subscription, lineItems []*subscription.SubscriptionLineItem, periodStart, periodEnd time.Time, includeUsage bool) (*BillingCalculationResult, error)

	// CreateInvoiceRequestForCharges creates an invoice creation request for the given charges
	CreateInvoiceRequestForCharges(ctx context.Context, sub *subscription.Subscription, result *BillingCalculationResult, periodStart, periodEnd time.Time, description string, metadata types.Metadata) (*dto.CreateInvoiceRequest, error)

	// GetCustomerEntitlements returns aggregated entitlements for a customer across all subscriptions
	GetCustomerEntitlements(ctx context.Context, customerID string, req *dto.GetCustomerEntitlementsRequest) (*dto.CustomerEntitlementsResponse, error)

	// AggregateEntitlements aggregates entitlements from multiple sources into a unified view
	// If subscriptionID is provided, it will be used for sources that don't have a subscription ID set
	AggregateEntitlements(entitlements []*dto.EntitlementResponse, subscriptionID string) []*dto.AggregatedFeature

	// GetCustomerUsageSummary returns usage summaries for a customer's features
	GetCustomerUsageSummary(ctx context.Context, customerID string, req *dto.GetCustomerUsageSummaryRequest) (*dto.CustomerUsageSummaryResponse, error)
}

type billingService struct {
	ServiceParams
}

func NewBillingService(params ServiceParams) BillingService {
	return &billingService{
		ServiceParams: params,
	}
}

func (s *billingService) CalculateFixedCharges(
	ctx context.Context,
	sub *subscription.Subscription,
	periodStart,
	periodEnd time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
	fixedCost := decimal.Zero
	fixedCostLineItems := make([]dto.CreateInvoiceLineItemRequest, 0)

	priceService := NewPriceService(s.ServiceParams)

	// Process fixed charges from line items
	for _, item := range sub.LineItems {
		if item.PriceType != types.PRICE_TYPE_FIXED {
			continue
		}

		// skip if the line item start date is after the period end
		if item.StartDate.After(periodEnd) {
			s.Logger.Debugw("skipping fixed charge line item because it starts after the period end",
				"subscription_id", sub.ID,
				"line_item_id", item.ID,
				"price_id", item.PriceID,
				"start_date", item.StartDate,
				"period_end", periodEnd)
			continue
		}

		price, err := priceService.GetPrice(ctx, item.PriceID)
		if err != nil {
			return nil, fixedCost, err
		}

		amount := priceService.CalculateCost(ctx, price.Price, item.Quantity)

		// Apply proration if applicable
		proratedAmount, err := s.applyProrationToLineItem(ctx, sub, item, price.Price, amount, &periodStart, &periodEnd)
		if err != nil {
			s.Logger.Warnw("failed to apply proration to line item, using original amount",
				"error", err,
				"subscription_id", sub.ID,
				"line_item_id", item.ID,
				"price_id", item.PriceID)
			proratedAmount = amount
		}
		amount = proratedAmount

		// Calculate price unit amount if price unit is available
		var priceUnitAmount *decimal.Decimal
		if item.PriceUnit != "" {
			convertedAmount, err := s.PriceUnitRepo.ConvertToPriceUnit(ctx, item.PriceUnit, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), amount)
			if err != nil {
				s.Logger.Warnw("failed to convert amount to price unit",
					"error", err,
					"price_unit", item.PriceUnit,
					"amount", amount)
			} else {
				priceUnitAmount = &convertedAmount
			}
		}

		fixedCostLineItems = append(fixedCostLineItems, dto.CreateInvoiceLineItemRequest{
			EntityID:        lo.ToPtr(item.EntityID),
			EntityType:      lo.ToPtr(string(item.EntityType)),
			PlanDisplayName: lo.ToPtr(item.PlanDisplayName),
			PriceID:         lo.ToPtr(item.PriceID),
			PriceType:       lo.ToPtr(string(item.PriceType)),
			PriceUnit:       lo.ToPtr(item.PriceUnit),
			PriceUnitAmount: priceUnitAmount,
			DisplayName:     lo.ToPtr(item.DisplayName),
			Amount:          amount,
			Quantity:        item.Quantity,
			PeriodStart:     lo.ToPtr(periodStart),
			PeriodEnd:       lo.ToPtr(periodEnd),
			Metadata: types.Metadata{
				"description": fmt.Sprintf("%s (Fixed Charge)", item.DisplayName),
			},
		})

		fixedCost = fixedCost.Add(amount)
	}

	return fixedCostLineItems, fixedCost, nil
}

func (s *billingService) CalculateUsageCharges(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	periodStart,
	periodEnd time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {

	if usage == nil {
		return nil, decimal.Zero, nil
	}

	usageCharges := make([]dto.CreateInvoiceLineItemRequest, 0)
	totalUsageCost := decimal.Zero

	// Use subscription service to get aggregated entitlements
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	aggregatedEntitlements, err := subscriptionService.GetAggregatedSubscriptionEntitlements(ctx, sub.ID, nil)
	if err != nil {
		return nil, decimal.Zero, err
	}

	// Map aggregated entitlements by meter ID for efficient lookup
	entitlementsByMeterID := make(map[string]*dto.AggregatedEntitlement)
	for _, feature := range aggregatedEntitlements.Features {
		if feature.Feature != nil && types.FeatureType(feature.Feature.Type) == types.FeatureTypeMetered &&
			feature.Feature.MeterID != "" && feature.Entitlement != nil {
			entitlementsByMeterID[feature.Feature.MeterID] = feature.Entitlement
		}
	}

	// Create price service once before processing charges
	priceService := NewPriceService(s.ServiceParams)

	// First collect all meter IDs from line items and charges
	meterIDs := make([]string, 0)
	for _, item := range sub.LineItems {
		if item.PriceType == types.PRICE_TYPE_USAGE && item.MeterID != "" {
			meterIDs = append(meterIDs, item.MeterID)
		}
	}
	meterIDs = lo.Uniq(meterIDs)

	// Fetch all meters at once
	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.MeterIDs = meterIDs
	meters, err := s.MeterRepo.List(ctx, meterFilter)
	if err != nil {
		return nil, decimal.Zero, err
	}

	// Create meter lookup map
	meterMap := make(map[string]*meter.Meter)
	for _, m := range meters {
		meterMap[m.ID] = m
	}

	// filter out line items that are not active
	for _, item := range sub.LineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}

		// Find matching usage charges - may have multiple if there's overage
		var matchingCharges []*dto.SubscriptionUsageByMetersResponse
		for _, charge := range usage.Charges {
			if charge.Price.ID == item.PriceID {
				matchingCharges = append(matchingCharges, charge)
			}
		}

		if len(matchingCharges) == 0 {
			s.Logger.Debugw("no matching charge found for usage line item",
				"subscription_id", sub.ID,
				"line_item_id", item.ID,
				"price_id", item.PriceID)
			continue
		}

		// Get customer for usage request
		customer, err := s.CustomerRepo.Get(ctx, sub.CustomerID)
		if err != nil {
			return nil, decimal.Zero, err
		}
		eventService := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger, s.Config)

		// Process each matching charge individually (normal and overage charges)
		for _, matchingCharge := range matchingCharges {
			quantityForCalculation := decimal.NewFromFloat(matchingCharge.Quantity)
			matchingEntitlement, ok := entitlementsByMeterID[item.MeterID]

			// Only apply entitlement adjustments if:
			// 1. This is not an overage charge
			// 2. There is a matching entitlement
			// 3. The entitlement is enabled
			if !matchingCharge.IsOverage && ok && matchingEntitlement.IsEnabled {
				if matchingEntitlement.UsageLimit != nil {

					// consider the usage reset period
					// TODO: Support other reset periods i.e. weekly, yearly
					// usage limit is set, so we decrement the usage quantity by the already entitled usage

					// case 1 : when the usage reset period is billing period
					if (matchingEntitlement.UsageResetPeriod) == types.EntitlementUsageResetPeriod(sub.BillingPeriod) {

						usageAllowed := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						adjustedQuantity := decimal.NewFromFloat(matchingCharge.Quantity).Sub(usageAllowed)
						quantityForCalculation = decimal.Max(adjustedQuantity, decimal.Zero)

					} else if matchingEntitlement.UsageResetPeriod == types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY {

						// case 2 : when the usage reset period is daily
						// For daily reset periods, we need to fetch usage with daily window size
						// and calculate overage per day, then sum the total overage

						// Create usage request with daily window size
						usageRequest := &dto.GetUsageByMeterRequest{
							MeterID:            item.MeterID,
							PriceID:            item.PriceID,
							ExternalCustomerID: customer.ExternalID,
							StartTime:          item.GetPeriodStart(periodStart),
							EndTime:            item.GetPeriodEnd(periodEnd),
							WindowSize:         types.WindowSizeDay, // Use daily window size
						}

						// Get usage data with daily windows
						usageResult, err := eventService.GetUsageByMeter(ctx, usageRequest)
						if err != nil {
							return nil, decimal.Zero, err
						}

						// Calculate daily limit
						dailyLimit := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						totalBillableQuantity := decimal.Zero

						s.Logger.Debugw("calculating daily usage charges",
							"subscription_id", sub.ID,
							"line_item_id", item.ID,
							"meter_id", item.MeterID,
							"daily_limit", dailyLimit,
							"num_daily_windows", len(usageResult.Results))

						// Process each daily window
						for _, dailyResult := range usageResult.Results {
							dailyUsage := dailyResult.Value

							// Calculate overage for this day: max(0, daily_usage - daily_limit)
							dailyOverage := decimal.Max(decimal.Zero, dailyUsage.Sub(dailyLimit))

							if dailyOverage.GreaterThan(decimal.Zero) {
								// Add to total billable quantity
								totalBillableQuantity = totalBillableQuantity.Add(dailyOverage)

								s.Logger.Debugw("daily overage calculated",
									"subscription_id", sub.ID,
									"line_item_id", item.ID,
									"date", dailyResult.WindowSize,
									"daily_usage", dailyUsage,
									"daily_limit", dailyLimit,
									"daily_overage", dailyOverage)
							}
						}

						// Use the total billable quantity for calculation
						quantityForCalculation = totalBillableQuantity
					} else if matchingEntitlement.UsageResetPeriod == types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY {

						// case 3 : when the usage reset period is monthly
						// For monthly reset periods, we need to fetch usage with monthly window size
						// and calculate overage per month, then sum the total overage

						// Create usage request with monthly window size
						usageRequest := &dto.GetUsageByMeterRequest{
							MeterID:            item.MeterID,
							PriceID:            item.PriceID,
							ExternalCustomerID: customer.ExternalID,
							StartTime:          item.GetPeriodStart(periodStart),
							EndTime:            item.GetPeriodEnd(periodEnd),
							BillingAnchor:      &sub.BillingAnchor,
							WindowSize:         types.WindowSizeMonth, // Use monthly window size
						}

						// Get usage data with monthly windows
						usageResult, err := eventService.GetUsageByMeter(ctx, usageRequest)
						if err != nil {
							return nil, decimal.Zero, err
						}

						// Calculate monthly limit
						monthlyLimit := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						totalBillableQuantity := decimal.Zero

						s.Logger.Debugw("calculating monthly usage charges",
							"subscription_id", sub.ID,
							"line_item_id", item.ID,
							"meter_id", item.MeterID,
							"monthly_limit", monthlyLimit,
							"num_monthly_windows", len(usageResult.Results))

						// Process each monthly window
						for _, monthlyResult := range usageResult.Results {
							monthlyUsage := monthlyResult.Value

							// Calculate overage for this month: max(0, monthly_usage - monthly_limit)
							monthlyOverage := decimal.Max(decimal.Zero, monthlyUsage.Sub(monthlyLimit))

							if monthlyOverage.GreaterThan(decimal.Zero) {
								// Add to total billable quantity
								totalBillableQuantity = totalBillableQuantity.Add(monthlyOverage)

								s.Logger.Debugw("monthly overage calculated",
									"subscription_id", sub.ID,
									"line_item_id", item.ID,
									"month", monthlyResult.WindowSize,
									"monthly_usage", monthlyUsage,
									"monthly_limit", monthlyLimit,
									"monthly_overage", monthlyOverage)
							}
						}

						// Use the total billable quantity for calculation
						quantityForCalculation = totalBillableQuantity
					} else if matchingEntitlement.UsageResetPeriod == types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER {
						// Calculate usage for never reset entitlements using helper function
						usageAllowed := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						quantityForCalculation, err = s.calculateNeverResetUsage(ctx, sub, item, customer, eventService, periodStart, periodEnd, usageAllowed)
						if err != nil {
							return nil, decimal.Zero, err
						}
					} else {
						usageAllowed := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						adjustedQuantity := decimal.NewFromFloat(matchingCharge.Quantity).Sub(usageAllowed)
						quantityForCalculation = decimal.Max(adjustedQuantity, decimal.Zero)
					}

					// Recalculate the amount based on the adjusted quantity
					if matchingCharge.Price != nil {
						// Get meter from pre-fetched map
						meter, ok := meterMap[item.MeterID]
						if !ok {
							return nil, decimal.Zero, ierr.NewError("meter not found").
								WithHint(fmt.Sprintf("Meter with ID %s not found", item.MeterID)).
								WithReportableDetails(map[string]interface{}{
									"meter_id": item.MeterID,
								}).
								Mark(ierr.ErrNotFound)
						}

						// For bucketed max, we need to process each bucket's max value
						if meter.IsBucketedMaxMeter() {
							// Get usage with bucketed values
							usageRequest := &dto.GetUsageByMeterRequest{
								MeterID:            item.MeterID,
								PriceID:            item.PriceID,
								ExternalCustomerID: customer.ExternalID,
								StartTime:          item.GetPeriodStart(periodStart),
								EndTime:            item.GetPeriodEnd(periodEnd),
								WindowSize:         types.WindowSizeMonth, // Set monthly window size for custom billing periods
								BillingAnchor:      &sub.BillingAnchor,
							}

							// Get usage data with buckets
							usageResult, err := eventService.GetUsageByMeter(ctx, usageRequest)
							if err != nil {
								return nil, decimal.Zero, err
							}

							// Extract bucket values
							bucketedValues := make([]decimal.Decimal, len(usageResult.Results))
							for i, result := range usageResult.Results {
								bucketedValues[i] = result.Value
							}

							// Calculate cost using bucketed values
							adjustedAmount := priceService.CalculateBucketedCost(ctx, matchingCharge.Price, bucketedValues)
							matchingCharge.Amount = adjustedAmount.InexactFloat64()

							// Update quantity to reflect the sum of all bucket maxes
							totalBucketQuantity := decimal.Zero
							for _, bucketValue := range bucketedValues {
								totalBucketQuantity = totalBucketQuantity.Add(bucketValue)
							}
							matchingCharge.Quantity = totalBucketQuantity.InexactFloat64()
							quantityForCalculation = totalBucketQuantity
						} else if meter.IsBucketedSumMeter() {
							// For sum with bucket, we need to process each bucket's sum value
							// Get usage with bucketed values
							usageRequest := &dto.GetUsageByMeterRequest{
								MeterID:            item.MeterID,
								PriceID:            item.PriceID,
								ExternalCustomerID: customer.ExternalID,
								StartTime:          item.GetPeriodStart(periodStart),
								EndTime:            item.GetPeriodEnd(periodEnd),
								WindowSize:         meter.Aggregation.BucketSize, // Use the meter's bucket size
								BillingAnchor:      &sub.BillingAnchor,
							}

							// Get usage data with buckets
							usageResult, err := eventService.GetUsageByMeter(ctx, usageRequest)
							if err != nil {
								return nil, decimal.Zero, err
							}

							// Extract bucket values (each bucket contains the sum for that window)
							bucketedValues := make([]decimal.Decimal, len(usageResult.Results))
							for i, result := range usageResult.Results {
								bucketedValues[i] = result.Value
							}

							// Calculate cost using bucketed values (each bucket is priced independently)
							adjustedAmount := priceService.CalculateBucketedCost(ctx, matchingCharge.Price, bucketedValues)
							matchingCharge.Amount = adjustedAmount.InexactFloat64()

							// Update quantity to reflect the sum of all bucket sums
							totalBucketQuantity := decimal.Zero
							for _, bucketValue := range bucketedValues {
								totalBucketQuantity = totalBucketQuantity.Add(bucketValue)
							}
							matchingCharge.Quantity = totalBucketQuantity.InexactFloat64()
							quantityForCalculation = totalBucketQuantity
						} else {
							// For regular pricing, use standard cost calculation
							adjustedAmount := priceService.CalculateCost(ctx, matchingCharge.Price, quantityForCalculation)
							matchingCharge.Amount = adjustedAmount.InexactFloat64()
						}
					}
				} else {
					// unlimited usage allowed, so we set the usage quantity for calculation to 0
					quantityForCalculation = decimal.Zero
					matchingCharge.Amount = 0
				}
			}
			// For all other cases (no entitlement, disabled entitlement, or overage),
			// use the full quantity and calculate the amount normally

			// Add the amount to total usage cost
			lineItemAmount := decimal.NewFromFloat(matchingCharge.Amount)

			// Store commitment info separately
			var commitmentInfo *types.CommitmentInfo

			// Apply line-item commitment if configured
			// Line item commitment takes precedence over subscription-level commitment
			if item.HasCommitment() {
				// Defensive check: skip commitment application if Price is nil
				if matchingCharge.Price == nil {
					s.Logger.Debugw("skipping commitment application due to missing price",
						"subscription_id", sub.ID,
						"line_item_id", item.ID,
						"price_id", item.PriceID)
				} else {
					commitmentCalc := newCommitmentCalculator(s.Logger, priceService)

					// Check if this is window-based commitment
					if item.CommitmentWindowed {
						// For window commitment, we need bucketed values
						// Get meter to access bucket configuration
						meter, ok := meterMap[item.MeterID]
						if !ok {
							return nil, decimal.Zero, ierr.NewError("meter not found for window commitment").
								WithHint(fmt.Sprintf("Meter with ID %s not found", item.MeterID)).
								WithReportableDetails(map[string]interface{}{
									"meter_id":     item.MeterID,
									"line_item_id": item.ID,
								}).
								Mark(ierr.ErrNotFound)
						}

						// Fetch bucketed usage values
						usageRequest := &dto.GetUsageByMeterRequest{
							MeterID:            item.MeterID,
							PriceID:            item.PriceID,
							ExternalCustomerID: customer.ExternalID,
							StartTime:          item.GetPeriodStart(periodStart),
							EndTime:            item.GetPeriodEnd(periodEnd),
							WindowSize:         meter.Aggregation.BucketSize,
							BillingAnchor:      &sub.BillingAnchor,
						}

						usageResult, err := eventService.GetUsageByMeter(ctx, usageRequest)
						if err != nil {
							return nil, decimal.Zero, err
						}

						// Extract bucket values
						bucketedValues := make([]decimal.Decimal, len(usageResult.Results))
						for i, result := range usageResult.Results {
							bucketedValues[i] = result.Value
						}

						// Apply window-based commitment
						adjustedAmount, info, err := commitmentCalc.applyWindowCommitmentToLineItem(
							ctx, item, bucketedValues, matchingCharge.Price)
						if err != nil {
							return nil, decimal.Zero, err
						}

						lineItemAmount = adjustedAmount
						matchingCharge.Amount = adjustedAmount.InexactFloat64()
						commitmentInfo = info
					} else {
						// Non-window commitment: apply to aggregated usage cost
						adjustedAmount, info, err := commitmentCalc.applyCommitmentToLineItem(
							ctx, item, lineItemAmount, matchingCharge.Price)
						if err != nil {
							return nil, decimal.Zero, err
						}

						lineItemAmount = adjustedAmount
						matchingCharge.Amount = adjustedAmount.InexactFloat64()
						commitmentInfo = info
					}
				}
			}

			totalUsageCost = totalUsageCost.Add(lineItemAmount)

			// Create metadata for the line item, including overage information if applicable
			metadata := types.Metadata{
				"description": fmt.Sprintf("%s (Usage Charge)", item.DisplayName),
			}

			displayName := lo.ToPtr(item.DisplayName)

			// Add overage specific information
			if matchingCharge.IsOverage {
				metadata["is_overage"] = "true"
				metadata["overage_factor"] = fmt.Sprintf("%v", matchingCharge.OverageFactor)
				metadata["description"] = fmt.Sprintf("%s (Overage Charge)", item.DisplayName)
				displayName = lo.ToPtr(fmt.Sprintf("%s (Overage)", item.DisplayName))
			}

			// Add usage reset period metadata if entitlement has daily, monthly, or never reset
			if !matchingCharge.IsOverage && ok && matchingEntitlement.IsEnabled {
				switch matchingEntitlement.UsageResetPeriod {
				case types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY:
					metadata["usage_reset_period"] = "daily"
				case types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY:
					metadata["usage_reset_period"] = "monthly"
				case types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER:
					metadata["usage_reset_period"] = "never"
				}
			}

			s.Logger.Debugw("usage charges for line item",
				"amount", matchingCharge.Amount,
				"quantity", matchingCharge.Quantity,
				"is_overage", matchingCharge.IsOverage,
				"subscription_id", sub.ID,
				"line_item_id", item.ID,
				"price_id", item.PriceID)

			// Calculate price unit amount if price unit is available
			var priceUnitAmount *decimal.Decimal
			if item.PriceUnit != "" {
				convertedAmount, err := s.PriceUnitRepo.ConvertToPriceUnit(ctx, item.PriceUnit, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), lineItemAmount)
				if err != nil {
					s.Logger.Warnw("failed to convert amount to price unit",
						"error", err,
						"price_unit", item.PriceUnit,
						"amount", lineItemAmount)
				} else {
					priceUnitAmount = &convertedAmount
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
				PriceUnit:        lo.ToPtr(item.PriceUnit),
				PriceUnitAmount:  priceUnitAmount,
				DisplayName:      displayName,
				Amount:           lineItemAmount,
				Quantity:         quantityForCalculation,
				PeriodStart:      lo.ToPtr(item.GetPeriodStart(periodStart)),
				PeriodEnd:        lo.ToPtr(item.GetPeriodEnd(periodEnd)),
				Metadata:         metadata,
				CommitmentInfo:   commitmentInfo,
			})
		}
	}

	// Add commitment true-up line item if there's remaining commitment
	commitmentAmount := lo.FromPtr(sub.CommitmentAmount)
	overageFactor := lo.FromPtr(sub.OverageFactor)
	hasCommitment := commitmentAmount.GreaterThan(decimal.Zero) && overageFactor.GreaterThan(decimal.NewFromInt(1))

	if hasCommitment {
		// If there's overage, commitment is fully utilized, so no true-up needed
		if !usage.HasOverage && sub.EnableTrueUp {
			remainingCommitment := s.calculateRemainingCommitment(usage, commitmentAmount)

			if remainingCommitment.GreaterThan(decimal.Zero) {
				// Get plan display name from line items
				planDisplayName := ""
				for _, item := range sub.LineItems {
					if item.PlanDisplayName != "" {
						planDisplayName = item.PlanDisplayName
						break
					}
				}
				// Round remaining commitment to currency precision (2 decimal places for most currencies)
				precision := types.GetCurrencyPrecision(sub.Currency)
				roundedRemainingCommitment := remainingCommitment.Round(precision)
				commitmentUtilized := commitmentAmount.Sub(roundedRemainingCommitment)
				trueUpLineItem := dto.CreateInvoiceLineItemRequest{
					EntityID:        lo.ToPtr(sub.PlanID),
					EntityType:      lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
					PriceType:       lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
					PlanDisplayName: lo.ToPtr(planDisplayName),
					DisplayName:     lo.ToPtr(fmt.Sprintf("%s True Up", planDisplayName)), // Plan display name with true up suffix
					Amount:          roundedRemainingCommitment,
					Quantity:        decimal.NewFromInt(1),
					PeriodStart:     &periodStart,
					PeriodEnd:       &periodEnd,
					PriceID:         lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)),
					Metadata: types.Metadata{
						"is_commitment_trueup": "true",
						"description":          "Remaining commitment amount for billing period",
						"commitment_amount":    commitmentAmount.String(),
						"commitment_utilized":  commitmentUtilized.String(),
					},
				}

				usageCharges = append(usageCharges, trueUpLineItem)
				totalUsageCost = totalUsageCost.Add(roundedRemainingCommitment)
			}
		}
	}

	return usageCharges, totalUsageCost, nil
}

// calculateRemainingCommitment calculates the remaining commitment amount
// that needs to be charged as a true-up
func (s *billingService) calculateRemainingCommitment(
	usage *dto.GetUsageBySubscriptionResponse,
	commitmentAmount decimal.Decimal,
) decimal.Decimal {
	if usage == nil {
		return decimal.Zero
	}

	commitmentUtilized := decimal.NewFromFloat(usage.CommitmentUtilized)
	remainingCommitment := commitmentAmount.Sub(commitmentUtilized)
	return decimal.Max(remainingCommitment, decimal.Zero)
}

func (s *billingService) CalculateUsageChargesForPreview(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	periodStart,
	periodEnd time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {

	if usage == nil {
		return nil, decimal.Zero, nil
	}

	usageCharges := make([]dto.CreateInvoiceLineItemRequest, 0)
	totalUsageCost := decimal.Zero

	// Use subscription service to get aggregated entitlements
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	aggregatedEntitlements, err := subscriptionService.GetAggregatedSubscriptionEntitlements(ctx, sub.ID, nil)
	if err != nil {
		return nil, decimal.Zero, err
	}

	// Map aggregated entitlements by meter ID for efficient lookup
	entitlementsByMeterID := make(map[string]*dto.AggregatedEntitlement)
	for _, feature := range aggregatedEntitlements.Features {
		if feature.Feature != nil && types.FeatureType(feature.Feature.Type) == types.FeatureTypeMetered &&
			feature.Feature.MeterID != "" && feature.Entitlement != nil {
			entitlementsByMeterID[feature.Feature.MeterID] = feature.Entitlement
		}
	}

	// Create price service once before processing charges
	priceService := NewPriceService(s.ServiceParams)

	// First collect all meter IDs from line items and charges
	meterIDs := make([]string, 0)
	for _, item := range sub.LineItems {
		if item.PriceType == types.PRICE_TYPE_USAGE && item.MeterID != "" {
			meterIDs = append(meterIDs, item.MeterID)
		}
	}
	meterIDs = lo.Uniq(meterIDs)

	// Fetch all meters at once
	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.MeterIDs = meterIDs
	meters, err := s.MeterRepo.List(ctx, meterFilter)
	if err != nil {
		return nil, decimal.Zero, err
	}

	// Create meter lookup map
	meterMap := make(map[string]*meter.Meter)
	for _, m := range meters {
		meterMap[m.ID] = m
	}

	// filter out line items that are not active
	for _, item := range sub.LineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}

		// Find matching usage charges - may have multiple if there's overage
		var matchingCharges []*dto.SubscriptionUsageByMetersResponse
		for _, charge := range usage.Charges {
			if charge.Price.ID == item.PriceID {
				matchingCharges = append(matchingCharges, charge)
			}
		}

		if len(matchingCharges) == 0 {
			s.Logger.Debugw("no matching charge found for usage line item",
				"subscription_id", sub.ID,
				"line_item_id", item.ID,
				"price_id", item.PriceID)
			continue
		}

		// Get customer for usage request
		customer, err := s.CustomerRepo.Get(ctx, sub.CustomerID)
		if err != nil {
			return nil, decimal.Zero, err
		}
		eventService := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger, s.Config)

		// Get meter from pre-fetched map (needed for bucketed meter check)
		meter, meterOk := meterMap[item.MeterID]
		if !meterOk {
			return nil, decimal.Zero, ierr.NewError("meter not found").
				WithHint(fmt.Sprintf("Meter with ID %s not found", item.MeterID)).
				WithReportableDetails(map[string]interface{}{
					"meter_id": item.MeterID,
				}).
				Mark(ierr.ErrNotFound)
		}

		// Process each matching charge individually (normal and overage charges)
		for _, matchingCharge := range matchingCharges {
			quantityForCalculation := decimal.NewFromFloat(matchingCharge.Quantity)
			matchingEntitlement, entitlementOk := entitlementsByMeterID[item.MeterID]

			// Handle bucketed max meters first - this should always be checked regardless of entitlements
			// But skip overage charges as they already have the correct amount with overage factor applied
			if meter.IsBucketedMaxMeter() && matchingCharge.Price != nil {
				// Get usage with bucketed values
				usageRequest := &events.FeatureUsageParams{
					PriceID: item.PriceID,
					MeterID: item.MeterID,
					UsageParams: &events.UsageParams{
						ExternalCustomerID: customer.ExternalID,
						AggregationType:    types.AggregationMax,
						StartTime:          item.GetPeriodStart(periodStart),
						EndTime:            item.GetPeriodEnd(periodEnd),
						WindowSize:         meter.Aggregation.BucketSize, // Set monthly window size for custom billing periods
						BillingAnchor:      &sub.BillingAnchor,
					},
				}

				// Get usage data with buckets
				usageResult, err := s.FeatureUsageRepo.GetUsageForMaxMetersWithBuckets(ctx, usageRequest)
				if err != nil {
					return nil, decimal.Zero, err
				}

				// Extract bucket values
				bucketedValues := make([]decimal.Decimal, len(usageResult.Results))
				for i, result := range usageResult.Results {
					bucketedValues[i] = result.Value
				}

				// Calculate cost using bucketed values
				adjustedAmount := priceService.CalculateBucketedCost(ctx, matchingCharge.Price, bucketedValues)
				matchingCharge.Amount = price.FormatAmountToFloat64WithPrecision(adjustedAmount, matchingCharge.Price.Currency)

				// Update quantity to reflect the sum of all bucket maxes
				totalBucketQuantity := decimal.Zero
				for _, bucketValue := range bucketedValues {
					totalBucketQuantity = totalBucketQuantity.Add(bucketValue)
				}
				matchingCharge.Quantity = totalBucketQuantity.InexactFloat64()
				quantityForCalculation = totalBucketQuantity
			} else if meter.IsBucketedSumMeter() && matchingCharge.Price != nil {
				// Handle sum with bucket meters - uses optimized feature_usage table
				// Get usage with bucketed values
				usageRequest := &events.FeatureUsageParams{
					PriceID: item.PriceID,
					MeterID: item.MeterID,
					UsageParams: &events.UsageParams{
						ExternalCustomerID: customer.ExternalID,
						AggregationType:    types.AggregationSum,
						StartTime:          item.GetPeriodStart(periodStart),
						EndTime:            item.GetPeriodEnd(periodEnd),
						WindowSize:         meter.Aggregation.BucketSize, // Use the meter's bucket size
						BillingAnchor:      &sub.BillingAnchor,
					},
				}

				// Get usage data with buckets from feature_usage table (optimized, pre-aggregated)
				usageResult, err := s.FeatureUsageRepo.GetUsageForMaxMetersWithBuckets(ctx, usageRequest)
				if err != nil {
					return nil, decimal.Zero, err
				}

				// Extract bucket values (each bucket contains the sum for that window)
				bucketedValues := make([]decimal.Decimal, len(usageResult.Results))
				for i, result := range usageResult.Results {
					bucketedValues[i] = result.Value
				}

				// Calculate cost using bucketed values (each bucket is priced independently)
				adjustedAmount := priceService.CalculateBucketedCost(ctx, matchingCharge.Price, bucketedValues)
				matchingCharge.Amount = price.FormatAmountToFloat64WithPrecision(adjustedAmount, matchingCharge.Price.Currency)

				// Update quantity to reflect the sum of all bucket sums
				totalBucketQuantity := decimal.Zero
				for _, bucketValue := range bucketedValues {
					totalBucketQuantity = totalBucketQuantity.Add(bucketValue)
				}
				matchingCharge.Quantity = totalBucketQuantity.InexactFloat64()
				quantityForCalculation = totalBucketQuantity
			}

			// Only apply entitlement adjustments if:
			// 1. This is not an overage charge
			// 2. There is a matching entitlement
			// 3. The entitlement is enabled
			// 4. This is not a bucketed max meter (already handled above)
			// 5. This is not a sum with bucket meter (already handled above)
			if !matchingCharge.IsOverage && entitlementOk && matchingEntitlement.IsEnabled {
				if matchingEntitlement.UsageLimit != nil {

					// consider the usage reset period
					// TODO: Support other reset periods i.e. weekly, yearly
					// usage limit is set, so we decrement the usage quantity by the already entitled usage

					// case 1 : when the usage reset period is billing period
					if (matchingEntitlement.UsageResetPeriod) == types.EntitlementUsageResetPeriod(sub.BillingPeriod) {

						usageAllowed := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						adjustedQuantity := decimal.NewFromFloat(matchingCharge.Quantity).Sub(usageAllowed)
						quantityForCalculation = decimal.Max(adjustedQuantity, decimal.Zero)

					} else if matchingEntitlement.UsageResetPeriod == types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY {

						// case 2 : when the usage reset period is daily
						// For daily reset periods, we need to fetch usage with daily window size
						// and calculate overage per day, then sum the total overage

						// Create usage request with daily window size
						usageRequest := &dto.GetUsageByMeterRequest{
							MeterID:            item.MeterID,
							PriceID:            item.PriceID,
							ExternalCustomerID: customer.ExternalID,
							StartTime:          item.GetPeriodStart(periodStart),
							EndTime:            item.GetPeriodEnd(periodEnd),
							WindowSize:         types.WindowSizeDay, // Use daily window size
						}

						// Get usage data with daily windows
						usageResult, err := eventService.GetUsageByMeter(ctx, usageRequest)
						if err != nil {
							return nil, decimal.Zero, err
						}

						// Calculate daily limit
						dailyLimit := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						totalBillableQuantity := decimal.Zero

						s.Logger.Debugw("calculating daily usage charges",
							"subscription_id", sub.ID,
							"line_item_id", item.ID,
							"meter_id", item.MeterID,
							"daily_limit", dailyLimit,
							"num_daily_windows", len(usageResult.Results))

						// Process each daily window
						for _, dailyResult := range usageResult.Results {
							dailyUsage := dailyResult.Value

							// Calculate overage for this day: max(0, daily_usage - daily_limit)
							dailyOverage := decimal.Max(decimal.Zero, dailyUsage.Sub(dailyLimit))

							if dailyOverage.GreaterThan(decimal.Zero) {
								// Add to total billable quantity
								totalBillableQuantity = totalBillableQuantity.Add(dailyOverage)

								s.Logger.Debugw("daily overage calculated",
									"subscription_id", sub.ID,
									"line_item_id", item.ID,
									"date", dailyResult.WindowSize,
									"daily_usage", dailyUsage,
									"daily_limit", dailyLimit,
									"daily_overage", dailyOverage)
							}
						}

						// Use the total billable quantity for calculation
						quantityForCalculation = totalBillableQuantity
					} else if matchingEntitlement.UsageResetPeriod == types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY {

						// case 3 : when the usage reset period is monthly
						// For monthly reset periods, we need to fetch usage with monthly window size
						// and calculate overage per month, then sum the total overage

						// Create usage request with monthly window size
						usageRequest := &dto.GetUsageByMeterRequest{
							MeterID:            item.MeterID,
							PriceID:            item.PriceID,
							ExternalCustomerID: customer.ExternalID,
							StartTime:          item.GetPeriodStart(periodStart),
							EndTime:            item.GetPeriodEnd(periodEnd),
							BillingAnchor:      &sub.BillingAnchor,
							WindowSize:         types.WindowSizeMonth, // Use monthly window size
						}

						// Get usage data with monthly windows
						usageResult, err := eventService.GetUsageByMeter(ctx, usageRequest)
						if err != nil {
							return nil, decimal.Zero, err
						}

						// Calculate monthly limit
						monthlyLimit := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						totalBillableQuantity := decimal.Zero

						s.Logger.Debugw("calculating monthly usage charges",
							"subscription_id", sub.ID,
							"line_item_id", item.ID,
							"meter_id", item.MeterID,
							"monthly_limit", monthlyLimit,
							"num_monthly_windows", len(usageResult.Results))

						// Process each monthly window
						for _, monthlyResult := range usageResult.Results {
							monthlyUsage := monthlyResult.Value

							// Calculate overage for this month: max(0, monthly_usage - monthly_limit)
							monthlyOverage := decimal.Max(decimal.Zero, monthlyUsage.Sub(monthlyLimit))

							if monthlyOverage.GreaterThan(decimal.Zero) {
								// Add to total billable quantity
								totalBillableQuantity = totalBillableQuantity.Add(monthlyOverage)

								s.Logger.Debugw("monthly overage calculated",
									"subscription_id", sub.ID,
									"line_item_id", item.ID,
									"month", monthlyResult.WindowSize,
									"monthly_usage", monthlyUsage,
									"monthly_limit", monthlyLimit,
									"monthly_overage", monthlyOverage)
							}
						}

						// Use the total billable quantity for calculation
						quantityForCalculation = totalBillableQuantity
					} else if matchingEntitlement.UsageResetPeriod == types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER {
						// Calculate usage for never reset entitlements using helper function
						usageAllowed := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						quantityForCalculation, err = s.calculateNeverResetUsage(ctx, sub, item, customer, eventService, periodStart, periodEnd, usageAllowed)
						if err != nil {
							return nil, decimal.Zero, err
						}
					} else {
						usageAllowed := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						adjustedQuantity := decimal.NewFromFloat(matchingCharge.Quantity).Sub(usageAllowed)
						quantityForCalculation = decimal.Max(adjustedQuantity, decimal.Zero)
					}

					// Recalculate the amount based on the adjusted quantity (only for non-bucketed meters and non-sum-with-bucket meters)
					if matchingCharge.Price != nil {
						// For regular pricing, use standard cost calculation
						adjustedAmount := priceService.CalculateCost(ctx, matchingCharge.Price, quantityForCalculation)
						matchingCharge.Amount = price.FormatAmountToFloat64WithPrecision(adjustedAmount, matchingCharge.Price.Currency)
					}
				} else {
					// unlimited usage allowed, so we set the usage quantity for calculation to 0
					quantityForCalculation = decimal.Zero
					matchingCharge.Amount = 0
				}
			} else if !matchingCharge.IsOverage && !meter.IsBucketedMaxMeter() && !meter.IsBucketedSumMeter() && matchingCharge.Price != nil {
				// For non-bucketed meters without entitlements (but not overage charges),
				// calculate cost normally. Overage charges already have the correct amount
				// calculated by GetFeatureUsageBySubscription with the overage factor applied.
				adjustedAmount := priceService.CalculateCost(ctx, matchingCharge.Price, quantityForCalculation)
				matchingCharge.Amount = price.FormatAmountToFloat64WithPrecision(adjustedAmount, matchingCharge.Price.Currency)
			}

			// Add the amount to total usage cost
			lineItemAmount := decimal.NewFromFloat(matchingCharge.Amount)

			// Store commitment info separately
			var commitmentInfo *types.CommitmentInfo

			// Apply line-item commitment if configured
			// Line item commitment takes precedence over subscription-level commitment
			if item.HasCommitment() {
				// Defensive check: skip commitment application if Price is nil
				if matchingCharge.Price == nil {
					s.Logger.Debugw("skipping commitment application due to missing price",
						"subscription_id", sub.ID,
						"line_item_id", item.ID,
						"price_id", item.PriceID)
				} else {
					commitmentCalc := newCommitmentCalculator(s.Logger, priceService)

					// Check if this is window-based commitment
					if item.CommitmentWindowed {
						// For window commitment, we need bucketed values
						// Get meter to access bucket configuration
						meter, ok := meterMap[item.MeterID]
						if !ok {
							return nil, decimal.Zero, ierr.NewError("meter not found for window commitment").
								WithHint(fmt.Sprintf("Meter with ID %s not found", item.MeterID)).
								WithReportableDetails(map[string]interface{}{
									"meter_id":     item.MeterID,
									"line_item_id": item.ID,
								}).
								Mark(ierr.ErrNotFound)
						}

						// Fetch bucketed usage values
						usageRequest := &dto.GetUsageByMeterRequest{
							MeterID:            item.MeterID,
							PriceID:            item.PriceID,
							ExternalCustomerID: customer.ExternalID,
							StartTime:          item.GetPeriodStart(periodStart),
							EndTime:            item.GetPeriodEnd(periodEnd),
							WindowSize:         meter.Aggregation.BucketSize,
							BillingAnchor:      &sub.BillingAnchor,
						}

						usageResult, err := eventService.GetUsageByMeter(ctx, usageRequest)
						if err != nil {
							return nil, decimal.Zero, err
						}

						// Extract bucket values
						bucketedValues := make([]decimal.Decimal, len(usageResult.Results))
						for i, result := range usageResult.Results {
							bucketedValues[i] = result.Value
						}

						// Apply window-based commitment
						adjustedAmount, info, err := commitmentCalc.applyWindowCommitmentToLineItem(
							ctx, item, bucketedValues, matchingCharge.Price)
						if err != nil {
							return nil, decimal.Zero, err
						}

						lineItemAmount = adjustedAmount
						matchingCharge.Amount = adjustedAmount.InexactFloat64()
						commitmentInfo = info
					} else {
						// Non-window commitment: apply to aggregated usage cost
						adjustedAmount, info, err := commitmentCalc.applyCommitmentToLineItem(
							ctx, item, lineItemAmount, matchingCharge.Price)
						if err != nil {
							return nil, decimal.Zero, err
						}

						lineItemAmount = adjustedAmount
						matchingCharge.Amount = adjustedAmount.InexactFloat64()
						commitmentInfo = info
					}
				}
			}

			totalUsageCost = totalUsageCost.Add(lineItemAmount)

			// Create metadata for the line item, including overage information if applicable
			metadata := types.Metadata{
				"description": fmt.Sprintf("%s (Usage Charge)", item.DisplayName),
			}

			displayName := lo.ToPtr(item.DisplayName)

			// Add overage specific information
			if matchingCharge.IsOverage {
				metadata["is_overage"] = "true"
				metadata["overage_factor"] = fmt.Sprintf("%v", matchingCharge.OverageFactor)
				metadata["description"] = fmt.Sprintf("%s (Overage Charge)", item.DisplayName)
				displayName = lo.ToPtr(fmt.Sprintf("%s (Overage)", item.DisplayName))
			}

			// Add usage reset period metadata if entitlement has daily, monthly, or never reset
			if !matchingCharge.IsOverage && entitlementOk && matchingEntitlement != nil && matchingEntitlement.IsEnabled {
				switch matchingEntitlement.UsageResetPeriod {
				case types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY:
					metadata["usage_reset_period"] = "daily"
				case types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY:
					metadata["usage_reset_period"] = "monthly"
				case types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER:
					metadata["usage_reset_period"] = "never"
				}
			}

			s.Logger.Debugw("usage charges for line item",
				"amount", matchingCharge.Amount,
				"quantity", matchingCharge.Quantity,
				"is_overage", matchingCharge.IsOverage,
				"subscription_id", sub.ID,
				"line_item_id", item.ID,
				"price_id", item.PriceID)

			// Calculate price unit amount if price unit is available
			var priceUnitAmount *decimal.Decimal
			if item.PriceUnit != "" {
				convertedAmount, err := s.PriceUnitRepo.ConvertToPriceUnit(ctx, item.PriceUnit, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), lineItemAmount)
				if err != nil {
					s.Logger.Warnw("failed to convert amount to price unit",
						"error", err,
						"price_unit", item.PriceUnit,
						"amount", lineItemAmount)
				} else {
					priceUnitAmount = &convertedAmount
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
				PriceUnit:        lo.ToPtr(item.PriceUnit),
				PriceUnitAmount:  priceUnitAmount,
				DisplayName:      displayName,
				Amount:           lineItemAmount,
				Quantity:         quantityForCalculation,
				PeriodStart:      lo.ToPtr(item.GetPeriodStart(periodStart)),
				PeriodEnd:        lo.ToPtr(item.GetPeriodEnd(periodEnd)),
				Metadata:         metadata,
				CommitmentInfo:   commitmentInfo,
			})
		}
	}

	// Add commitment true-up line item if there's remaining commitment
	commitmentAmount := lo.FromPtr(sub.CommitmentAmount)
	overageFactor := lo.FromPtr(sub.OverageFactor)
	hasCommitment := commitmentAmount.GreaterThan(decimal.Zero) && overageFactor.GreaterThan(decimal.NewFromInt(1))

	if hasCommitment {
		// If there's overage, commitment is fully utilized, so no true-up needed
		if !usage.HasOverage && sub.EnableTrueUp {
			remainingCommitment := s.calculateRemainingCommitment(usage, commitmentAmount)

			if remainingCommitment.GreaterThan(decimal.Zero) {
				planDisplayName := ""
				for _, item := range sub.LineItems {
					if item.PlanDisplayName != "" {
						planDisplayName = item.PlanDisplayName
						break
					}
				}
				// Round remaining commitment to currency precision (2 decimal places for most currencies)
				precision := types.GetCurrencyPrecision(sub.Currency)
				roundedRemainingCommitment := remainingCommitment.Round(precision)
				commitmentUtilized := commitmentAmount.Sub(roundedRemainingCommitment)
				trueUpLineItem := dto.CreateInvoiceLineItemRequest{
					EntityID:        lo.ToPtr(sub.PlanID),
					EntityType:      lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
					PriceType:       lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
					PlanDisplayName: lo.ToPtr(planDisplayName),
					DisplayName:     lo.ToPtr(fmt.Sprintf("%s True Up", planDisplayName)), // Plan display name with true up suffix
					Amount:          roundedRemainingCommitment,
					Quantity:        decimal.NewFromInt(1),
					PeriodStart:     &periodStart,
					PeriodEnd:       &periodEnd,
					PriceID:         lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)),
					Metadata: types.Metadata{
						"is_commitment_trueup": "true",
						"description":          "Remaining commitment amount for billing period",
						"commitment_amount":    commitmentAmount.String(),
						"commitment_utilized":  commitmentUtilized.String(),
					},
				}

				usageCharges = append(usageCharges, trueUpLineItem)
				totalUsageCost = totalUsageCost.Add(roundedRemainingCommitment)
			}
		}
	}

	return usageCharges, totalUsageCost, nil
}

func (s *billingService) CalculateAllCharges(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	periodStart,
	periodEnd time.Time,
) (*BillingCalculationResult, error) {
	// Calculate fixed charges
	fixedCharges, fixedTotal, err := s.CalculateFixedCharges(ctx, sub, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}

	// Calculate usage charges
	usageCharges, usageTotal, err := s.CalculateUsageCharges(ctx, sub, usage, periodStart, periodEnd)
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

func (s *billingService) calculateAllChargesForPreview(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	periodStart,
	periodEnd time.Time,
) (*BillingCalculationResult, error) {
	// Calculate fixed charges
	fixedCharges, fixedTotal, err := s.CalculateFixedCharges(ctx, sub, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}

	// Calculate usage charges
	usageCharges, usageTotal, err := s.CalculateUsageChargesForPreview(ctx, sub, usage, periodStart, periodEnd)
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

func (s *billingService) PrepareSubscriptionInvoiceRequest(
	ctx context.Context,
	sub *subscription.Subscription,
	periodStart,
	periodEnd time.Time,
	referencePoint types.InvoiceReferencePoint,
) (*dto.CreateInvoiceRequest, error) {
	s.Logger.Infow("preparing subscription invoice request",
		"subscription_id", sub.ID,
		"period_start", periodStart,
		"period_end", periodEnd,
		"reference_point", referencePoint)

	// Validate that the billing period respects subscription end date
	if err := s.validatePeriodAgainstSubscriptionEndDate(sub, periodStart, periodEnd); err != nil {
		return nil, err
	}

	// nothing to invoice default response 0$ invoice
	zeroAmountInvoice, err := s.CreateInvoiceRequestForCharges(ctx,
		sub, nil, periodStart, periodEnd, "", types.Metadata{})
	if err != nil {
		return nil, err
	}

	// Calculate next period for advance charges
	nextPeriodStart := periodEnd
	nextPeriodEnd, err := types.NextBillingDate(
		nextPeriodStart,
		sub.BillingAnchor,
		sub.BillingPeriodCount,
		sub.BillingPeriod,
		sub.EndDate,
	)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("failed to calculate next billing date").
			Mark(ierr.ErrSystem)
	}

	// Classify line items
	classification := s.ClassifyLineItems(sub, periodStart, periodEnd, nextPeriodStart, nextPeriodEnd)

	var calculationResult *BillingCalculationResult
	var metadata types.Metadata = make(types.Metadata)
	var description string

	switch referencePoint {
	case types.ReferencePointPeriodStart:
		// Only include advance charges for current period
		advanceLineItems, err := s.FilterLineItemsToBeInvoiced(ctx, sub, periodStart, periodEnd, classification.CurrentPeriodAdvance)
		if err != nil {
			return nil, err
		}

		if len(advanceLineItems) == 0 {
			return zeroAmountInvoice, nil
		}

		calculationResult, err = s.CalculateCharges(
			ctx,
			sub,
			advanceLineItems,
			periodStart,
			periodEnd,
			false, // No usage for advance
		)
		if err != nil {
			return nil, err
		}

		description = fmt.Sprintf("Invoice for advance charges - subscription %s", sub.ID)

	case types.ReferencePointPeriodEnd:
		// Include both arrear charges for current period and advance charges for next period
		// First, process arrear charges for current period
		arrearLineItems, err := s.FilterLineItemsToBeInvoiced(ctx, sub, periodStart, periodEnd, classification.CurrentPeriodArrear)
		if err != nil {
			return nil, err
		}

		// Then, process advance charges for next period
		advanceLineItems, err := s.FilterLineItemsToBeInvoiced(ctx, sub, nextPeriodStart, nextPeriodEnd, classification.NextPeriodAdvance)
		if err != nil {
			return nil, err
		}

		// Combine both sets of line items
		combinedLineItems := append(arrearLineItems, advanceLineItems...)
		if len(combinedLineItems) == 0 {
			return zeroAmountInvoice, nil
		}

		// For current period arrear charges
		arrearResult, err := s.CalculateCharges(
			ctx,
			sub,
			arrearLineItems,
			periodStart,
			periodEnd,
			classification.HasUsageCharges, // Include usage for arrear
		)
		if err != nil {
			return nil, err
		}

		// For next period advance charges
		advanceResult, err := s.CalculateCharges(
			ctx,
			sub,
			advanceLineItems,
			nextPeriodStart,
			nextPeriodEnd,
			false, // No usage for advance
		)
		if err != nil {
			return nil, err
		}

		// Combine results
		calculationResult = &BillingCalculationResult{
			FixedCharges: append(arrearResult.FixedCharges, advanceResult.FixedCharges...),
			UsageCharges: arrearResult.UsageCharges, // Only arrear has usage
			TotalAmount:  arrearResult.TotalAmount.Add(advanceResult.TotalAmount),
			Currency:     sub.Currency,
		}

		description = fmt.Sprintf("Invoice for subscription %s", sub.ID)

	case types.ReferencePointPreview:
		// For preview, include both current period arrear and next period advance
		// but don't filter out already invoiced items

		// For current period arrear charges
		arrearResult, err := s.calculateChargesForPreview(
			ctx,
			sub,
			classification.CurrentPeriodArrear,
			periodStart,
			periodEnd,
			classification.HasUsageCharges, // Include usage for arrear
		)
		if err != nil {
			return nil, err
		}

		// For next period advance charges
		advanceResult, err := s.calculateChargesForPreview(
			ctx,
			sub,
			classification.NextPeriodAdvance,
			nextPeriodStart,
			nextPeriodEnd,
			false, // No usage for advance
		)
		if err != nil {
			return nil, err
		}

		// Combine results
		calculationResult = &BillingCalculationResult{
			FixedCharges: append(arrearResult.FixedCharges, advanceResult.FixedCharges...),
			UsageCharges: arrearResult.UsageCharges, // Only arrear has usage
			TotalAmount:  arrearResult.TotalAmount.Add(advanceResult.TotalAmount),
			Currency:     sub.Currency,
		}

		description = fmt.Sprintf("Preview invoice for subscription %s", sub.ID)
		metadata["is_preview"] = "true"
	case types.ReferencePointCancel:
		// for cancel, include arrer line items only
		arrearLineItems, err := s.FilterLineItemsToBeInvoiced(ctx, sub, periodStart, periodEnd, classification.CurrentPeriodArrear)
		if err != nil {
			return nil, err
		}

		// For current period arrear charges
		arrearResult, err := s.CalculateCharges(
			ctx,
			sub,
			arrearLineItems,
			periodStart,
			periodEnd,
			true, // Include usage for arrear
		)
		if err != nil {
			return nil, err
		}

		calculationResult = &BillingCalculationResult{
			FixedCharges: arrearResult.FixedCharges,
			UsageCharges: arrearResult.UsageCharges, // Only arrear has usage
			TotalAmount:  arrearResult.TotalAmount,
			Currency:     sub.Currency,
		}

		description = fmt.Sprintf("Invoice for subscription %s", sub.ID)

	default:
		return nil, ierr.NewError("invalid reference point").
			WithHint(fmt.Sprintf("Reference point '%s' is not supported", referencePoint)).
			Mark(ierr.ErrValidation)
	}

	// Create invoice request for the calculated charges
	return s.CreateInvoiceRequestForCharges(
		ctx,
		sub,
		calculationResult,
		periodStart,
		periodEnd,
		description,
		metadata,
	)
}

// validatePeriodAgainstSubscriptionEndDate ensures billing periods don't exceed subscription end date
func (s *billingService) validatePeriodAgainstSubscriptionEndDate(
	sub *subscription.Subscription,
	periodStart,
	periodEnd time.Time,
) error {
	// If no end date, no validation needed
	if sub.EndDate == nil {
		return nil
	}

	// Period start should not be after subscription end date
	if periodStart.After(*sub.EndDate) {
		return ierr.NewError("billing period starts after subscription end date").
			WithHint("Cannot bill for periods that start after subscription has ended").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":       sub.ID,
				"period_start":          periodStart,
				"subscription_end_date": *sub.EndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	// If period end is after subscription end date, that's acceptable for final billing
	// but we should log it for transparency
	if periodEnd.After(*sub.EndDate) {
		s.Logger.Infow("billing period extends beyond subscription end date - will be handled appropriately",
			"subscription_id", sub.ID,
			"period_start", periodStart,
			"period_end", periodEnd,
			"subscription_end_date", *sub.EndDate)
	}

	return nil
}
func (s *billingService) checkIfChargeInvoiced(
	invoice *invoice.Invoice,
	charge *subscription.SubscriptionLineItem,
	periodStart,
	periodEnd time.Time,
) bool {
	for _, item := range invoice.LineItems {
		// match the price id
		if lo.FromPtr(item.PriceID) == charge.PriceID {
			// match the period start and end
			if item.PeriodStart.Equal(periodStart) &&
				item.PeriodEnd.Equal(periodEnd) {
				return true
			}
		}
	}
	return false
}

// ClassifyLineItems classifies line items based on cadence and type
func (s *billingService) ClassifyLineItems(
	sub *subscription.Subscription,
	currentPeriodStart,
	currentPeriodEnd time.Time,
	nextPeriodStart,
	nextPeriodEnd time.Time,
) *LineItemClassification {
	result := &LineItemClassification{
		CurrentPeriodAdvance: make([]*subscription.SubscriptionLineItem, 0),
		CurrentPeriodArrear:  make([]*subscription.SubscriptionLineItem, 0),
		NextPeriodAdvance:    make([]*subscription.SubscriptionLineItem, 0),
		HasUsageCharges:      false,
	}

	for _, item := range sub.LineItems {
		// Current period advance charges (fixed only)
		// TODO: add support for usage charges with advance cadence later
		if item.InvoiceCadence == types.InvoiceCadenceAdvance &&
			item.PriceType == types.PRICE_TYPE_FIXED {
			result.CurrentPeriodAdvance = append(result.CurrentPeriodAdvance, item)

			// Also add to next period advance for preview purposes
			result.NextPeriodAdvance = append(result.NextPeriodAdvance, item)
		}

		// Current period arrear charges (fixed and usage)
		if item.InvoiceCadence == types.InvoiceCadenceArrear {
			result.CurrentPeriodArrear = append(result.CurrentPeriodArrear, item)
		}

		// Check if there are any usage charges
		if item.PriceType == types.PRICE_TYPE_USAGE {
			result.HasUsageCharges = true
		}
	}

	return result
}

// FilterLineItemsToBeInvoiced filters the line items to be invoiced for the given period
// by checking if an invoice already exists for those line items and period
func (s *billingService) FilterLineItemsToBeInvoiced(
	ctx context.Context,
	sub *subscription.Subscription,
	periodStart,
	periodEnd time.Time,
	lineItems []*subscription.SubscriptionLineItem,
) ([]*subscription.SubscriptionLineItem, error) {
	// If no line items to process, return empty slice immediately
	if len(lineItems) == 0 {
		return []*subscription.SubscriptionLineItem{}, nil
	}

	// Validate period against subscription end date
	if sub.EndDate != nil && !periodStart.Before(*sub.EndDate) {
		s.Logger.Debugw("period starts at or after subscription end date, no line items to invoice",
			"subscription_id", sub.ID,
			"period_start", periodStart,
			"subscription_end_date", *sub.EndDate)
		return []*subscription.SubscriptionLineItem{}, nil
	}

	filteredLineItems := make([]*subscription.SubscriptionLineItem, 0, len(lineItems))

	// Get existing invoices for this period
	invoiceFilter := types.NewNoLimitInvoiceFilter()
	invoiceFilter.SubscriptionID = sub.ID
	invoiceFilter.InvoiceType = types.InvoiceTypeSubscription
	invoiceFilter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusDraft, types.InvoiceStatusFinalized}
	invoiceFilter.TimeRangeFilter = &types.TimeRangeFilter{
		StartTime: lo.ToPtr(periodStart),
		EndTime:   lo.ToPtr(periodEnd),
	}

	invoices, err := s.InvoiceRepo.List(ctx, invoiceFilter)
	if err != nil {
		return nil, err
	}

	// If no invoices exist, return all line items
	if len(invoices) == 0 {
		s.Logger.Debugw("no existing invoices found for period, including all line items",
			"subscription_id", sub.ID,
			"period_start", periodStart,
			"period_end", periodEnd,
			"num_line_items", len(lineItems))
		return lineItems, nil
	}

	// Check line items against existing invoices to determine which are not yet invoiced
	for _, lineItem := range lineItems {
		lineItemInvoiced := false

		for _, invoice := range invoices {
			if s.checkIfChargeInvoiced(invoice, lineItem, periodStart, periodEnd) {
				lineItemInvoiced = true
				break
			}
		}

		// Include line item only if it has not been invoiced yet
		if !lineItemInvoiced {
			filteredLineItems = append(filteredLineItems, lineItem)
		}
	}

	s.Logger.Debugw("filtered line items to be invoiced",
		"subscription_id", sub.ID,
		"period_start", periodStart,
		"period_end", periodEnd,
		"total_line_items", len(lineItems),
		"filtered_line_items", len(filteredLineItems))

	return filteredLineItems, nil
}

func (s *billingService) calculateChargesForPreview(
	ctx context.Context,
	sub *subscription.Subscription,
	lineItems []*subscription.SubscriptionLineItem,
	periodStart,
	periodEnd time.Time,
	includeUsage bool,
) (*BillingCalculationResult, error) {
	// Create a filtered subscription with only the specified line items
	filteredSub := *sub
	filteredSub.LineItems = lineItems

	// Get usage data if needed
	var usage *dto.GetUsageBySubscriptionResponse
	var err error

	if includeUsage {
		subscriptionService := NewSubscriptionService(s.ServiceParams)
		usage, err = subscriptionService.GetFeatureUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
			SubscriptionID: sub.ID,
			StartTime:      periodStart,
			EndTime:        periodEnd,
		})
		if err != nil {
			return nil, err
		}
	}

	// Calculate charges
	return s.calculateAllChargesForPreview(ctx, &filteredSub, usage, periodStart, periodEnd)
}

// CalculateCharges calculates charges for the given line items and period
func (s *billingService) CalculateCharges(
	ctx context.Context,
	sub *subscription.Subscription,
	lineItems []*subscription.SubscriptionLineItem,
	periodStart,
	periodEnd time.Time,
	includeUsage bool,
) (*BillingCalculationResult, error) {
	// Create a filtered subscription with only the specified line items
	filteredSub := *sub
	filteredSub.LineItems = lineItems

	// Get usage data if needed
	var usage *dto.GetUsageBySubscriptionResponse
	var err error

	if includeUsage {
		subscriptionService := NewSubscriptionService(s.ServiceParams)
		usage, err = subscriptionService.GetUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
			SubscriptionID: sub.ID,
			StartTime:      periodStart,
			EndTime:        periodEnd,
		})
		if err != nil {
			return nil, err
		}
	}

	// Calculate charges
	return s.CalculateAllCharges(ctx, &filteredSub, usage, periodStart, periodEnd)
}

// CreateInvoiceRequestForCharges creates an invoice for the given charges
func (s *billingService) CreateInvoiceRequestForCharges(
	ctx context.Context,
	sub *subscription.Subscription,
	result *BillingCalculationResult,
	periodStart,
	periodEnd time.Time,
	description string, // mark optional
	metadata types.Metadata, // mark optional
) (*dto.CreateInvoiceRequest, error) {
	// Get invoice config for tenant
	settingsSvc := NewSettingsService(s.ServiceParams).(*settingsService)
	invoiceConfig, err := GetSetting[types.InvoiceConfig](
		settingsSvc,
		ctx,
		types.SettingKeyInvoiceConfig,
	)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get invoice configuration").
			Mark(ierr.ErrValidation)
	}

	// Prepare invoice due date using tenant's configuration
	invoiceDueDate := periodEnd.Add(24 * time.Hour * time.Duration(*invoiceConfig.DueDateDays))

	if result == nil {
		// prepare result for zero amount invoice
		result = &BillingCalculationResult{
			TotalAmount:  decimal.Zero,
			Currency:     sub.Currency,
			FixedCharges: make([]dto.CreateInvoiceLineItemRequest, 0),
			UsageCharges: make([]dto.CreateInvoiceLineItemRequest, 0),
		}
	}

	// Apply Coupons if any - both subscription level and line item level
	couponAssociationService := NewCouponAssociationService(s.ServiceParams)
	couponValidationService := NewCouponValidationService(s.ServiceParams)

	// Get all coupon associations (both subscription-level and line item-level) that are active during the subscription's current billing period
	// Using a single query to fetch both types
	allCouponsFilter := types.NewCouponAssociationFilter()
	allCouponsFilter.SubscriptionIDs = []string{sub.ID}
	allCouponsFilter.ActiveOnly = true
	allCouponsFilter.PeriodStart = &sub.CurrentPeriodStart
	allCouponsFilter.PeriodEnd = &sub.CurrentPeriodEnd
	allCouponAssociationsResponse, err := couponAssociationService.ListCouponAssociations(ctx, allCouponsFilter)
	if err != nil {
		return nil, err
	}
	allCouponAssociations := allCouponAssociationsResponse.Items

	// Build maps for efficient lookups
	subLineItemIDToPriceIDMap := make(map[string]string)
	for _, lineItem := range sub.LineItems {
		if lineItem.PriceID != "" {
			subLineItemIDToPriceIDMap[lineItem.ID] = lineItem.PriceID
		}
	}

	// Build set of price IDs that appear in invoice line items
	invoiceLineItemPriceIDs := make(map[string]bool)
	for _, lineItem := range append(result.FixedCharges, result.UsageCharges...) {
		if lineItem.PriceID != nil {
			invoiceLineItemPriceIDs[*lineItem.PriceID] = true
		}
	}

	// Process all coupon associations in a single loop
	validCoupons := make([]dto.InvoiceCoupon, 0)
	validLineItemCoupons := make([]dto.InvoiceLineItemCoupon, 0)

	for _, couponAssociation := range allCouponAssociations {
		// Get coupon details for validation
		coupon, err := s.CouponRepo.Get(ctx, couponAssociation.CouponID)
		if err != nil {
			s.Logger.Errorw("failed to get coupon", "error", err, "coupon_id", couponAssociation.CouponID)
			continue
		}

		// Validate coupon
		if err := couponValidationService.ValidateCoupon(ctx, *coupon, sub); err != nil {
			s.Logger.Errorw("failed to validate coupon", "error", err, "coupon_id", couponAssociation.CouponID)
			continue
		}

		if couponAssociation.SubscriptionLineItemID == nil {
			// Subscription-level coupon
			validCoupons = append(validCoupons, dto.InvoiceCoupon{
				CouponID:            couponAssociation.CouponID,
				CouponAssociationID: &couponAssociation.ID,
			})
		} else {
			// Line item-level coupon - only include if the line item is in the invoice
			priceID, ok := subLineItemIDToPriceIDMap[*couponAssociation.SubscriptionLineItemID]
			if !ok || priceID == "" {
				continue
			}
			// Only add if this price ID appears in the invoice line items
			if !invoiceLineItemPriceIDs[priceID] {
				continue
			}
			validLineItemCoupons = append(validLineItemCoupons, dto.InvoiceLineItemCoupon{
				LineItemID:          priceID,
				CouponID:            couponAssociation.CouponID,
				CouponAssociationID: &couponAssociation.ID,
			})
		}
	}
	// Resolve tax rates for invoice level (invoice-level only per scope)
	// Prepare minimal request for tax resolution using subscription context
	// Use invoicing customer ID if available, otherwise fallback to subscription customer ID
	invoicingCustomerID := sub.GetInvoicingCustomerID()
	taxService := NewTaxService(s.ServiceParams)
	taxPrepareReq := dto.CreateInvoiceRequest{
		SubscriptionID: lo.ToPtr(sub.ID),
		CustomerID:     invoicingCustomerID,
	}
	preparedTaxRates, err := taxService.PrepareTaxRatesForInvoice(ctx, taxPrepareReq)
	if err != nil {
		return nil, err
	}
	// Create invoice request
	// Use invoicing customer ID if available, otherwise fallback to subscription customer ID
	req := &dto.CreateInvoiceRequest{
		CustomerID:       invoicingCustomerID,
		SubscriptionID:   lo.ToPtr(sub.ID),
		InvoiceType:      types.InvoiceTypeSubscription,
		InvoiceStatus:    lo.ToPtr(types.InvoiceStatusDraft),
		PaymentStatus:    lo.ToPtr(types.PaymentStatusPending),
		Currency:         sub.Currency,
		AmountDue:        result.TotalAmount,
		Total:            result.TotalAmount,
		Subtotal:         result.TotalAmount,
		Description:      description,
		DueDate:          lo.ToPtr(invoiceDueDate),
		BillingPeriod:    lo.ToPtr(string(sub.BillingPeriod)),
		PeriodStart:      &periodStart,
		PeriodEnd:        &periodEnd,
		BillingReason:    types.InvoiceBillingReasonSubscriptionCycle,
		EnvironmentID:    sub.EnvironmentID,
		Metadata:         metadata,
		LineItems:        append(result.FixedCharges, result.UsageCharges...),
		InvoiceCoupons:   validCoupons,
		LineItemCoupons:  validLineItemCoupons,
		PreparedTaxRates: preparedTaxRates,
	}

	return req, nil
}

// applyProrationToLineItem applies proration calculation to a line item amount if proration is enabled
func (s *billingService) applyProrationToLineItem(
	ctx context.Context,
	sub *subscription.Subscription,
	item *subscription.SubscriptionLineItem,
	priceData *price.Price,
	originalAmount decimal.Decimal,
	periodStart *time.Time,
	periodEnd *time.Time,
) (decimal.Decimal, error) {

	prorationService := NewProrationService(s.ServiceParams)
	// Check if proration should be applied
	if sub.ProrationBehavior == types.ProrationBehaviorNone {
		// No proration needed
		return originalAmount, nil
	}

	// Check if period dates match subscription's current period
	if periodStart != nil && periodEnd != nil {
		if !periodStart.Equal(sub.CurrentPeriodStart) || !periodEnd.Equal(sub.CurrentPeriodEnd) {
			// Period doesn't match subscription's current period, don't apply proration
			return originalAmount, nil
		}
	}

	// If it's a usage charge, don't apply proration (usage is typically calculated for actual usage in the period)
	if item.PriceType == types.PRICE_TYPE_USAGE {
		return originalAmount, nil
	}

	action := types.ProrationActionAddItem
	if sub.SubscriptionStatus == types.SubscriptionStatusCancelled {
		action = types.ProrationActionCancellation
	}
	prorationParams, err := prorationService.CreateProrationParamsForLineItem(
		sub,
		item,
		priceData,
		action,
		sub.ProrationBehavior,
	)
	if err != nil {
		return originalAmount, err
	}

	prorationResult, err := prorationService.CalculateProration(ctx, prorationParams)
	if err != nil {
		return decimal.Zero, err
	}
	return prorationResult.NetAmount, nil
}

// Helper functions for aggregating entitlements
func aggregateMeteredEntitlementsForBilling(entitlements []*entitlement.Entitlement) *dto.AggregatedEntitlement {
	hasUnlimitedEntitlement := false
	isSoftLimit := false
	var totalLimit int64 = 0
	var usageResetPeriod types.EntitlementUsageResetPeriod
	resetPeriodCounts := make(map[types.EntitlementUsageResetPeriod]int)

	for _, e := range entitlements {
		if !e.IsEnabled {
			continue
		}

		if e.UsageLimit == nil {
			hasUnlimitedEntitlement = true
			break
		}

		if e.IsSoftLimit {
			isSoftLimit = true
		}

		// total limit is the sum of all limits
		totalLimit += *e.UsageLimit

		if e.UsageResetPeriod != "" {
			resetPeriodCounts[e.UsageResetPeriod]++
		}
	}

	// TODO: handle this better
	maxCount := 0
	for period, count := range resetPeriodCounts {
		if count > maxCount {
			maxCount = count
			usageResetPeriod = period
		}
	}

	var finalLimit *int64
	if !hasUnlimitedEntitlement {
		finalLimit = &totalLimit
	}

	return &dto.AggregatedEntitlement{
		IsEnabled:        len(entitlements) > 0,
		UsageLimit:       finalLimit,
		IsSoftLimit:      isSoftLimit,
		UsageResetPeriod: usageResetPeriod,
	}

}

func aggregateBooleanEntitlementsForBilling(entitlements []*entitlement.Entitlement) *dto.AggregatedEntitlement {
	isEnabled := false

	// If any subscription enables the feature, it's enabled
	for _, e := range entitlements {
		if e.IsEnabled {
			isEnabled = true
			break
		}
	}

	return &dto.AggregatedEntitlement{
		IsEnabled: isEnabled,
	}
}

func aggregateStaticEntitlementsForBilling(entitlements []*entitlement.Entitlement) *dto.AggregatedEntitlement {
	isEnabled := false
	staticValues := []string{}
	valueMap := make(map[string]bool) // To deduplicate values

	for _, e := range entitlements {
		if e.IsEnabled {
			isEnabled = true
			if e.StaticValue != "" && !valueMap[e.StaticValue] {
				staticValues = append(staticValues, e.StaticValue)
				valueMap[e.StaticValue] = true
			}
		}
	}

	return &dto.AggregatedEntitlement{
		IsEnabled:    isEnabled,
		StaticValues: staticValues,
	}
}

// AggregateEntitlements is a generic function that aggregates entitlements from multiple sources
// into a unified view. It can be used for both customer and subscription entitlements.
// If subscriptionID is provided, it will be used for sources that don't have a subscription ID set
func (s *billingService) AggregateEntitlements(entitlements []*dto.EntitlementResponse, subscriptionID string) []*dto.AggregatedFeature {
	// Map to store entitlements by feature ID
	featureIDs := make([]string, 0)
	entitlementsByFeature := make(map[string][]*dto.EntitlementResponse)
	sourcesByFeature := make(map[string][]*dto.EntitlementSource)

	// Process each entitlement
	for _, ent := range entitlements {
		// Skip disabled entitlements
		if !ent.IsEnabled || ent.Status != (types.StatusPublished) {
			continue
		}

		// Add feature ID to list
		featureIDs = append(featureIDs, ent.FeatureID)

		// Initialize collections if needed
		if _, ok := entitlementsByFeature[ent.FeatureID]; !ok {
			entitlementsByFeature[ent.FeatureID] = make([]*dto.EntitlementResponse, 0)
			sourcesByFeature[ent.FeatureID] = make([]*dto.EntitlementSource, 0)
		}

		// Add entitlement to feature entitlements
		entitlementsByFeature[ent.FeatureID] = append(entitlementsByFeature[ent.FeatureID], ent)

		// Create source for this entitlement
		entityType := dto.EntitlementSourceEntityTypePlan
		entityName := ""

		// Determine entity type and name
		switch ent.EntityType {
		case types.ENTITLEMENT_ENTITY_TYPE_PLAN:
			entityType = dto.EntitlementSourceEntityTypePlan
			if ent.Plan != nil {
				entityName = ent.Plan.Name
			}
		case types.ENTITLEMENT_ENTITY_TYPE_ADDON:
			entityType = dto.EntitlementSourceEntityTypeAddon
			if ent.Addon != nil {
				entityName = ent.Addon.Name
			}
		case types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION:
			entityType = dto.EntitlementSourceEntityTypeSubscription
			// For subscription entitlements, entity_name can be left empty or set to subscription identifier
			// The entity_id is the subscription ID itself
		}

		// For subscription ID, use the one from the source if available, otherwise use the provided one
		sourceSubscriptionID := subscriptionID

		source := &dto.EntitlementSource{
			SubscriptionID: sourceSubscriptionID,
			EntityID:       ent.EntityID,
			EntityType:     entityType,
			EntityName:     entityName,
			Quantity:       1, // Default to 1, could be refined based on addon occurrences
			EntitlementID:  ent.ID,
			IsEnabled:      ent.IsEnabled,
			UsageLimit:     ent.UsageLimit,
			StaticValue:    ent.StaticValue,
		}

		// Add source to feature sources
		sourcesByFeature[ent.FeatureID] = append(sourcesByFeature[ent.FeatureID], source)
	}

	// Deduplicate feature IDs
	featureIDs = lo.Uniq(featureIDs)

	// Aggregate entitlements by feature and build the response
	aggregatedFeatures := make([]*dto.AggregatedFeature, 0, len(featureIDs))

	for _, featureID := range featureIDs {
		entResponses := entitlementsByFeature[featureID]
		if len(entResponses) == 0 {
			continue
		}

		// Use the first entitlement to get feature details
		if entResponses[0].Feature == nil {
			continue
		}

		featureResponse := entResponses[0].Feature

		// Convert dto.EntitlementResponse to entitlement.Entitlement for aggregation
		domainEntitlements := make([]*entitlement.Entitlement, 0, len(entResponses))
		for _, entResp := range entResponses {
			domainEnt := &entitlement.Entitlement{
				ID:               entResp.ID,
				EntityType:       types.EntitlementEntityType(entResp.EntityType),
				EntityID:         entResp.EntityID,
				FeatureID:        entResp.FeatureID,
				FeatureType:      types.FeatureType(entResp.FeatureType),
				IsEnabled:        entResp.IsEnabled,
				UsageLimit:       entResp.UsageLimit,
				UsageResetPeriod: types.EntitlementUsageResetPeriod(entResp.UsageResetPeriod),
				IsSoftLimit:      entResp.IsSoftLimit,
				StaticValue:      entResp.StaticValue,
			}
			domainEntitlements = append(domainEntitlements, domainEnt)
		}

		// Aggregate entitlements based on feature type
		var aggregatedEntitlement *dto.AggregatedEntitlement
		switch types.FeatureType(featureResponse.Type) {
		case types.FeatureTypeMetered:
			aggregatedEntitlement = aggregateMeteredEntitlementsForBilling(domainEntitlements)
		case types.FeatureTypeBoolean:
			aggregatedEntitlement = aggregateBooleanEntitlementsForBilling(domainEntitlements)
		case types.FeatureTypeStatic:
			aggregatedEntitlement = aggregateStaticEntitlementsForBilling(domainEntitlements)
		default:
			// Skip unknown feature types
			continue
		}

		// Create aggregated feature with sources
		aggregatedFeature := &dto.AggregatedFeature{
			Feature:     featureResponse,
			Entitlement: aggregatedEntitlement,
			Sources:     sourcesByFeature[featureID],
		}

		aggregatedFeatures = append(aggregatedFeatures, aggregatedFeature)
	}

	return aggregatedFeatures
}

func (s *billingService) GetCustomerEntitlements(ctx context.Context, customerID string, req *dto.GetCustomerEntitlementsRequest) (*dto.CustomerEntitlementsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	resp := &dto.CustomerEntitlementsResponse{
		CustomerID: customerID,
		Features:   []*dto.AggregatedFeature{},
	}

	// 1. Get active subscriptions for the customer
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	subscriptions, err := subscriptionService.ListByCustomerID(ctx, customerID)
	if err != nil {
		return nil, err
	}

	// Filter subscriptions if IDs are specified
	if len(req.SubscriptionIDs) > 0 {
		filteredSubscriptions := make([]*subscription.Subscription, 0)
		for _, sub := range subscriptions {
			if lo.Contains(req.SubscriptionIDs, sub.ID) {
				filteredSubscriptions = append(filteredSubscriptions, sub)
			}
		}
		subscriptions = filteredSubscriptions
	}

	// Return empty response if no subscriptions found
	if len(subscriptions) == 0 {
		return resp, nil
	}

	// Collect all entitlements from all subscriptions
	allEntitlements := make([]*dto.EntitlementResponse, 0)

	// Process each subscription to get its entitlements (including both plan and addon entitlements)
	for _, sub := range subscriptions {
		// Get all entitlements for this subscription (plan + addons)
		subEntitlements, err := subscriptionService.GetSubscriptionEntitlements(ctx, sub.ID)
		if err != nil {
			s.Logger.Warnw("failed to get subscription entitlements, skipping",
				"subscription_id", sub.ID,
				"error", err)
			continue
		}

		// Filter by feature IDs if specified
		if len(req.FeatureIDs) > 0 {
			for _, ent := range subEntitlements {
				if lo.Contains(req.FeatureIDs, ent.FeatureID) {
					allEntitlements = append(allEntitlements, ent)
				}
			}
		} else {
			allEntitlements = append(allEntitlements, subEntitlements...)
		}
	}

	// Use the generic aggregation function
	aggregatedFeatures := s.AggregateEntitlements(allEntitlements, subscriptions[0].ID)

	// Build final response
	response := &dto.CustomerEntitlementsResponse{
		CustomerID: customerID,
		Features:   aggregatedFeatures,
	}

	return response, nil
}

func (s *billingService) GetCustomerUsageSummary(ctx context.Context, customerID string, req *dto.GetCustomerUsageSummaryRequest) (*dto.CustomerUsageSummaryResponse, error) {
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	eventService := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger, s.Config)

	// get customer
	customer, err := s.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		return nil, err
	}

	// Convert feature lookup keys to IDs if provided
	featureIDs := req.FeatureIDs
	if len(req.FeatureLookupKeys) > 0 {
		// Use built-in LookupKeys filter for efficient batch lookup
		filter := types.NewDefaultFeatureFilter()
		filter.LookupKeys = req.FeatureLookupKeys
		features, err := s.FeatureRepo.List(ctx, filter)
		if err != nil {
			return nil, err
		}
		for _, f := range features {
			featureIDs = append(featureIDs, f.ID)
		}
	}

	// 1. Get customer entitlements first
	entitlementsReq := &dto.GetCustomerEntitlementsRequest{
		SubscriptionIDs: req.SubscriptionIDs,
		FeatureIDs:      featureIDs,
	}

	entitlements, err := s.GetCustomerEntitlements(ctx, customerID, entitlementsReq)
	if err != nil {
		return nil, err
	}

	// If no features found, return empty response
	if len(entitlements.Features) == 0 {
		return &dto.CustomerUsageSummaryResponse{
			CustomerID: customerID,
			Features:   make([]*dto.FeatureUsageSummary, 0),
		}, nil
	}

	// 2. Build subscription and feature maps for efficient lookup
	subscriptionMap := make(map[string]*subscription.Subscription)
	featureSubscriptionMap := make(map[string]*subscription.Subscription) // feature ID -> subscription
	usageByFeature := make(map[string]decimal.Decimal)
	meterFeatureMap := make(map[string]string)
	featureMeterMap := make(map[string]string) // feature ID -> meter ID
	featureUsageResetPeriodMap := make(map[string]types.EntitlementUsageResetPeriod)

	// Collect unique subscription IDs and build feature maps
	subscriptionIDs := make([]string, 0)
	for _, feature := range entitlements.Features {
		usageByFeature[feature.Feature.ID] = decimal.Zero
		meterFeatureMap[feature.Feature.MeterID] = feature.Feature.ID
		featureMeterMap[feature.Feature.ID] = feature.Feature.MeterID
		featureUsageResetPeriodMap[feature.Feature.ID] = feature.Entitlement.UsageResetPeriod

		// Map feature to its subscription (use first source)
		if len(feature.Sources) > 0 {
			subscriptionIDs = append(subscriptionIDs, feature.Sources[0].SubscriptionID)
		}
	}
	subscriptionIDs = lo.Uniq(subscriptionIDs)

	// Fetch all subscriptions at once
	for _, subscriptionID := range subscriptionIDs {
		sub, err := s.SubRepo.Get(ctx, subscriptionID)
		if err != nil {
			s.Logger.Warnw("failed to get subscription", "subscription_id", subscriptionID, "error", err)
			continue
		}
		subscriptionMap[subscriptionID] = sub
	}

	// Map features to their subscriptions
	for _, feature := range entitlements.Features {
		if len(feature.Sources) > 0 {
			subscriptionID := feature.Sources[0].SubscriptionID
			if sub, exists := subscriptionMap[subscriptionID]; exists {
				featureSubscriptionMap[feature.Feature.ID] = sub
			}
		}
	}

	// 3. Process usage data for each subscription
	for _, subscriptionID := range subscriptionIDs {
		sub := subscriptionMap[subscriptionID]
		if sub == nil {
			continue
		}

		usageReq := &dto.GetUsageBySubscriptionRequest{
			SubscriptionID: subscriptionID,
		}

		usage, err := subscriptionService.GetUsageBySubscription(ctx, usageReq)
		if err != nil {
			s.Logger.Warnw("failed to get usage for subscription", "subscription_id", subscriptionID, "error", err)
			continue
		}

		// Process usage data for features that have charges
		for _, charge := range usage.Charges {
			if featureID, ok := meterFeatureMap[charge.MeterID]; ok {
				resetPeriod := featureUsageResetPeriodMap[featureID]
				if resetPeriod.String() == sub.BillingPeriod.String() {
					currentUsage := usageByFeature[featureID]
					usageByFeature[featureID] = currentUsage.Add(decimal.NewFromFloat(charge.Quantity))
				} else if resetPeriod == types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY {
					// Handle daily reset features: get today's usage from daily windows
					meterID := featureMeterMap[featureID]
					// Create usage request with daily window size for current billing period
					usageRequest := &dto.GetUsageByMeterRequest{
						MeterID:            meterID,
						ExternalCustomerID: customer.ExternalID,
						StartTime:          sub.CurrentPeriodStart,
						EndTime:            sub.CurrentPeriodEnd,
						WindowSize:         types.WindowSizeDay,
					}

					// Get usage data with daily windows
					usageResult, err := eventService.GetUsageByMeter(ctx, usageRequest)
					if err != nil {
						s.Logger.Warnw("failed to get daily usage for feature",
							"feature_id", featureID,
							"meter_id", meterID,
							"subscription_id", subscriptionID,
							"error", err)
						continue
					}

					// Pick the last bucket (today's usage) if available
					dailyUsage := decimal.Zero
					today := time.Now().In(sub.CurrentPeriodStart.Location())
					todayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())

					todayEnd := todayStart.AddDate(0, 0, 1)
					if len(usageResult.Results) > 0 {
						lastBucket := usageResult.Results[len(usageResult.Results)-1]
						// check if last bucket is today's usage
						if (lastBucket.WindowSize.After(todayStart) || lastBucket.WindowSize.Equal(todayStart)) && lastBucket.WindowSize.Before(todayEnd) {
							dailyUsage = lastBucket.Value
						}

						s.Logger.Debugw("using daily usage for feature summary",
							"customer_id", customerID,
							"external_customer_id", customer.ExternalID,
							"feature_id", featureID,
							"meter_id", meterID,
							"subscription_id", subscriptionID,
							"today_usage", dailyUsage,
							"today_start", todayStart,
							"today_end", todayEnd,
							"last_bucket", lastBucket.WindowSize,
							"last_bucket_value", lastBucket.Value,
							"total_daily_windows", len(usageResult.Results))
					}
					usageByFeature[featureID] = dailyUsage
				} else if resetPeriod == types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY {
					// Handle monthly reset features: get current month's usage from monthly windows
					meterID := featureMeterMap[featureID]

					// Create usage request for current month with monthly window size
					usageRequest := &dto.GetUsageByMeterRequest{
						MeterID:            meterID,
						ExternalCustomerID: customer.ExternalID,
						StartTime:          sub.CurrentPeriodStart,
						EndTime:            sub.CurrentPeriodEnd,
						WindowSize:         types.WindowSizeMonth,
						BillingAnchor:      &sub.BillingAnchor,
					}

					// Get usage data for current month
					usageResult, err := eventService.GetUsageByMeter(ctx, usageRequest)
					if err != nil {
						s.Logger.Warnw("failed to get monthly usage for feature",
							"feature_id", featureID,
							"meter_id", meterID,
							"subscription_id", subscriptionID,
							"error", err)
						continue
					}

					// Get the current month's usage (last bucket if available)
					monthlyUsage := decimal.Zero
					currentTime := time.Now().In(sub.CurrentPeriodStart.Location())
					if len(usageResult.Results) > 0 {
						// Find the current month's bucket
						for _, result := range usageResult.Results {
							windowStart := result.WindowSize
							// Calculate window end (next month's start)
							windowEnd := windowStart.AddDate(0, 1, 0)
							// TODO : critical think of cliff cases here ex 28th feb of a leap year adding 1 month
							// will miss factoring in the 29th feb from this bucket
							// TODO : move this all to flexprice calculated buckets logics upfront
							// rather than relying on clickhouse calculated window sizes

							// Check if current time falls within this window
							if (currentTime.Equal(windowStart) || currentTime.After(windowStart)) && currentTime.Before(windowEnd) {
								monthlyUsage = result.Value
								break
							}
						}
					}

					s.Logger.Debugw("using monthly usage for feature summary",
						"customer_id", customerID,
						"external_customer_id", customer.ExternalID,
						"feature_id", featureID,
						"meter_id", meterID,
						"subscription_id", subscriptionID,
						"current_time", currentTime,
						"monthly_usage", monthlyUsage)

					usageByFeature[featureID] = monthlyUsage
				} else if resetPeriod == types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER {
					// Handle never reset features: get cumulative usage from subscription start
					meterID := featureMeterMap[featureID]

					// For never reset features, calculate cumulative usage from subscription start to current period end
					// This maintains consistency with the billing logic
					totalUsageRequest := &dto.GetUsageByMeterRequest{
						MeterID:            meterID,
						ExternalCustomerID: customer.ExternalID,
						StartTime:          sub.StartDate,
						EndTime:            sub.CurrentPeriodEnd,
					}

					totalUsageResult, err := eventService.GetUsageByMeter(ctx, totalUsageRequest)
					if err != nil {
						s.Logger.Warnw("failed to get total usage for never reset feature",
							"feature_id", featureID,
							"meter_id", meterID,
							"subscription_id", subscriptionID,
							"error", err)
						continue
					}

					// Calculate total cumulative usage from subscription start
					usageByFeature[featureID] = totalUsageResult.Value

					s.Logger.Debugw("using cumulative usage for never reset feature summary",
						"customer_id", customerID,
						"external_customer_id", customer.ExternalID,
						"feature_id", featureID,
						"meter_id", meterID,
						"subscription_id", subscriptionID,
						"subscription_start", sub.StartDate,
						"current_period_end", sub.CurrentPeriodEnd,
						"total_cumulative_usage", totalUsageResult.Value)
				}
			}
		}
	}

	currentTime := time.Now().UTC()
	// 4. Calculate next usage reset at for metered features only
	// Boolean and static features don't have usage reset periods
	featureNextUsageResetAtMap := make(map[string]*time.Time)
	for _, feature := range entitlements.Features {
		featureID := feature.Feature.ID
		// Only calculate reset time for metered features
		if types.FeatureType(feature.Feature.Type) != types.FeatureTypeMetered {
			continue
		}
		if sub, exists := featureSubscriptionMap[featureID]; exists {
			resetPeriod := featureUsageResetPeriodMap[featureID]
			// Skip if reset period is empty (shouldn't happen for metered, but defensive check)
			if resetPeriod == "" {
				continue
			}
			nextUsageResetAt, err := types.GetNextUsageResetAt(currentTime, sub.StartDate, sub.EndDate, sub.BillingAnchor, resetPeriod)
			if err != nil {
				s.Logger.Warnw("failed to get next usage reset at for feature",
					"feature_id", featureID,
					"subscription_id", sub.ID,
					"error", err)
				continue
			}
			featureNextUsageResetAtMap[featureID] = &nextUsageResetAt
		}
	}

	// 5. Sort features by type and name
	features := entitlements.Features
	featureOrder := map[types.FeatureType]int{
		types.FeatureTypeMetered: 1,
		types.FeatureTypeStatic:  2,
		types.FeatureTypeBoolean: 3,
	}

	sort.SliceStable(features, func(i, j int) bool {
		// Compare by FeatureType priority first
		if featureOrder[features[i].Feature.Type] != featureOrder[features[j].Feature.Type] {
			return featureOrder[features[i].Feature.Type] < featureOrder[features[j].Feature.Type]
		}
		// If same FeatureType, sort by Name alphabetically
		return features[i].Feature.Name < features[j].Feature.Name
	})

	// 6. Build final response combining entitlements and usage
	resp := &dto.CustomerUsageSummaryResponse{
		CustomerID: customerID,
		Features:   make([]*dto.FeatureUsageSummary, 0, len(features)),
	}

	for _, feature := range features {
		featureID := feature.Feature.ID
		usage := usageByFeature[featureID]
		nextUsageResetAt := featureNextUsageResetAtMap[featureID]

		featureSummary := &dto.FeatureUsageSummary{
			Feature:          feature.Feature,
			TotalLimit:       feature.Entitlement.UsageLimit,
			IsUnlimited:      feature.Entitlement.UsageLimit == nil,
			CurrentUsage:     usage,
			UsagePercent:     s.getUsagePercent(usage, feature.Entitlement.UsageLimit),
			IsEnabled:        feature.Entitlement.IsEnabled,
			IsSoftLimit:      feature.Entitlement.IsSoftLimit,
			Sources:          feature.Sources,
			NextUsageResetAt: nextUsageResetAt,
		}

		resp.Features = append(resp.Features, featureSummary)
	}

	return resp, nil
}

func (s *billingService) getUsagePercent(usage decimal.Decimal, limit *int64) decimal.Decimal {
	if limit == nil {
		return decimal.Zero
	}

	if *limit <= 0 {
		return decimal.NewFromInt(100)
	}

	return usage.Div(decimal.NewFromInt(*limit))
}

// calculateNeverResetUsage calculates billable usage for never reset entitlements with line item lifecycle awareness
// This function is optimized for period-end billing scenarios where we need to calculate cumulative usage
// that respects line item boundaries and lifecycle states.
//
// Never Reset Entitlement Logic:
// - Usage accumulates from subscription start date and never resets
// - Respects line item lifecycle: active, expired, or future states
// - Only bills for the intersection of line item active period and billing period
// - Handles line item transitions gracefully (similar to plan sync logic)
//
// Calculation Method:
// - totalUsage: From subscription start to line item period end
// - previousPeriodUsage: From subscription start to line item period start
// - billableQuantity: totalUsage - previousPeriodUsage - usageAllowed
// - Ensures billable quantity is never negative (max with zero)
func (s *billingService) calculateNeverResetUsage(
	ctx context.Context,
	sub *subscription.Subscription,
	item *subscription.SubscriptionLineItem,
	customer *customer.Customer,
	eventService EventService,
	periodStart,
	periodEnd time.Time,
	usageAllowed decimal.Decimal,
) (decimal.Decimal, error) {

	// Calculate line item period boundaries
	lineItemPeriodStart := item.GetPeriodStart(periodStart)
	lineItemPeriodEnd := item.GetPeriodEnd(periodEnd)

	// For never reset entitlements, calculate cumulative usage from subscription start
	// This maintains the "never reset" behavior while respecting line item boundaries

	// Get total cumulative usage from subscription start to line item period end
	totalUsageRequest := &dto.GetUsageByMeterRequest{
		MeterID:            item.MeterID,
		PriceID:            item.PriceID,
		ExternalCustomerID: customer.ExternalID,
		StartTime:          sub.StartDate,
		EndTime:            lineItemPeriodEnd,
		BillingAnchor:      &sub.BillingAnchor,
	}

	totalUsageResult, err := eventService.GetUsageByMeter(ctx, totalUsageRequest)
	if err != nil {
		return decimal.Zero, err
	}

	// Get cumulative usage from subscription start to line item period start
	// This represents usage that was already billed in previous periods
	previousPeriodUsageRequest := &dto.GetUsageByMeterRequest{
		MeterID:            item.MeterID,
		PriceID:            item.PriceID,
		ExternalCustomerID: customer.ExternalID,
		StartTime:          sub.StartDate,
		EndTime:            lineItemPeriodStart,
	}

	previousPeriodUsageResult, err := eventService.GetUsageByMeter(ctx, previousPeriodUsageRequest)
	if err != nil {
		return decimal.Zero, err
	}

	// Calculate cumulative usage totals
	totalUsage := totalUsageResult.Value
	previousPeriodUsage := previousPeriodUsageResult.Value

	// Calculate billable quantity = totalUsage - previousPeriodUsage - usageAllowed
	periodUsage := totalUsage.Sub(previousPeriodUsage)
	billableQuantity := totalUsage.Sub(previousPeriodUsage).Sub(usageAllowed)

	// Ensure billable quantity is not negative
	billableQuantity = decimal.Max(billableQuantity, decimal.Zero)

	s.Logger.Debugw("calculated never reset usage for line item",
		"line_item_id", item.ID,
		"meter_id", item.MeterID,
		"subscription_start", sub.StartDate,
		"line_item_period_start", lineItemPeriodStart,
		"line_item_period_end", lineItemPeriodEnd,
		"total_cumulative_usage", totalUsage,
		"previous_period_usage", previousPeriodUsage,
		"period_usage", periodUsage,
		"usage_allowed", usageAllowed,
		"billable_quantity", billableQuantity)

	return billableQuantity, nil
}
