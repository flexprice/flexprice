package service

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type prorationService struct {
	serviceParams  ServiceParams
	invoiceService InvoiceService
}

// NewProrationService creates a new proration service.
func NewProrationService(
	serviceParams ServiceParams,
) proration.Service {
	return &prorationService{
		serviceParams:  serviceParams,
		invoiceService: NewInvoiceService(serviceParams),
	}
}

// CalculateProration delegates to the underlying calculator.
func (s *prorationService) CalculateProration(ctx context.Context, params proration.ProrationParams) (*proration.ProrationResult, error) {
	calculator := s.serviceParams.ProrationCalculator
	s.serviceParams.Logger.Info("calculating proration",
		zap.String("subscription_id", params.SubscriptionID),
		zap.String("line_item_id", params.LineItemID),
		zap.String("action", string(params.Action)),
	)

	result, err := calculator.Calculate(ctx, params)
	if err != nil {
		s.serviceParams.Logger.Error("proration calculation failed",
			zap.Error(err),
			zap.String("subscription_id", params.SubscriptionID),
			zap.String("line_item_id", params.LineItemID),
		)
		return nil, ierr.NewErrorf("proration calculation failed: %v", err).
			WithHint("Check if the subscription and line item details are valid").
			Mark(ierr.ErrSystem)
	}

	s.serviceParams.Logger.Debug("proration calculation completed",
		zap.String("subscription_id", params.SubscriptionID),
		zap.String("line_item_id", params.LineItemID),
		zap.String("net_amount", result.NetAmount.String()),
	)

	return result, nil
}

