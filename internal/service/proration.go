package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// ProrationService handles subscription proration calculations
type ProrationService interface {
	// CalculateProration calculates the proration for changing a subscription line item
	// Returns ProrationResult containing credit and charge items
	// Can be used both for preview and actual application
	CalculateProration(ctx context.Context,
		subscription *subscription.Subscription,
		params *proration.ProrationParams) (*proration.ProrationResult, error)

	// ApplyProration applies the calculated proration to the subscription
	// This creates the appropriate invoices or credits based on the proration result
	ApplyProration(ctx context.Context,
		subscription *subscription.Subscription,
		prorationResult *proration.ProrationResult,
		prorationBehavior types.ProrationBehavior) error
}

// prorationService implements the ProrationService interface
type prorationService struct {
	ServiceParams
	BillingService BillingService
	InvoiceService InvoiceService
	PriceService   PriceService
}

// NewProrationService creates a new proration service
func NewProrationService(params ServiceParams) ProrationService {
	return &prorationService{
		ServiceParams:  params,
		BillingService: NewBillingService(params),
		InvoiceService: NewInvoiceService(params),
		PriceService:   NewPriceService(params.PriceRepo, params.MeterRepo, params.Logger),
	}
}

// CalculateProration calculates proration for subscription changes
func (s *prorationService) CalculateProration(
	ctx context.Context,
	sub *subscription.Subscription,
	params *proration.ProrationParams,
) (*proration.ProrationResult, error) {
	// Skip if behavior is none
	// Note this will always be none for usage based pricing
	if params.ProrationBehavior == types.ProrationBehaviorNone {
		return nil, nil
	}

	// Set default strategy if not specified
	if params.ProrationStrategy == "" {
		params.ProrationStrategy = types.ProrationStrategyDayBased
	}

	// Initialize result
	result := &proration.ProrationResult{
		Credits:       []proration.ProrationLineItem{},
		Charges:       []proration.ProrationLineItem{},
		Currency:      sub.Currency,
		Action:        params.Action,
		ProrationDate: params.ProrationDate,
		LineItemID:    params.LineItemID,
		IsPreview:     false,
	}

	// Validate inputs
	if err := s.validateProrationParams(ctx, sub, params); err != nil {
		return nil, err
	}

	// Handle different actions
	switch params.Action {
	case types.ProrationActionPlanChange, types.ProrationActionQuantityChange:
		if err := s.calculateChangePriceOrQuantity(ctx, sub, params, result); err != nil {
			return nil, err
		}

	case types.ProrationActionAddItem:
		if err := s.calculateAddItem(ctx, sub, params, result); err != nil {
			return nil, err
		}

	case types.ProrationActionRemoveItem:
		if err := s.calculateRemoveItem(ctx, sub, params, result); err != nil {
			return nil, err
		}

	case types.ProrationActionCancellation:
		if err := s.calculateCancellation(ctx, sub, params, result); err != nil {
			return nil, err
		}

	default:
		return nil, ierr.NewError("unsupported proration action").
			WithHint("The specified proration action is not supported").
			Mark(ierr.ErrValidation)
	}

	// Calculate net amount
	var netAmount decimal.Decimal
	for _, charge := range result.Charges {
		netAmount = netAmount.Add(charge.Amount)
	}

	for _, credit := range result.Credits {
		netAmount = netAmount.Sub(credit.Amount)
	}

	result.NetAmount = netAmount

	return result, nil
}

