package service

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

// ApplyProration implements the logic to persist proration effects.
func (s *prorationService) ApplyProration(ctx context.Context,
	result *proration.ProrationResult,
	behavior types.ProrationBehavior,
	tenantID string,
	environmentID string,
	subscriptionID string,
	customerID string,
) error {
	if behavior == types.ProrationBehaviorNone || result == nil {
		// Nothing to apply for previews or empty results
		return nil
	}
	logger := s.serviceParams.Logger

	logger.Info("applying proration",
		zap.String("tenant_id", tenantID),
		zap.String("environment_id", environmentID),
		zap.String("subscription_id", subscriptionID),
		zap.String("behavior", string(behavior)),
	)

	if behavior == types.ProrationBehaviorCreateProrations {
		// Check if there's an existing invoice for the current period
		logger.Info("checking for existing invoice",
			zap.String("subscription_id", subscriptionID),
			zap.String("period_start", result.ProrationDate.Format(time.RFC3339)),
			zap.String("period_end", result.ProrationDate.Format(time.RFC3339)),
		)

		// Create or get invoice for the period
		var invoiceID string
		if result.CurrentPeriodStart.IsZero() {
			// If no period is specified, create a one-off invoice for the proration date
			invoiceResp, err := s.invoiceService.CreateInvoice(ctx, dto.CreateInvoiceRequest{
				CustomerID:     customerID,
				EnvironmentID:  environmentID,
				SubscriptionID: &subscriptionID,
				PeriodStart:    &result.ProrationDate,
				PeriodEnd:      &result.ProrationDate,
				InvoiceType:    types.InvoiceTypeSubscription,
				Currency:       result.Currency,
				AmountDue:      result.NetAmount,
				InvoiceStatus:  lo.ToPtr(types.InvoiceStatusDraft),
				PaymentStatus:  lo.ToPtr(types.PaymentStatusPending),
				DueDate:        lo.ToPtr(result.CurrentPeriodEnd.Add(24 * time.Hour)),
			})
			if err != nil {
				logger.Error("failed to create new invoice",
					zap.Error(err),
					zap.String("subscription_id", subscriptionID),
				)
				return ierr.NewErrorf("failed to create invoice: %v", err).
					WithHint("Check if all invoice details are valid").
					Mark(ierr.ErrSystem)
			}
			invoiceID = invoiceResp.ID
		} else {
			// Find invoice for the billing period
			invoices, err := s.invoiceService.ListInvoices(ctx, &types.InvoiceFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				TimeRangeFilter: &types.TimeRangeFilter{
					StartTime: &result.CurrentPeriodStart,
					EndTime:   &result.CurrentPeriodEnd,
				},
				SubscriptionID: subscriptionID,
				InvoiceStatus:  []types.InvoiceStatus{types.InvoiceStatusDraft},
			})
			if err != nil {
				logger.Error("failed to find existing invoice",
					zap.Error(err),
					zap.String("subscription_id", subscriptionID),
				)
				return ierr.NewErrorf("failed to find existing invoice: %v", err).
					WithHint("Verify the invoice filter parameters are correct").
					Mark(ierr.ErrDatabase)
			}

			if len(invoices.Items) == 0 {
				// Create new invoice for the period if none exists
				invoiceResp, err := s.invoiceService.CreateInvoice(ctx, dto.CreateInvoiceRequest{
					CustomerID:     customerID,
					EnvironmentID:  environmentID,
					SubscriptionID: &subscriptionID,
					PeriodStart:    &result.CurrentPeriodStart,
					PeriodEnd:      &result.CurrentPeriodEnd,
					InvoiceType:    types.InvoiceTypeSubscription,
					Currency:       result.Currency,
					AmountDue:      result.NetAmount,
					BillingPeriod:  lo.ToPtr(string(result.BillingPeriod)),
					InvoiceStatus:  lo.ToPtr(types.InvoiceStatusDraft),
					PaymentStatus:  lo.ToPtr(types.PaymentStatusPending),
					DueDate:        lo.ToPtr(result.CurrentPeriodEnd.Add(24 * time.Hour)),
				})
				if err != nil {
					logger.Error("failed to create new invoice",
						zap.Error(err),
						zap.String("subscription_id", subscriptionID),
					)
					return ierr.NewErrorf("failed to create invoice: %v", err).
						WithHint("Check if all invoice details are valid").
						Mark(ierr.ErrSystem)
				}
				invoiceID = invoiceResp.ID
			} else {
				invoiceID = invoices.Items[0].ID
			}
		}

		// Convert proration items to invoice line items
		var lineItemRequests []dto.CreateInvoiceLineItemRequest

		// Add credit items
		for _, credit := range result.CreditItems {
			lineItemRequests = append(lineItemRequests, dto.CreateInvoiceLineItemRequest{
				PriceID:     &credit.PriceID,
				DisplayName: &credit.Description,
				Amount:      credit.Amount,
				Quantity:    credit.Quantity,
				PeriodStart: &credit.StartDate,
				PeriodEnd:   &credit.EndDate,
			})
		}

		// Add charge items
		for _, charge := range result.ChargeItems {
			lineItemRequests = append(lineItemRequests, dto.CreateInvoiceLineItemRequest{
				PriceID:     &charge.PriceID,
				DisplayName: &charge.Description,
				Amount:      charge.Amount,
				Quantity:    charge.Quantity,
				PeriodStart: &charge.StartDate,
				PeriodEnd:   &charge.EndDate,
			})
		}

		// Add line items to invoice using invoice service
		if err := s.invoiceService.AddInvoiceLineItems(ctx, invoiceID, lineItemRequests); err != nil {
			logger.Error("failed to add proration line items to invoice",
				zap.Error(err),
				zap.String("invoice_id", invoiceID),
				zap.String("subscription_id", subscriptionID),
			)
			return ierr.NewErrorf("failed to add proration line items to invoice %s: %v", invoiceID, err).
				WithHint("Check if all line item details are valid").
				Mark(ierr.ErrDatabase)
		}

		logger.Info("successfully applied proration to invoice",
			zap.String("invoice_id", invoiceID),
			zap.String("subscription_id", subscriptionID),
			zap.Int("credit_items", len(result.CreditItems)),
			zap.Int("charge_items", len(result.ChargeItems)),
		)

		return nil
	}

	// Handle other behaviors if added in the future
	return ierr.NewErrorf("unsupported proration behavior: %s", behavior).
		WithHint("Only ProrationBehaviorCreateProrations is currently supported").
		Mark(ierr.ErrInvalidOperation)
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
func (s *prorationService) CalculateAndApplySubscriptionProration(
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
		"proration_mode", params.ProrationMode,
		"line_items_count", len(params.Subscription.LineItems))

	result := &proration.SubscriptionProrationResult{
		LineItemResults: make(map[string]*proration.ProrationResult),
		Currency:        params.Subscription.Currency,
	}

	// Only proceed if proration is needed
	if params.BillingCycle != types.BillingCycleCalendar ||
		params.ProrationMode != types.ProrationModeActive {
		logger.Infow("skipping proration - not needed",
			"subscription_id", params.Subscription.ID,
			"billing_cycle", params.BillingCycle,
			"proration_mode", params.ProrationMode)
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

		prorationParams := s.createProrationParamsForLineItem(
			params.Subscription,
			item,
			price,
			types.ProrationActionAddItem,
			types.ProrationBehaviorCreateProrations,
		)

		logger.Debugw("calculating proration for line item",
			"subscription_id", params.Subscription.ID,
			"line_item_id", item.ID,
			"price_id", item.PriceID,
			"action", prorationParams.Action)

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

	// Apply all prorations atomically
	err := s.serviceParams.DB.WithTx(ctx, func(ctx context.Context) error {
		for _, prorationResult := range result.LineItemResults {
			if err := s.ApplyProration(
				ctx,
				prorationResult,
				types.ProrationBehaviorCreateProrations,
				types.GetTenantID(ctx),
				types.GetEnvironmentID(ctx),
				params.Subscription.ID,
				params.Subscription.CustomerID,
			); err != nil {
				return ierr.NewErrorf("failed to apply proration: %v", err).
					WithHint("Check if all proration details are valid").
					Mark(ierr.ErrSystem)
			}
		}
		return nil
	})

	if err != nil {
		s.serviceParams.Logger.Errorw("failed to apply prorations",
			"error", err,
			"subscription_id", params.Subscription.ID)
		return nil, ierr.NewErrorf("failed to apply prorations: %v", err).
			WithHint("Verify all proration calculations are valid").
			Mark(ierr.ErrSystem)
	}

	s.serviceParams.Logger.Infow("successfully applied prorations",
		"subscription_id", params.Subscription.ID,
		"total_amount", result.TotalProrationAmount.String(),
		"line_items_processed", len(result.LineItemResults))

	return result, nil
}

// Helper method to create proration parameters for a line item
func (s *prorationService) createProrationParamsForLineItem(
	subscription *subscription.Subscription,
	item *subscription.SubscriptionLineItem,
	price *price.Price,
	action types.ProrationAction,
	behavior types.ProrationBehavior,
) proration.ProrationParams {
	periodStart, err := types.PreviousBillingDate(
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
		ProrationDate:         subscription.CurrentPeriodStart,
		ProrationBehavior:     behavior,
		CustomerTimezone:      subscription.CustomerTimezone,
		OriginalAmountPaid:    decimal.Zero,
		PreviousCreditsIssued: decimal.Zero,
		ProrationStrategy:     types.StrategyDayBased,
		Currency:              price.Currency,
		PlanDisplayName:       item.PlanDisplayName,
	}
}