// validateSubscriptionProrationParams validates the parameters for subscription proration calculation
func (s *prorationService) validateSubscriptionProrationParams(params proration.SubscriptionProrationParams) error {
	if params.Subscription == nil {
		return ierr.NewError("subscription is required").
			WithHint("Provide a valid subscription object").
			Mark(ierr.ErrValidation)
	}
	if params.Subscription.ID == "" {
		return ierr.NewError("subscription ID is required").
			WithHint("Provide a valid subscription ID").
			Mark(ierr.ErrValidation)
	}
	if params.Subscription.StartDate.IsZero() {
		return ierr.NewError("subscription start date is required").
			WithHint("Set a valid start date for the subscription").
			Mark(ierr.ErrValidation)
	}
	if params.Subscription.BillingAnchor.IsZero() {
		return ierr.NewError("subscription billing anchor is required").
			WithHint("Set a valid billing anchor date").
			Mark(ierr.ErrValidation)
	}
	if len(params.Subscription.LineItems) == 0 {
		return ierr.NewError("subscription must have at least one line item").
			WithHint("Add at least one line item to the subscription").
			Mark(ierr.ErrValidation)
	}
	if params.Prices == nil {
		return ierr.NewError("prices map is required").
			WithHint("Provide a valid prices map").
			Mark(ierr.ErrValidation)
	}

	// Validate each line item has a corresponding price
	for _, item := range params.Subscription.LineItems {
		if item.ID == "" {
			return ierr.NewError("line item ID is required").
				WithHint("Provide a valid ID for each line item").
				Mark(ierr.ErrValidation)
		}
		if item.PriceID == "" {
			return ierr.NewErrorf("price ID is required for line item %s", item.ID).
				WithHint("Set a valid price ID for each line item").
				Mark(ierr.ErrValidation)
		}
		if _, exists := params.Prices[item.PriceID]; !exists {
			return ierr.NewErrorf("price not found for line item %s with price ID %s", item.ID, item.PriceID).
				WithHint("Ensure all referenced prices exist").
				Mark(ierr.ErrNotFound)
		}
		if item.Quantity.IsNegative() {
			return ierr.NewErrorf("quantity must be positive for line item %s", item.ID).
				WithHint("Set a positive quantity for each line item").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// CalculateAndApplySubscriptionProration handles proration for an entire subscription.
func (s *prorationService) CalculateSubscriptionProration(
	ctx context.Context,
	params proration.SubscriptionProrationParams,
) (*proration.SubscriptionProrationResult, error) {
	if err := s.validateSubscriptionProrationParams(params); err != nil {
		return nil, ierr.NewErrorf("invalid subscription proration parameters: %v", err).
			WithHint("Check all required subscription parameters").
			Mark(ierr.ErrValidation)
	}

	logger := s.serviceParams.Logger
	logger.Infow("starting subscription proration calculation",
		"subscription_id", params.Subscription.ID,
		"billing_cycle", params.BillingCycle,
		"proration_behavior", params.ProrationBehavior,
		"line_items_count", len(params.Subscription.LineItems))

	result := &proration.SubscriptionProrationResult{
		LineItemResults: make(map[string]*proration.ProrationResult),
		Currency:        params.Subscription.Currency,
	}

	// Only proceed if proration is needed
	if params.BillingCycle != types.BillingCycleCalendar ||
		params.ProrationBehavior == types.ProrationBehaviorNone {
		logger.Infow("skipping proration - not needed",
			"subscription_id", params.Subscription.ID,
			"billing_cycle", params.BillingCycle,
			"proration_behavior", params.ProrationBehavior)
		return result, nil
	}

	// Calculate proration for each line item
	var errors []error
	for _, item := range params.Subscription.LineItems {
		price, ok := params.Prices[item.PriceID]
		if !ok {
			logger.Debugw("price not found for line item - skipping",
				"subscription_id", params.Subscription.ID,
				"line_item_id", item.ID,
				"price_id", item.PriceID)
			continue
		}

		if price == nil {
			logger.Debugw("price not found for line item - skipping",
				"subscription_id", params.Subscription.ID,
				"line_item_id", item.ID,
				"price_id", item.PriceID)
			continue
		}

		prorationParams, err := s.CreateProrationParamsForLineItem(
			params.Subscription,
			item,
			price,
			types.ProrationActionAddItem,
			params.ProrationBehavior,
		)
		if err != nil {
			logger.Errorw("failed to create proration parameters for line item",
				"error", err,
				"subscription_id", params.Subscription.ID,
				"line_item_id", item.ID)
			errors = append(errors, ierr.NewErrorf("line item %s: %v", item.ID, err).
				WithHint("Check line item configuration").
				Mark(ierr.ErrSystem))
			continue // Skip this item but continue with others
		}

		prorationResult, err := s.CalculateProration(ctx, prorationParams)
		if err != nil {
			logger.Errorw("failed to calculate proration for line item",
				"error", err,
				"subscription_id", params.Subscription.ID,
				"line_item_id", item.ID)
			errors = append(errors, ierr.NewErrorf("line item %s: %v", item.ID, err).
				WithHint("Check line item configuration").
				Mark(ierr.ErrSystem))
			continue // Skip this item but continue with others
		}

		// Set currency from the first valid price
		if result.Currency == "" && price.Currency != "" {
			result.Currency = price.Currency
		}

		prorationResult.BillingPeriod = params.Subscription.BillingPeriod
		result.LineItemResults[item.ID] = prorationResult
		result.TotalProrationAmount = result.TotalProrationAmount.Add(prorationResult.NetAmount)

		logger.Debugw("proration calculated for line item",
			"subscription_id", params.Subscription.ID,
			"line_item_id", item.ID,
			"net_amount", prorationResult.NetAmount.String(),
			"credit_items", len(prorationResult.CreditItems),
			"charge_items", len(prorationResult.ChargeItems))
	}

	if len(errors) > 0 {
		return nil, ierr.NewErrorf("failed to calculate proration for some line items: %v", errors).
			WithHint("Review errors for each failed line item").
			Mark(ierr.ErrSystem)
	}

	logger.Infow("proration calculation completed",
		"subscription_id", params.Subscription.ID,
		"total_amount", result.TotalProrationAmount.String(),
		"line_items_processed", len(result.LineItemResults))

	return result, nil
}

// CalculateSubscriptionCancellationProration handles proration calculation for subscription cancellation.
// This provides a single, unified function for calculating all proration changes during cancellation.
func (s *prorationService) CalculateSubscriptionCancellationProration(
	ctx context.Context,
	subscription *subscription.Subscription,
	lineItems []*subscription.SubscriptionLineItem,
	cancellationType types.CancellationType,
	effectiveDate time.Time,
	reason string,
	behavior types.ProrationBehavior,
) (*proration.SubscriptionProrationResult, error) {
	logger := s.serviceParams.Logger.With(
		zap.String("subscription_id", subscription.ID),
		zap.String("cancellation_type", string(cancellationType)),
		zap.String("reason", reason),
		zap.Int("line_items_count", len(lineItems)),
	)

	logger.Info("starting subscription cancellation proration calculation")

	// Initialize result
	result := &proration.SubscriptionProrationResult{
		LineItemResults:      make(map[string]*proration.ProrationResult),
		TotalProrationAmount: decimal.Zero,
		Currency:             subscription.Currency,
	}

	// Skip proration if behavior is none
	if behavior == types.ProrationBehaviorNone {
		logger.Info("skipping proration calculation - behavior is none")
		return result, nil
	}

	// Skip proration for end_of_period cancellations (typically no credits issued)
	if cancellationType == types.CancellationTypeEndOfPeriod {
		logger.Info("skipping proration calculation - end of period cancellation")
		return result, nil
	}

	var processingErrors []error
	processedCount := 0

	// Process each active line item
	for _, lineItem := range lineItems {
		if lineItem.Status != types.StatusPublished {
			logger.Debugw("skipping inactive line item",
				"line_item_id", lineItem.ID,
				"status", lineItem.Status)
			continue
		}

		// Get price for line item
		price, err := s.serviceParams.PriceRepo.Get(ctx, lineItem.PriceID)
		if err != nil {
			logger.Errorw("failed to get price for line item",
				"line_item_id", lineItem.ID,
				"price_id", lineItem.PriceID,
				"error", err)
			processingErrors = append(processingErrors,
				ierr.NewErrorf("line item %s: failed to get price: %v", lineItem.ID, err).
					Mark(ierr.ErrDatabase))
			continue
		}

		if price == nil {
			logger.Warnw("price not found for line item - skipping",
				"line_item_id", lineItem.ID,
				"price_id", lineItem.PriceID)
			continue
		}

		// Create proration parameters for cancellation
		params, err := s.CreateProrationParamsForLineItemCancellation(
			ctx,
			subscription,
			lineItem,
			price,
			effectiveDate,
			cancellationType,
			reason,
			behavior,
		)
		if err != nil {
			logger.Errorw("failed to create proration params",
				"line_item_id", lineItem.ID,
				"error", err)
			processingErrors = append(processingErrors,
				ierr.NewErrorf("line item %s: failed to create proration params: %v", lineItem.ID, err).
					Mark(ierr.ErrSystem))
			continue
		}

		// Calculate proration for this line item
		prorationResult, err := s.CalculateProration(ctx, params)
		if err != nil {
			logger.Errorw("failed to calculate proration",
				"line_item_id", lineItem.ID,
				"error", err)
			processingErrors = append(processingErrors,
				ierr.NewErrorf("line item %s: failed to calculate proration: %v", lineItem.ID, err).
					Mark(ierr.ErrSystem))
			continue
		}

		// Set billing period from subscription
		prorationResult.BillingPeriod = subscription.BillingPeriod

		// Store result
		result.LineItemResults[lineItem.ID] = prorationResult
		result.TotalProrationAmount = result.TotalProrationAmount.Add(prorationResult.NetAmount)

		processedCount++

		logger.Debugw("proration calculated for line item",
			"line_item_id", lineItem.ID,
			"net_amount", prorationResult.NetAmount.String(),
			"credit_items", len(prorationResult.CreditItems),
			"charge_items", len(prorationResult.ChargeItems))
	}

	// Handle processing errors
	if len(processingErrors) > 0 {
		if processedCount == 0 {
			// All line items failed - return error
			return nil, ierr.NewErrorf("failed to calculate proration for all line items: %v", processingErrors).
				WithHint("Review line item configurations and price data").
				Mark(ierr.ErrSystem)
		} else {
			// Some succeeded, some failed - log warnings but continue
			logger.Warnw("some line items failed proration calculation",
				"failed_count", len(processingErrors),
				"succeeded_count", processedCount,
				"errors", processingErrors)
		}
	}

	logger.Infow("subscription cancellation proration calculation completed",
		"subscription_id", subscription.ID,
		"total_proration_amount", result.TotalProrationAmount.String(),
		"line_items_processed", processedCount,
		"line_items_failed", len(processingErrors))

	return result, nil
}

// CreateProrationParamsForLineItemCancellation creates proration parameters for cancellation scenarios
func (s *prorationService) CreateProrationParamsForLineItemCancellation(
	ctx context.Context,
	subscription *subscription.Subscription,
	item *subscription.SubscriptionLineItem,
	price *price.Price,
	cancellationDate time.Time,
	cancellationType types.CancellationType,
	cancellationReason string,
	behavior types.ProrationBehavior,
) (proration.ProrationParams, error) {
	logger := s.serviceParams.Logger.With(
		zap.String("subscription_id", subscription.ID),
		zap.String("line_item_id", item.ID),
		zap.String("cancellation_type", string(cancellationType)),
	)

	logger.Info("creating proration parameters for cancellation")

	// Get billing period boundaries
	periodStart := subscription.CurrentPeriodStart

	periodEnd := subscription.CurrentPeriodEnd

	// Determine effective cancellation date based on type
	effectiveDate := cancellationDate
	switch cancellationType {
	case types.CancellationTypeEndOfPeriod:
		effectiveDate = periodEnd
		logger.Debugw("using end of period for cancellation", "effective_date", effectiveDate)
	case types.CancellationTypeImmediate:
		// Use provided cancellation date, but ensure it's not before period start
		if cancellationDate.Before(periodStart) {
			effectiveDate = periodStart
			logger.Warnw("cancellation date before period start, using period start",
				"requested_date", cancellationDate,
				"period_start", periodStart)
		}
	}

	// Validate effective date is within current period for immediate cancellations
	if cancellationType == types.CancellationTypeImmediate &&
		(effectiveDate.Before(periodStart) || effectiveDate.After(periodEnd)) {
		return proration.ProrationParams{}, ierr.NewError("cancellation date must be within current billing period").
			WithHintf("Period: %s to %s, Cancellation: %s",
				periodStart.Format("2006-01-02"),
				periodEnd.Format("2006-01-02"),
				effectiveDate.Format("2006-01-02")).
			Mark(ierr.ErrValidation)
	}

	// TODO: Implement credit capping to prevent over-crediting customers
	// This is critical for financial integrity - ensures customers never receive
	// more credits than they originally paid for the current billing period

	// TODO: Get original amount paid for this line item in current period
	// originalAmountPaid, err := s.getOriginalAmountPaidForLineItem(ctx, subscription.ID, item.ID, periodStart)
	// if err != nil {
	//     logger.Warnw("failed to get original amount paid, using calculated amount",
	//         "error", err,
	//         "line_item_id", item.ID)
	//     // Fallback to calculated amount based on current price and quantity
	//     originalAmountPaid = price.Amount.Mul(item.Quantity)
	// }

	// For now, use calculated amount as fallback
	originalAmountPaid := price.Amount.Mul(item.Quantity)

	// TODO: Get any previous credits issued for this line item in current period
	// previousCredits, err := s.getPreviousCreditsForLineItem(ctx, subscription.ID, item.ID, periodStart, effectiveDate)
	// if err != nil {
	//     logger.Warnw("failed to get previous credits, assuming zero",
	//         "error", err,
	//         "line_item_id", item.ID)
	//     previousCredits = decimal.Zero
	// }

	// For now, assume no previous credits
	previousCredits := decimal.Zero

	// Determine if customer is eligible for refund/credit
	refundEligible := s.isRefundEligible(subscription, item, price, cancellationType, effectiveDate)

	logger.Debugw("cancellation proration parameters calculated",
		"effective_date", effectiveDate,
		"original_amount_paid", originalAmountPaid.String(),
		"previous_credits", previousCredits.String(),
		"refund_eligible", refundEligible)

	return proration.ProrationParams{
		SubscriptionID:     subscription.ID,
		LineItemID:         item.ID,
		PlanPayInAdvance:   price.InvoiceCadence == types.InvoiceCadenceAdvance,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd.Add(time.Second * -1), // Subtract 1 second to avoid overlap
		Action:             types.ProrationActionCancellation,

		// For cancellation, we only have "old" values (what's being cancelled)
		OldPriceID:      item.PriceID,
		OldQuantity:     item.Quantity,
		OldPricePerUnit: price.Amount,
		NewPriceID:      "", // Nothing new for cancellation
		NewQuantity:     decimal.Zero,
		NewPricePerUnit: decimal.Zero,

		ProrationDate:     effectiveDate,
		ProrationBehavior: behavior,
		CustomerTimezone:  subscription.CustomerTimezone,
		ProrationStrategy: types.StrategySecondBased,
		Currency:          price.Currency,
		PlanDisplayName:   item.PlanDisplayName,
		TerminationReason: types.TerminationReasonCancellation,

		// Critical for credit capping
		OriginalAmountPaid:    originalAmountPaid,
		PreviousCreditsIssued: previousCredits,

		// Cancellation-specific fields
		CancellationType:   cancellationType,
		CancellationReason: cancellationReason,
		RefundEligible:     refundEligible,
	}, nil
}

// Helper method to create proration parameters for a line item (internal use)
func (s *prorationService) CreateProrationParamsForLineItem(
	subscription *subscription.Subscription,
	item *subscription.SubscriptionLineItem,
	price *price.Price,
	action types.ProrationAction,
	behavior types.ProrationBehavior,
) (proration.ProrationParams, error) {

	/*
		Why are we calculating the previous billing date?
		We need it to determine the start of the current billing period
		so we can calculate the total number of days in that period.

		Example:
		- Subscription created on 15 Aug 2025
		- Billing period: monthly
		- Billing anchor: 1st of the month

		In this case, the period start is 1 Aug 2025,
		which defines the full billing duration of 31 days.
	*/
	var periodStart time.Time
	if subscription.BillingCycle == types.BillingCycleAnniversary {
		periodStart = subscription.BillingAnchor
	} else {
		previousBillingDate, err := types.PreviousBillingDate(
			subscription.BillingAnchor,
			subscription.BillingPeriodCount,
			subscription.BillingPeriod,
		)
		if err != nil {
			// Fallback to current period start if calculation fails
			s.serviceParams.Logger.Warnw("failed to calculate period start for proration, using fallback",
				"error", err,
				"subscription_id", subscription.ID,
				"billing_anchor", subscription.BillingAnchor,
				"billing_period", subscription.BillingPeriod,
				"billing_period_count", subscription.BillingPeriodCount)
			periodStart = subscription.CurrentPeriodStart
		} else {
			periodStart = previousBillingDate
		}
	}
	return proration.ProrationParams{
		SubscriptionID:        subscription.ID,
		LineItemID:            item.ID,
		PlanPayInAdvance:      price.InvoiceCadence == types.InvoiceCadenceAdvance,
		CurrentPeriodStart:    periodStart,
		CurrentPeriodEnd:      subscription.CurrentPeriodEnd.Add(time.Second * -1),
		Action:                action,
		NewPriceID:            item.PriceID,
		NewQuantity:           item.Quantity,
		NewPricePerUnit:       price.Amount,
		ProrationDate:         subscription.StartDate,
		ProrationBehavior:     behavior,
		CustomerTimezone:      subscription.CustomerTimezone,
		OriginalAmountPaid:    decimal.Zero,
		PreviousCreditsIssued: decimal.Zero,
		ProrationStrategy:     types.StrategySecondBased,
		Currency:              price.Currency,
		PlanDisplayName:       item.PlanDisplayName,
	}, nil
}

// isRefundEligible determines if a customer is eligible for refund/credit based on cancellation scenario
func (s *prorationService) isRefundEligible(
	subscription *subscription.Subscription,
	item *subscription.SubscriptionLineItem,
	price *price.Price,
	cancellationType types.CancellationType,
	effectiveDate time.Time,
) bool {
	logger := s.serviceParams.Logger.With(
		zap.String("subscription_id", subscription.ID),
		zap.String("line_item_id", item.ID),
		zap.String("cancellation_type", string(cancellationType)),
	)

	// Basic eligibility rules
	switch cancellationType {
	case types.CancellationTypeEndOfPeriod:
		// End of period cancellations typically don't get credits
		// since customer uses service for full period
		logger.Debug("end of period cancellation - not eligible for refund")
		return false

	case types.CancellationTypeImmediate:
		// Immediate cancellations are eligible if they paid in advance
		// and there's unused time remaining
		if price.InvoiceCadence == types.InvoiceCadenceAdvance {
			remainingTime := subscription.CurrentPeriodEnd.Sub(effectiveDate)
			eligible := remainingTime > 0
			logger.Debugw("immediate cancellation eligibility check",
				"pay_in_advance", true,
				"remaining_time", remainingTime.String(),
				"eligible", eligible)
			return eligible
		}

		// For arrears billing, no refund needed (they pay for what they used)
		logger.Debug("arrears billing cancellation - no refund needed")
		return false

	default:
		logger.Warnw("unknown cancellation type", "type", cancellationType)
		return false
	}
}