// calculateProportionalAmount calculates a prorated amount based
// on the strategy and invoice cadence
func (s *prorationService) calculateProportionalAmount(
	totalPeriodAmount decimal.Decimal,
	startDate, endDate, prorationDate time.Time,
	strategy types.ProrationStrategy,
	invoiceCadence types.InvoiceCadence,
) (decimal.Decimal, types.TransactionType) {
	var coefficient decimal.Decimal
	var transactionType types.TransactionType

	if invoiceCadence == types.InvoiceCadenceArrear {
		transactionType = types.TransactionTypeDebit
	} else if invoiceCadence == types.InvoiceCadenceAdvance {
		transactionType = types.TransactionTypeCredit
	}

	switch strategy {
	case types.ProrationStrategySecondBased:
		// Total period duration in seconds
		totalDuration := decimal.NewFromFloat(endDate.Sub(startDate).Seconds())

		// Remaining period duration in seconds
		remainingDuration := decimal.NewFromFloat(endDate.Sub(prorationDate).Seconds())

		// Used period duration in seconds
		usedDuration := decimal.NewFromFloat(prorationDate.Sub(startDate).Seconds())

		if invoiceCadence == types.InvoiceCadenceArrear {
			coefficient = remainingDuration.Div(totalDuration)
		} else if invoiceCadence == types.InvoiceCadenceAdvance {
			coefficient = usedDuration.Div(totalDuration)
		}
	case types.ProrationStrategyDayBased, "": // Default to day-based
		// Calculate days in the period
		totalDays := decimal.NewFromInt(int64(s.daysBetween(startDate, endDate)))

		// Calculate remaining days
		remainingDays := decimal.NewFromInt(int64(s.daysBetween(prorationDate, endDate)))

		// Used days
		usedDays := decimal.NewFromInt(int64(s.daysBetween(startDate, prorationDate)))

		if invoiceCadence == types.InvoiceCadenceArrear {
			coefficient = remainingDays.Div(totalDays)
		} else if invoiceCadence == types.InvoiceCadenceAdvance {
			coefficient = usedDays.Div(totalDays)
		}
	}

	// Apply coefficient to amount
	return totalPeriodAmount.Mul(coefficient).Round(2), transactionType
}

// daysBetween calculates the number of calendar days between two dates
func (s *prorationService) daysBetween(start, end time.Time) int {
	// Convert to same timezone to avoid timezone issues
	startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	endDate := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)

	// Calculate difference in days
	return int(endDate.Sub(startDate).Hours()/24) + 1 // +1 to include both start and end days
}

