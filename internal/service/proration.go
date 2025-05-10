package service

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

type prorationService struct {
	calculator  proration.Calculator
	invoiceRepo invoice.Repository
	logger      *logger.Logger
}

// NewProrationService creates a new proration service.
func NewProrationService(
	calculator proration.Calculator,
	invoiceRepo invoice.Repository,
	logger *logger.Logger,
) proration.Service {
	return &prorationService{
		calculator:  calculator,
		invoiceRepo: invoiceRepo,
		logger:      logger,
	}
}

// CalculateProration delegates to the underlying calculator.
func (s *prorationService) CalculateProration(ctx context.Context, params proration.ProrationParams) (*proration.ProrationResult, error) {
	s.logger.Debug("calculating proration",
		zap.String("subscription_id", params.SubscriptionID),
		zap.String("line_item_id", params.LineItemID),
		zap.String("action", string(params.Action)),
	)

	result, err := s.calculator.Calculate(ctx, params)
	if err != nil {
		s.logger.Error("proration calculation failed",
			zap.Error(err),
			zap.String("subscription_id", params.SubscriptionID),
			zap.String("line_item_id", params.LineItemID),
		)
		return nil, fmt.Errorf("proration calculation failed: %w", err)
	}

	s.logger.Debug("proration calculation completed",
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
) error {
	if behavior == types.ProrationBehaviorNone || result == nil {
		// Nothing to apply for previews or empty results
		return nil
	}

	s.logger.Debug("applying proration",
		zap.String("tenant_id", tenantID),
		zap.String("environment_id", environmentID),
		zap.String("subscription_id", subscriptionID),
		zap.String("behavior", string(behavior)),
	)

	if behavior == types.ProrationBehaviorCreateProrations {
		// Check if there's an existing invoice for the current period
		exists, err := s.invoiceRepo.ExistsForPeriod(ctx, subscriptionID, result.ProrationDate, result.ProrationDate)
		if err != nil {
			s.logger.Error("failed to check for existing invoice",
				zap.Error(err),
				zap.String("subscription_id", subscriptionID),
			)
			return fmt.Errorf("failed to check for existing invoice: %w", err)
		}

		var inv *invoice.Invoice
		if !exists {
			// Create a new invoice
			nextNumber, err := s.invoiceRepo.GetNextInvoiceNumber(ctx)
			if err != nil {
				s.logger.Error("failed to get next invoice number",
					zap.Error(err),
					zap.String("subscription_id", subscriptionID),
				)
				return fmt.Errorf("failed to get next invoice number: %w", err)
			}

			nextSeq, err := s.invoiceRepo.GetNextBillingSequence(ctx, subscriptionID)
			if err != nil {
				s.logger.Error("failed to get next billing sequence",
					zap.Error(err),
					zap.String("subscription_id", subscriptionID),
				)
				return fmt.Errorf("failed to get next billing sequence: %w", err)
			}

			inv = &invoice.Invoice{
				SubscriptionID:  &subscriptionID,
				InvoiceType:     types.InvoiceTypeSubscription,
				InvoiceStatus:   types.InvoiceStatusDraft,
				PaymentStatus:   types.PaymentStatusPending,
				Currency:        result.Currency,
				InvoiceNumber:   &nextNumber,
				BillingSequence: &nextSeq,
				Description:     "Proration Invoice",
				BillingReason:   string(types.InvoiceBillingReasonSubscriptionUpdate),
				PeriodStart:     &result.ProrationDate,
				PeriodEnd:       &result.ProrationDate,
				EnvironmentID:   environmentID,
				BaseModel: types.BaseModel{
					TenantID: tenantID,
					Status:   types.StatusPublished,
				},
			}

			if err := s.invoiceRepo.Create(ctx, inv); err != nil {
				s.logger.Error("failed to create invoice for proration",
					zap.Error(err),
					zap.String("subscription_id", subscriptionID),
				)
				return fmt.Errorf("failed to create invoice for proration: %w", err)
			}
		} else {
			// Find the existing invoice
			filter := &types.InvoiceFilter{
				QueryFilter:    types.NewDefaultQueryFilter(),
				SubscriptionID: subscriptionID,
				TimeRangeFilter: &types.TimeRangeFilter{
					StartTime: &result.ProrationDate,
					EndTime:   &result.ProrationDate,
				},
			}

			invoices, err := s.invoiceRepo.List(ctx, filter)
			if err != nil {
				s.logger.Error("failed to find existing invoice",
					zap.Error(err),
					zap.String("subscription_id", subscriptionID),
				)
				return fmt.Errorf("failed to find existing invoice: %w", err)
			}
			if len(invoices) == 0 {
				return fmt.Errorf("no active invoice found for period")
			}
			inv = invoices[0]
		}

		// Convert proration items to invoice line items
		var lineItems []*invoice.InvoiceLineItem

		// Add credit items
		for _, credit := range result.CreditItems {
			lineItems = append(lineItems, &invoice.InvoiceLineItem{
				InvoiceID:      inv.ID,
				SubscriptionID: &subscriptionID,
				PriceID:        &credit.PriceID,
				DisplayName:    &credit.Description,
				Amount:         credit.Amount,
				Quantity:       credit.Quantity,
				Currency:       result.Currency,
				PeriodStart:    &credit.StartDate,
				PeriodEnd:      &credit.EndDate,
				EnvironmentID:  environmentID,
				BaseModel: types.BaseModel{
					TenantID: tenantID,
					Status:   types.StatusPublished,
				},
			})
		}

		// Add charge items
		for _, charge := range result.ChargeItems {
			lineItems = append(lineItems, &invoice.InvoiceLineItem{
				InvoiceID:      inv.ID,
				SubscriptionID: &subscriptionID,
				PriceID:        &charge.PriceID,
				DisplayName:    &charge.Description,
				Amount:         charge.Amount,
				Quantity:       charge.Quantity,
				Currency:       result.Currency,
				PeriodStart:    &charge.StartDate,
				PeriodEnd:      &charge.EndDate,
				EnvironmentID:  environmentID,
				BaseModel: types.BaseModel{
					TenantID: tenantID,
					Status:   types.StatusPublished,
				},
			})
		}

		// Add line items to invoice
		if err := s.invoiceRepo.AddLineItems(ctx, inv.ID, lineItems); err != nil {
			s.logger.Error("failed to add proration line items to invoice",
				zap.Error(err),
				zap.String("invoice_id", inv.ID),
				zap.String("subscription_id", subscriptionID),
			)
			return fmt.Errorf("failed to add proration line items to invoice %s: %w", inv.ID, err)
		}

		s.logger.Debug("successfully applied proration to invoice",
			zap.String("invoice_id", inv.ID),
			zap.String("subscription_id", subscriptionID),
			zap.Int("credit_items", len(result.CreditItems)),
			zap.Int("charge_items", len(result.ChargeItems)),
		)

		return nil
	}

	// Handle other behaviors if added in the future
	return fmt.Errorf("unsupported proration behavior: %s", behavior)
}

// validateSubscriptionProrationParams validates the parameters for subscription proration calculation
func (s *prorationService) validateSubscriptionProrationParams(params proration.SubscriptionProrationParams) error {
	if params.Subscription == nil {
		return fmt.Errorf("subscription is required")
	}
	if params.Subscription.ID == "" {
		return fmt.Errorf("subscription ID is required")
	}
	if params.Subscription.StartDate.IsZero() {
		return fmt.Errorf("subscription start date is required")
	}
	if params.Subscription.BillingAnchor.IsZero() {
		return fmt.Errorf("subscription billing anchor is required")
	}
	if len(params.Subscription.LineItems) == 0 {
		return fmt.Errorf("subscription must have at least one line item")
	}
	if params.Prices == nil {
		return fmt.Errorf("prices map is required")
	}

	// Validate each line item has a corresponding price
	for _, item := range params.Subscription.LineItems {
		if item.ID == "" {
			return fmt.Errorf("line item ID is required")
		}
		if item.PriceID == "" {
			return fmt.Errorf("price ID is required for line item %s", item.ID)
		}
		if _, exists := params.Prices[item.PriceID]; !exists {
			return fmt.Errorf("price not found for line item %s with price ID %s", item.ID, item.PriceID)
		}
		if item.Quantity.IsZero() || item.Quantity.IsNegative() {
			return fmt.Errorf("quantity must be positive for line item %s", item.ID)
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
		return nil, fmt.Errorf("invalid subscription proration parameters: %w", err)
	}

	result := &proration.SubscriptionProrationResult{
		LineItemResults: make(map[string]*proration.ProrationResult),
	}

	// Only proceed if proration is needed
	if params.BillingCycle != types.BillingCycleCalendar ||
		params.ProrationMode != types.ProrationModeActive {
		return result, nil
	}

	// Calculate proration for each line item
	for _, item := range params.Subscription.LineItems {
		price, ok := params.Prices[item.PriceID]
		if !ok {
			s.logger.Debug("price not found for line item",
				zap.String("priceID", item.PriceID))
			continue
		}

		prorationParams := proration.ProrationParams{
			SubscriptionID:     params.Subscription.ID,
			LineItemID:         item.ID,
			PlanPayInAdvance:   price.InvoiceCadence == types.InvoiceCadenceAdvance,
			CurrentPeriodStart: params.Subscription.StartDate,
			CurrentPeriodEnd:   params.Subscription.BillingAnchor,
			Action:             types.ProrationActionAddItem,
			NewPriceID:         item.PriceID,
			NewQuantity:        item.Quantity,
			NewPricePerUnit:    price.Amount,
			ProrationDate:      params.Subscription.StartDate,
			ProrationBehavior:  types.ProrationBehaviorCreateProrations,
			CustomerTimezone:   params.Subscription.CustomerTimezone,
		}

		prorationResult, err := s.CalculateProration(ctx, prorationParams)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate proration for line item %s: %w", item.ID, err)
		}

		result.LineItemResults[item.ID] = prorationResult
		result.TotalProrationAmount = result.TotalProrationAmount.Add(prorationResult.NetAmount)
	}

	// Apply all prorations atomically
	for _, prorationResult := range result.LineItemResults {
		if err := s.ApplyProration(
			ctx,
			prorationResult,
			types.ProrationBehaviorCreateProrations,
			types.GetTenantID(ctx),
			types.GetEnvironmentID(ctx),
			params.Subscription.ID,
		); err != nil {
			return nil, fmt.Errorf("failed to apply proration: %w", err)
		}
	}

	return result, nil
}
