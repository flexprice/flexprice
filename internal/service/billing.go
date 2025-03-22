package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
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

	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)

	// Process fixed charges from line items
	for _, item := range sub.LineItems {
		if item.PriceType != types.PRICE_TYPE_FIXED {
			continue
		}

		price, err := priceService.GetPrice(ctx, item.PriceID)
		if err != nil {
			return nil, fixedCost, err
		}

		amount := priceService.CalculateCost(ctx, price.Price, item.Quantity)

		fixedCostLineItems = append(fixedCostLineItems, dto.CreateInvoiceLineItemRequest{
			PlanID:          lo.ToPtr(item.PlanID),
			PlanDisplayName: lo.ToPtr(item.PlanDisplayName),
			PriceID:         item.PriceID,
			PriceType:       lo.ToPtr(string(item.PriceType)),
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
	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)
	entitlementService := NewEntitlementService(s.EntitlementRepo, s.PlanRepo, s.FeatureRepo, s.MeterRepo, s.Logger)

	if usage == nil {
		return nil, decimal.Zero, nil
	}

	usageCharges := make([]dto.CreateInvoiceLineItemRequest, 0)
	totalUsageCost := decimal.Zero

	planIDs := make([]string, 0)
	for _, item := range sub.LineItems {
		if item.PriceType == types.PRICE_TYPE_USAGE {
			planIDs = append(planIDs, item.PlanID)
		}
	}
	planIDs = lo.Uniq(planIDs)

	// map of plan ID to meter ID to entitlement
	entitlementsByPlanMeterID := make(map[string]map[string]*dto.EntitlementResponse)
	for _, planID := range planIDs {
		entitlements, err := entitlementService.GetPlanEntitlements(ctx, planID)
		if err != nil {
			return nil, decimal.Zero, err
		}

		for _, entitlement := range entitlements.Items {
			if entitlement.FeatureType == types.FeatureTypeMetered {
				if _, ok := entitlementsByPlanMeterID[planID]; !ok {
					entitlementsByPlanMeterID[planID] = make(map[string]*dto.EntitlementResponse)
				}
				entitlementsByPlanMeterID[planID][entitlement.Feature.MeterID] = entitlement
			}
		}
	}

	// Process usage charges from line items
	for _, item := range sub.LineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}

		// Find matching usage charge
		var matchingCharge *dto.SubscriptionUsageByMetersResponse
		for _, charge := range usage.Charges {
			if charge.Price.ID == item.PriceID {
				matchingCharge = charge
				break
			}
		}

		if matchingCharge == nil {
			s.Logger.Debugw("no matching charge found for usage line item",
				"subscription_id", sub.ID,
				"line_item_id", item.ID,
				"price_id", item.PriceID)
			continue
		}

		quantityForCalculation := decimal.NewFromFloat(matchingCharge.Quantity)
		matchingEntitlement, ok := entitlementsByPlanMeterID[item.PlanID][item.MeterID]
		if ok && matchingEntitlement != nil {
			if matchingEntitlement.UsageLimit != nil {
				// usage limit is set, so we decrement the usage quantity by the already entitled usage
				usageAllowed := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
				adjustedQuantity := decimal.NewFromFloat(matchingCharge.Quantity).Sub(usageAllowed)
				quantityForCalculation = decimal.Max(adjustedQuantity, decimal.Zero)
			} else {
				// unlimited usage allowed, so we set the usage quantity for calculation to 0
				quantityForCalculation = decimal.Zero
			}
		}

		// Recompute the amount based on the quantity for calculation
		lineItemAmount := priceService.CalculateCost(ctx, matchingCharge.Price, quantityForCalculation)
		totalUsageCost = totalUsageCost.Add(lineItemAmount)

		s.Logger.Debugw("usage charges for line item",
			"original_amount", matchingCharge.Amount,
			"calculated_amount", lineItemAmount,
			"original_quantity", matchingCharge.Quantity,
			"calculated_quantity", quantityForCalculation,
			"subscription_id", sub.ID,
			"line_item_id", item.ID,
			"price_id", item.PriceID)

		// TODO: for now we skip line items with no usage charges
		// but this needs to be behind a feature flag as per tenant config
		if lineItemAmount.Equal(decimal.Zero) {
			continue
		}

		usageCharges = append(usageCharges, dto.CreateInvoiceLineItemRequest{
			PlanID:           lo.ToPtr(item.PlanID),
			PlanDisplayName:  lo.ToPtr(item.PlanDisplayName),
			PriceType:        lo.ToPtr(string(item.PriceType)),
			PriceID:          item.PriceID,
			MeterID:          lo.ToPtr(item.MeterID),
			MeterDisplayName: lo.ToPtr(item.MeterDisplayName),
			DisplayName:      lo.ToPtr(item.DisplayName),
			Amount:           lineItemAmount,
			Quantity:         quantityForCalculation,
			PeriodStart:      lo.ToPtr(periodStart),
			PeriodEnd:        lo.ToPtr(periodEnd),
			Metadata: types.Metadata{
				"description": fmt.Sprintf("%s (Usage Charge)", item.DisplayName),
			},
		})
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
			return nil, ierr.NewError("no charges to invoice").
				WithHint("All charges have already been invoiced").
				Mark(ierr.ErrAlreadyExists)
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
		arrearResult, err := s.CalculateCharges(
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
		advanceResult, err := s.CalculateCharges(
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
func (s *billingService) checkIfChargeInvoiced(
	invoice *invoice.Invoice,
	charge *subscription.SubscriptionLineItem,
	periodStart,
	periodEnd time.Time,
) bool {
	for _, item := range invoice.LineItems {
		// match the price id
		if item.PriceID == charge.PriceID {
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
	// Prepare invoice due date
	invoiceDueDate := periodEnd.Add(24 * time.Hour * types.InvoiceDefaultDueDays)

	if result == nil {
		// prepare result for zero amount invoice
		result = &BillingCalculationResult{
			TotalAmount:  decimal.Zero,
			Currency:     sub.Currency,
			FixedCharges: make([]dto.CreateInvoiceLineItemRequest, 0),
			UsageCharges: make([]dto.CreateInvoiceLineItemRequest, 0),
		}
	}

	// Create invoice request
	req := &dto.CreateInvoiceRequest{
		CustomerID:     sub.CustomerID,
		SubscriptionID: lo.ToPtr(sub.ID),
		InvoiceType:    types.InvoiceTypeSubscription,
		InvoiceStatus:  lo.ToPtr(types.InvoiceStatusDraft),
		PaymentStatus:  lo.ToPtr(types.PaymentStatusPending),
		Currency:       sub.Currency,
		AmountDue:      result.TotalAmount,
		Description:    description,
		DueDate:        lo.ToPtr(invoiceDueDate),
		BillingPeriod:  lo.ToPtr(string(sub.BillingPeriod)),
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		BillingReason:  types.InvoiceBillingReasonSubscriptionCycle,
		EnvironmentID:  sub.EnvironmentID,
		Metadata:       metadata,
		LineItems:      append(result.FixedCharges, result.UsageCharges...),
	}

	return req, nil
}

// Helper functions for aggregating entitlements
func aggregateMeteredEntitlementsForBilling(entitlements []*entitlement.Entitlement) *dto.AggregatedEntitlement {
	var totalLimit int64 = 0
	isSoftLimit := true
	var usageResetPeriod types.BillingPeriod

	// First pass: check if any hard limits exist
	for _, e := range entitlements {
		if e.IsEnabled && !e.IsSoftLimit {
			isSoftLimit = false
			break
		}
	}

	// Second pass: calculate total limit based on soft/hard limit policy
	if isSoftLimit {
		// For soft limits, sum all limits
		for _, e := range entitlements {
			if e.IsEnabled && e.UsageLimit != nil {
				totalLimit += *e.UsageLimit
			}
		}
	} else {
		// For hard limits, use the minimum non-zero limit
		var minLimit *int64
		for _, e := range entitlements {
			if e.IsEnabled && e.UsageLimit != nil && !e.IsSoftLimit {
				if minLimit == nil || *e.UsageLimit < *minLimit {
					minLimit = e.UsageLimit
				}
			}
		}
		if minLimit != nil {
			totalLimit = *minLimit
		}
	}

	// Determine reset period (use most common)
	resetPeriodCounts := make(map[types.BillingPeriod]int)
	for _, e := range entitlements {
		if e.IsEnabled && e.UsageResetPeriod != "" {
			resetPeriodCounts[e.UsageResetPeriod]++
		}
	}

	maxCount := 0
	for period, count := range resetPeriodCounts {
		if count > maxCount {
			maxCount = count
			usageResetPeriod = period
		}
	}

	return &dto.AggregatedEntitlement{
		IsEnabled:        len(entitlements) > 0,
		UsageLimit:       &totalLimit,
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

func (s *billingService) GetCustomerEntitlements(ctx context.Context, customerID string, req *dto.GetCustomerEntitlementsRequest) (*dto.CustomerEntitlementsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	resp := &dto.CustomerEntitlementsResponse{
		CustomerID: customerID,
		Features:   []*dto.AggregatedFeature{},
	}

	// 1. Get active subscriptions for the customer
	subscriptions, err := s.SubRepo.ListByCustomerID(ctx, customerID)
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

	// 2. Extract plan IDs from active line items in subscriptions
	planIDs := make([]string, 0)
	subscriptionMap := make(map[string]*subscription.Subscription)

	for _, sub := range subscriptions {
		subscriptionMap[sub.ID] = sub
		for _, li := range sub.LineItems {
			if li.IsActive() {
				planIDs = append(planIDs, li.PlanID)
			}
		}
	}
	// Deduplicate plan IDs
	planIDs = lo.Uniq(planIDs)

	// 3. Get plans for the subscriptions
	planFilter := types.NewNoLimitPlanFilter()
	planFilter.PlanIDs = planIDs
	plans, err := s.PlanRepo.List(ctx, planFilter)
	if err != nil {
		return nil, err
	}

	// Create a map of plan IDs to plans for easy lookup
	planMap := make(map[string]*plan.Plan)
	for _, p := range plans {
		planMap[p.ID] = p
	}

	// 4. Get entitlements for the plans
	entitlements, err := s.EntitlementRepo.ListByPlanIDs(ctx, planIDs)
	if err != nil {
		return nil, err
	}

	// Filter by feature IDs if provided
	if len(req.FeatureIDs) > 0 {
		filteredEntitlements := make([]*entitlement.Entitlement, 0)
		for _, e := range entitlements {
			if lo.Contains(req.FeatureIDs, e.FeatureID) {
				filteredEntitlements = append(filteredEntitlements, e)
			}
		}
		entitlements = filteredEntitlements
	}

	if len(entitlements) == 0 {
		return resp, nil
	}

	// 5. Get all unique feature IDs and organize entitlements
	featureIDs := make([]string, 0)

	// Map of plan ID to its entitlements
	entitlementsByPlan := make(map[string][]*entitlement.Entitlement)

	for _, e := range entitlements {
		featureIDs = append(featureIDs, e.FeatureID)
		if _, ok := entitlementsByPlan[e.PlanID]; !ok {
			entitlementsByPlan[e.PlanID] = make([]*entitlement.Entitlement, 0)
		}
		entitlementsByPlan[e.PlanID] = append(entitlementsByPlan[e.PlanID], e)
	}
	featureIDs = lo.Uniq(featureIDs)

	// 6. Get features
	features, err := s.FeatureRepo.ListByIDs(ctx, featureIDs)
	if err != nil {
		return nil, err
	}

	// Create a map of feature IDs to features for easy lookup
	featureMap := make(map[string]*feature.Feature)
	for _, f := range features {
		featureMap[f.ID] = f
	}

	// 7. Group entitlements by feature (across all subscriptions and line items)
	// This will be used to create our final response with one entry per feature
	entitlementsByFeature := make(map[string][]*entitlement.Entitlement)

	// Track sources for each feature
	sourcesByFeature := make(map[string][]*dto.EntitlementSource)
	// Use a map to deduplicate sources by unique key
	sourceDedupeMap := make(map[string]bool)

	// Process each subscription and its line items
	for _, sub := range subscriptions {
		// Process each line item in the subscription
		for _, li := range sub.LineItems {
			if !li.IsActive() {
				continue
			}

			// Get entitlements for this plan
			planEntitlements, ok := entitlementsByPlan[li.PlanID]
			if !ok {
				continue
			}

			// Get the plan details
			p, ok := planMap[li.PlanID]
			if !ok {
				continue
			}

			// Convert quantity to int (floor the decimal)
			quantity := li.Quantity.IntPart()
			if quantity <= 0 {
				quantity = 1 // Ensure at least 1 quantity
			}

			// Process each entitlement for this plan
			for _, e := range planEntitlements {
				// Create a unique key for deduplication
				sourceKey := fmt.Sprintf("%s-%s-%s-%s", e.FeatureID, sub.ID, p.ID, e.ID)
				if sourceDedupeMap[sourceKey] {
					continue // Skip if we've already processed this source
				}
				sourceDedupeMap[sourceKey] = true

				// Create a source for this entitlement
				source := &dto.EntitlementSource{
					SubscriptionID: sub.ID,
					PlanID:         p.ID,
					PlanName:       p.Name,
					Quantity:       quantity,
					EntitlementID:  e.ID,
					IsEnabled:      e.IsEnabled,
					UsageLimit:     e.UsageLimit,
					StaticValue:    e.StaticValue,
				}

				// Initialize feature collections if needed
				if _, ok := entitlementsByFeature[e.FeatureID]; !ok {
					entitlementsByFeature[e.FeatureID] = make([]*entitlement.Entitlement, 0)
					sourcesByFeature[e.FeatureID] = make([]*dto.EntitlementSource, 0)
				}

				// Add source to feature sources
				sourcesByFeature[e.FeatureID] = append(sourcesByFeature[e.FeatureID], source)

				// For each quantity of the line item, add the entitlement
				for range quantity {
					// Duplicate the entitlement for each quantity
					entitlementCopy := *e // Make a copy to avoid modifying the original
					entitlementsByFeature[e.FeatureID] = append(entitlementsByFeature[e.FeatureID], &entitlementCopy)
				}
			}
		}
	}

	// 8. Aggregate entitlements by feature and build the response
	aggregatedFeatures := make([]*dto.AggregatedFeature, 0, len(featureIDs))

	for featureID, featureEntitlements := range entitlementsByFeature {
		f, ok := featureMap[featureID]
		if !ok {
			// Skip if feature not found
			continue
		}

		// Create feature response
		featureResponse := &dto.FeatureResponse{Feature: f}

		// Aggregate entitlements based on feature type
		var aggregatedEntitlement *dto.AggregatedEntitlement
		switch f.Type {
		case types.FeatureTypeMetered:
			aggregatedEntitlement = aggregateMeteredEntitlementsForBilling(featureEntitlements)
		case types.FeatureTypeBoolean:
			aggregatedEntitlement = aggregateBooleanEntitlementsForBilling(featureEntitlements)
		case types.FeatureTypeStatic:
			aggregatedEntitlement = aggregateStaticEntitlementsForBilling(featureEntitlements)
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

	// 9. Build final response
	response := &dto.CustomerEntitlementsResponse{
		CustomerID: customerID,
		Features:   aggregatedFeatures,
	}

	return response, nil
}

func (s *billingService) GetCustomerUsageSummary(ctx context.Context, customerID string, req *dto.GetCustomerUsageSummaryRequest) (*dto.CustomerUsageSummaryResponse, error) {
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	// 1. Get customer entitlements first
	entitlementsReq := &dto.GetCustomerEntitlementsRequest{
		SubscriptionIDs: req.SubscriptionIDs,
		FeatureIDs:      req.FeatureIDs,
	}

	entitlements, err := s.GetCustomerEntitlements(ctx, customerID, entitlementsReq)
	if err != nil {
		return nil, err
	}

	subscriptionIDs := make([]string, 0)
	for _, entitlement := range entitlements.Features {
		for _, source := range entitlement.Sources {
			subscriptionIDs = append(subscriptionIDs, source.SubscriptionID)
		}
	}
	subscriptionIDs = lo.Uniq(subscriptionIDs)

	// 2. Initialize response with customer ID
	resp := &dto.CustomerUsageSummaryResponse{
		CustomerID: customerID,
		Features:   make([]*dto.FeatureUsageSummary, 0),
	}

	// If no features found, return empty response
	if len(entitlements.Features) == 0 {
		return resp, nil
	}

	// 3. Create a map to track usage by feature ID
	usageByFeature := make(map[string]decimal.Decimal)
	meterFeatureMap := make(map[string]string)

	for _, feature := range entitlements.Features {
		usageByFeature[feature.Feature.ID] = decimal.Zero
		meterFeatureMap[feature.Feature.MeterID] = feature.Feature.ID
	}

	// 4. Get usage for each subscription
	for _, subscriptionID := range subscriptionIDs {
		usageReq := &dto.GetUsageBySubscriptionRequest{
			SubscriptionID: subscriptionID,
		}

		usage, err := subscriptionService.GetUsageBySubscription(ctx, usageReq)
		if err != nil {
			return nil, err
		}

		// Add usage if found for this feature
		for _, charge := range usage.Charges {
			if featureID, ok := meterFeatureMap[charge.MeterID]; ok {
				currentUsage := usageByFeature[featureID]
				usageByFeature[featureID] = currentUsage.Add(decimal.NewFromFloat(charge.Quantity))
			}
		}
	}

	// 5. Build final response combining entitlements and usage
	for _, feature := range entitlements.Features {
		featureID := feature.Feature.ID
		usage := usageByFeature[featureID]

		featureSummary := &dto.FeatureUsageSummary{
			Feature:      feature.Feature,
			TotalLimit:   feature.Entitlement.UsageLimit,
			CurrentUsage: usage,
			UsagePercent: s.getUsagePercent(usage, feature.Entitlement.UsageLimit),
			IsEnabled:    feature.Entitlement.IsEnabled,
			IsSoftLimit:  feature.Entitlement.IsSoftLimit,
			Sources:      feature.Sources,
		}

		resp.Features = append(resp.Features, featureSummary)
	}

	return resp, nil
}

func (s *billingService) getUsagePercent(usage decimal.Decimal, limit *int64) decimal.Decimal {
	if limit == nil {
		return decimal.Zero
	}

	if *limit == 0 {
		return decimal.Zero
	}

	return usage.Div(decimal.NewFromInt(*limit))
}