// validateProrationParams validates the proration parameters
func (s *prorationService) validateProrationParams(
	ctx context.Context,
	subscription *subscription.Subscription,
	params *proration.ProrationParams,
) error {
	// Check subscription is valid
	if subscription == nil {
		return ierr.NewError("subscription cannot be nil").
			WithHint("A valid subscription is required for proration calculations").
			Mark(ierr.ErrValidation)
	}

	// Validate line item exists (except for add_item)
	if params.Action != types.ProrationActionAddItem && params.LineItemID != "" {
		var foundLineItem bool
		for _, item := range subscription.LineItems {
			if item.ID == params.LineItemID {
				foundLineItem = true

				// Validate price type is fixed
				priceObj, err := s.PriceRepo.Get(ctx, item.PriceID)
				if err != nil {
					return err
				}

				if priceObj.Type != types.PRICE_TYPE_FIXED {
					return ierr.NewError("proration only supported for fixed prices").
						WithHint("Proration is only available for fixed price items").
						Mark(ierr.ErrValidation)
				}

				// Validate billing cadence is recurring
				if priceObj.BillingCadence != types.BILLING_CADENCE_RECURRING {
					return ierr.NewError("proration only supported for recurring billing cadence").
						WithHint("Proration is only available for recurring billing items").
						Mark(ierr.ErrValidation)
				}

				break
			}
		}

		if !foundLineItem {
			return ierr.NewError("line item not found").
				WithHintf("Line item %s not found in subscription %s", params.LineItemID, subscription.ID).
				Mark(ierr.ErrNotFound)
		}
	}

	// Action-specific validation
	switch params.Action {
	case types.ProrationActionPlanChange:
		if params.NewPriceID == nil {
			return ierr.NewError("new price ID is required for upgrades/downgrades").
				WithHint("A new price must be specified for upgrades and downgrades").
				Mark(ierr.ErrValidation)
		}

		// Validate new price if provided
		if params.NewPriceID != nil {
			newPrice, err := s.PriceRepo.Get(ctx, *params.NewPriceID)
			if err != nil {
				return err
			}

			if newPrice.Type != types.PRICE_TYPE_FIXED {
				return ierr.NewError("proration only supported for fixed prices").
					WithHint("Proration is only available for fixed price items").
					Mark(ierr.ErrValidation)
			}

			if newPrice.BillingCadence != types.BILLING_CADENCE_RECURRING {
				return ierr.NewError("proration only supported for recurring billing cadence").
					WithHint("Proration is only available for recurring billing items").
					Mark(ierr.ErrValidation)
			}
		}

	case types.ProrationActionQuantityChange:
		if params.OldQuantity == nil {
			return ierr.NewError("old quantity is required for quantity changes").
				WithHint("The current quantity must be specified for quantity changes").
				Mark(ierr.ErrValidation)
		}

	case types.ProrationActionAddItem:
		if params.NewPriceID == nil {
			return ierr.NewError("new price ID is required for add item").
				WithHint("A price must be specified when adding a new item").
				Mark(ierr.ErrValidation)
		}

		// Validate new price
		newPrice, err := s.PriceRepo.Get(ctx, *params.NewPriceID)
		if err != nil {
			return err
		}

		if newPrice.Type != types.PRICE_TYPE_FIXED {
			return ierr.NewError("proration only supported for fixed prices").
				WithHint("Proration is only available for fixed price items").
				Mark(ierr.ErrValidation)
		}

		if newPrice.BillingCadence != types.BILLING_CADENCE_RECURRING {
			return ierr.NewError("proration only supported for recurring billing cadence").
				WithHint("Proration is only available for recurring billing items").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// calculateChangePriceOrQuantity handles price changes and quantity changes
func (s *prorationService) calculateChangePriceOrQuantity(
	ctx context.Context,
	sub *subscription.Subscription,
	params *proration.ProrationParams,
	result *proration.ProrationResult,
) error {
	// Find the line item
	var lineItem *subscription.SubscriptionLineItem
	for _, item := range sub.LineItems {
		if item.ID == params.LineItemID {
			lineItem = item
			break
		}
	}

	if lineItem == nil {
		return ierr.NewError("line item not found").
			WithHintf("Line item %s not found in subscription %s", params.LineItemID, sub.ID).
			Mark(ierr.ErrNotFound)
	}

	// Get prices for calculation
	oldPriceID := lineItem.PriceID
	if params.OldPriceID != nil {
		oldPriceID = *params.OldPriceID
	}

	oldPrice, err := s.PriceRepo.Get(ctx, oldPriceID)
	if err != nil {
		return err
	}

	var newPrice *price.Price
	if params.NewPriceID != nil {
		newPrice, err = s.PriceRepo.Get(ctx, *params.NewPriceID)
		if err != nil {
			return err
		}
	} else {
		// For quantity change only, new price is the same as old price
		newPrice = oldPrice
	}

	// Get quantities
	oldQuantity := lineItem.Quantity
	if params.OldQuantity != nil {
		oldQuantity = *params.OldQuantity
	}

	newQuantity := oldQuantity
	if params.NewQuantity != nil {
		newQuantity = lo.FromPtr(params.NewQuantity)
	}

	// Calculate credit for unused portion
	oldAmount := s.PriceService.CalculateCost(ctx, oldPrice, oldQuantity)
	oldAmountProportional, oldAmountTransactionType := s.calculateProportionalAmount(
		oldAmount,
		sub.CurrentPeriodStart,
		sub.CurrentPeriodEnd,
		params.ProrationDate,
		params.ProrationStrategy,
		oldPrice.InvoiceCadence, // critical to highlight that this is the old price's invoice cadence
	)

	// Only add credit if there's an amount to credit
	if oldAmountProportional.GreaterThan(decimal.Zero) {
		oldAmountProportionalItem := proration.ProrationLineItem{
			Description:    fmt.Sprintf("Credit for unused portion of %s", oldPrice.ID),
			Amount:         oldAmountProportional,
			Type:           oldAmountTransactionType,
			PriceID:        oldPriceID,
			Quantity:       oldQuantity,
			UnitAmount:     oldPrice.Amount,
			PeriodStart:    params.ProrationDate,
			PeriodEnd:      sub.CurrentPeriodEnd,
			PlanChangeType: s.calculatePlanChangeType(ctx, oldPrice, newPrice),
		}

		if oldAmountTransactionType == types.TransactionTypeCredit {
			result.Credits = append(result.Credits, oldAmountProportionalItem)
		} else {
			result.Charges = append(result.Charges, oldAmountProportionalItem)
		}
	}

	// Calculate charge for new price/quantity for remainder of period
	newAmount := s.PriceService.CalculateCost(ctx, newPrice, newQuantity)
	newAmountProportional, newAmountTransactionType := s.calculateProportionalAmount(
		newAmount,
		sub.CurrentPeriodStart,
		sub.CurrentPeriodEnd,
		params.ProrationDate,
		params.ProrationStrategy,
		newPrice.InvoiceCadence, // critical to highlight that this is the new price's invoice cadence
	)

	// Only add charge if there's an amount to charge
	if newAmountProportional.GreaterThan(decimal.Zero) {
		newAmountProportionalItem := proration.ProrationLineItem{
			Description:    fmt.Sprintf("Adjustment for %s (prorated)", newPrice.ID),
			Amount:         newAmountProportional,
			Type:           newAmountTransactionType,
			PriceID:        newPrice.ID,
			Quantity:       newQuantity,
			UnitAmount:     newPrice.Amount,
			PeriodStart:    params.ProrationDate,
			PeriodEnd:      sub.CurrentPeriodEnd,
			PlanChangeType: s.calculatePlanChangeType(ctx, oldPrice, newPrice),
		}

		if newAmountTransactionType == types.TransactionTypeDebit {
			result.Charges = append(result.Charges, newAmountProportionalItem)
		} else {
			result.Credits = append(result.Credits, newAmountProportionalItem)
		}
	}
	return nil
}

// calculatePlanChangeType check if the plan change is an upgrade or downgrade
func (s *prorationService) calculatePlanChangeType(ctx context.Context, oldPrice *price.Price, newPrice *price.Price) types.PlanChangeType {
	planChangeType := types.PlanChangeTypeNoChange
	oldDailyPrice := s.PriceService.CalculateCost(ctx, oldPrice, decimal.NewFromInt(1)).
		Div(decimal.NewFromInt(int64(oldPrice.GetDaysInBillingInterval())))
	newDailyPrice := s.PriceService.CalculateCost(ctx, newPrice, decimal.NewFromInt(1)).
		Div(decimal.NewFromInt(int64(newPrice.GetDaysInBillingInterval())))

	if newDailyPrice.GreaterThan(oldDailyPrice) {
		planChangeType = types.PlanChangeTypeUpgrade
	} else if newDailyPrice.LessThan(oldDailyPrice) {
		planChangeType = types.PlanChangeTypeDowngrade
	} else {
		planChangeType = types.PlanChangeTypeNoChange
	}

	s.Logger.Debugw("calculated plan change type",
		"old_price_id", oldPrice.ID,
		"new_price_id", newPrice.ID,
		"old_daily_price", oldDailyPrice,
		"new_daily_price", newDailyPrice,
		"plan_change_type", planChangeType,
	)

	return planChangeType
}

// calculateAddItem handles adding a new line item to the subscription
func (s *prorationService) calculateAddItem(
	ctx context.Context,
	sub *subscription.Subscription,
	params *proration.ProrationParams,
	result *proration.ProrationResult,
) error {
	if params.NewPriceID == nil {
		return ierr.NewError("new price ID is required for add item").
			WithHint("A price must be specified when adding a new item").
			Mark(ierr.ErrValidation)
	}

	// Get the new price
	newPrice, err := s.PriceRepo.Get(ctx, *params.NewPriceID)
	if err != nil {
		return err
	}
	invoiceCadence := newPrice.InvoiceCadence

	// Calculate charge for the new item for remainder of period
	newAmount := s.PriceService.CalculateCost(ctx, newPrice, lo.FromPtr(params.NewQuantity))
	newAmountProportional, newAmountTransactionType := s.calculateProportionalAmount(
		newAmount,
		sub.CurrentPeriodStart,
		sub.CurrentPeriodEnd,
		params.ProrationDate,
		params.ProrationStrategy,
		invoiceCadence,
	)

	// For advance billing, charge for the remainder of the period
	if invoiceCadence == types.InvoiceCadenceAdvance {
		// Only add charge if there's an amount to charge
		if newAmountProportional.GreaterThan(decimal.Zero) {
			newAmountProportionalItem := proration.ProrationLineItem{
				Description: fmt.Sprintf("Adjustment for adding %s (prorated)", newPrice.ID),
				Amount:      newAmountProportional,
				Type:        newAmountTransactionType,
				PriceID:     newPrice.ID,
				Quantity:    lo.FromPtr((params.NewQuantity)),
				UnitAmount:  newPrice.Amount,
				PeriodStart: params.ProrationDate,
				PeriodEnd:   sub.CurrentPeriodEnd,
			}

			if newAmountTransactionType == types.TransactionTypeDebit {
				result.Charges = append(result.Charges, newAmountProportionalItem)
			} else {
				result.Credits = append(result.Credits, newAmountProportionalItem)
			}
		}
	} else {
		// For arrear billing, no charges are needed immediately
		// The new item will be billed at the end of the current period
		// But we can add a zero-amount placeholder for clarity in the result
		result.Charges = append(result.Charges, proration.ProrationLineItem{
			Description: fmt.Sprintf("New item %s added (will be billed at end of period)", newPrice.ID),
			Amount:      decimal.Zero,
			Type:        types.TransactionTypeDebit,
			PriceID:     newPrice.ID,
			Quantity:    lo.FromPtr(params.NewQuantity),
			UnitAmount:  newPrice.Amount,
			PeriodStart: params.ProrationDate,
			PeriodEnd:   sub.CurrentPeriodEnd,
		})
	}

	return nil
}

// calculateRemoveItem handles removing a line item from the subscription
func (s *prorationService) calculateRemoveItem(
	ctx context.Context,
	sub *subscription.Subscription,
	params *proration.ProrationParams,
	result *proration.ProrationResult,
) error {
	// Find the line item to remove
	var lineItem *subscription.SubscriptionLineItem
	for _, item := range sub.LineItems {
		if item.ID == params.LineItemID {
			lineItem = item
			break
		}
	}

	if lineItem == nil {
		return ierr.NewError("line item not found").
			WithHintf("Line item %s not found in subscription %s", params.LineItemID, sub.ID).
			Mark(ierr.ErrNotFound)
	}

	// Get price information
	priceObj, err := s.PriceRepo.Get(ctx, lineItem.PriceID)
	if err != nil {
		return err
	}

	amount, transactionType := s.calculateProportionalAmount(
		s.PriceService.CalculateCost(ctx, priceObj, lineItem.Quantity),
		sub.CurrentPeriodStart,
		sub.CurrentPeriodEnd,
		params.ProrationDate,
		params.ProrationStrategy,
		lineItem.InvoiceCadence,
	)

	if amount.GreaterThan(decimal.Zero) {
		prorationItem := proration.ProrationLineItem{
			Description: fmt.Sprintf("Adjustment for removed %s (prorated)", priceObj.ID),
			Amount:      amount,
			Type:        transactionType,
			PriceID:     priceObj.ID,
			Quantity:    lineItem.Quantity,
			UnitAmount:  priceObj.Amount,
			PeriodStart: params.ProrationDate,
			PeriodEnd:   sub.CurrentPeriodEnd,
		}

		if transactionType == types.TransactionTypeDebit {
			result.Charges = append(result.Charges, prorationItem)
		} else {
			result.Credits = append(result.Credits, prorationItem)
		}
	}
	return nil
}

// calculateCancellation handles subscription cancellation
func (s *prorationService) calculateCancellation(
	ctx context.Context,
	sub *subscription.Subscription,
	params *proration.ProrationParams,
	result *proration.ProrationResult,
) error {
	// For each line item, calculate appropriate adjustments based on invoice cadence
	for _, lineItem := range sub.LineItems {
		// Skip non-recurring items
		priceObj, err := s.PriceRepo.Get(ctx, lineItem.PriceID)
		if err != nil {
			return err
		}

		if priceObj.Type != types.PRICE_TYPE_FIXED ||
			priceObj.BillingCadence != types.BILLING_CADENCE_RECURRING {
			continue
		}

		amount, transactionType := s.calculateProportionalAmount(
			s.PriceService.CalculateCost(ctx, priceObj, lineItem.Quantity),
			sub.CurrentPeriodStart,
			sub.CurrentPeriodEnd,
			params.ProrationDate,
			params.ProrationStrategy,
			lineItem.InvoiceCadence,
		)

		if amount.GreaterThan(decimal.Zero) {
			prorationItem := proration.ProrationLineItem{
				Description: fmt.Sprintf("Charge for cancelled %s (prorated)", priceObj.ID),
				Amount:      amount,
				Type:        transactionType,
				PriceID:     priceObj.ID,
				Quantity:    lineItem.Quantity,
				UnitAmount:  priceObj.Amount,
				PeriodStart: params.ProrationDate,
				PeriodEnd:   sub.CurrentPeriodEnd,
			}

			if transactionType == types.TransactionTypeDebit {
				result.Charges = append(result.Charges, prorationItem)
			} else {
				result.Credits = append(result.Credits, prorationItem)
			}
		}
	}

	return nil
}

// ApplyProration applies the calculated proration to a subscription
func (s *prorationService) ApplyProration(
	ctx context.Context,
	subscription *subscription.Subscription,
	prorationResult *proration.ProrationResult,
	prorationBehavior types.ProrationBehavior,
) error {
	// Skip if behavior is none
	if prorationBehavior == types.ProrationBehaviorNone {
		return nil
	}

	// Get billing calculation result from proration result
	billingResult, err := s.BillingService.CalculateProrationCharges(ctx, subscription, prorationResult)
	if err != nil {
		return err
	}

	// Handle based on behavior
	switch prorationBehavior {
	case types.ProrationBehaviorAlwaysInvoice:
		// Create invoice immediately
		invoiceReq := dto.CreateInvoiceRequest{
			CustomerID:     subscription.CustomerID,
			SubscriptionID: lo.ToPtr(subscription.ID),
			InvoiceType:    types.InvoiceTypeSubscription,
			InvoiceStatus:  lo.ToPtr(types.InvoiceStatusDraft),
			Currency:       prorationResult.Currency,
			AmountDue:      prorationResult.NetAmount,
			Description:    fmt.Sprintf("Subscription update - %s", prorationResult.Action),
			BillingPeriod:  lo.ToPtr(string(subscription.BillingPeriod)),
			PeriodStart:    lo.ToPtr(subscription.CurrentPeriodStart),
			PeriodEnd:      lo.ToPtr(subscription.CurrentPeriodEnd),
			BillingReason:  types.InvoiceBillingReasonSubscriptionUpdate,
			EnvironmentID:  subscription.EnvironmentID,
			LineItems:      billingResult.FixedCharges,
		}

		invoice, err := s.InvoiceService.CreateInvoice(ctx, invoiceReq)
		if err != nil {
			return err
		}

		// Finalize the invoice
		if err := s.InvoiceService.FinalizeInvoice(ctx, invoice.ID); err != nil {
			return err
		}

		// If there's a negative amount, handle it as a credit
		if invoice.AmountDue.IsNegative() {
			// Add to customer credit balance instead of trying to charge
			// TODO: Implement credit management
			s.Logger.Infow("negative amount in proration invoice will be handled as a credit",
				"subscription_id", subscription.ID,
				"invoice_id", invoice.ID,
				"amount", invoice.AmountDue)
		} else if invoice.AmountDue.IsPositive() {
			// Attempt payment if there's a positive amount
			if err := s.InvoiceService.AttemptPayment(ctx, invoice.ID); err != nil {
				s.Logger.Warnw("failed to process proration payment",
					"subscription_id", subscription.ID,
					"invoice_id", invoice.ID,
					"error", err)
				// Don't return error here, the invoice is created and will be retried
			}
		}

	case types.ProrationBehaviorCreateProrations:
		// In the future, we'll implement storing pending items for the next invoice
		// For now, we'll just log that this behavior is not fully implemented
		s.Logger.Infow("create_prorations behavior not fully implemented yet",
			"subscription_id", subscription.ID,
			"proration_result", prorationResult)
		return ierr.NewError("create_prorations behavior not fully supported yet").
			WithHint("This behavior is not fully supported yet").
			WithReportableDetails(map[string]any{
				"subscription_id":  subscription.ID,
				"proration_result": prorationResult,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	return nil
}
