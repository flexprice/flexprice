package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"

	"github.com/shopspring/decimal"
)

// SubscriptionPaymentProcessor handles payment processing for subscriptions
type SubscriptionPaymentProcessor interface {
	HandlePaymentBehavior(ctx context.Context, subscription *subscription.Subscription, invoice *dto.InvoiceResponse, behavior types.PaymentBehavior, flowType types.InvoiceFlowType) error
	ProcessCreditsPaymentForInvoice(ctx context.Context, inv *dto.InvoiceResponse, sub *subscription.Subscription) decimal.Decimal
}

type subscriptionPaymentProcessor struct {
	*ServiceParams
}

// PaymentResult represents the result of a payment attempt
type PaymentResult struct {
	Success                    bool                `json:"success"`
	AmountPaid                 decimal.Decimal     `json:"amount_paid"`
	RemainingAmount            decimal.Decimal     `json:"remaining_amount"`
	PaymentMethods             []PaymentMethodUsed `json:"payment_methods_used"`
	RequiresManualConfirmation bool                `json:"requires_manual_confirmation"`
	Error                      error               `json:"error,omitempty"`
}

// PaymentMethodUsed represents a payment method that was used
type PaymentMethodUsed struct {
	Type   types.PaymentMethodType `json:"type"`
	ID     string                  `json:"id"`
	Amount decimal.Decimal         `json:"amount"`
	Status types.PaymentStatus     `json:"status"`
}

// NewSubscriptionPaymentProcessor creates a new subscription payment processor
func NewSubscriptionPaymentProcessor(params *ServiceParams) SubscriptionPaymentProcessor {
	return &subscriptionPaymentProcessor{
		ServiceParams: params,
	}
}

// HandlePaymentBehavior handles the payment result based on payment behavior
func (s *subscriptionPaymentProcessor) HandlePaymentBehavior(
	ctx context.Context,
	sub *subscription.Subscription,
	inv *dto.InvoiceResponse,
	behavior types.PaymentBehavior,
	flowType types.InvoiceFlowType,
) error {
	s.Logger.Infow("handling payment behavior",
		"subscription_id", sub.ID,
		"invoice_id", inv.ID,
		"amount_due", inv.AmountDue,
		"collection_method", sub.CollectionMethod,
		"payment_behavior", behavior,
	)

	// For manual flows, attempt payment and update subscription status based on result
	if flowType == types.InvoiceFlowManual {
		s.Logger.Infow("manual flow - attempting payment",
			"subscription_id", sub.ID,
			"invoice_id", inv.ID,
			"amount_due", inv.AmountDue,
		)

		result := s.processPayment(ctx, sub, inv, behavior, flowType)
		s.Logger.Infow("manual flow payment result",
			"subscription_id", sub.ID,
			"success", result.Success,
			"amount_paid", result.AmountPaid,
		)

		// If payment succeeded completely, mark subscription as active
		if result.Success {
			s.Logger.Infow("manual flow payment successful - activating subscription",
				"subscription_id", sub.ID,
				"amount_paid", result.AmountPaid,
			)
			sub.SubscriptionStatus = types.SubscriptionStatusActive
			return s.SubRepo.Update(ctx, sub)
		}

		// If payment failed or partial, keep subscription status unchanged
		s.Logger.Infow("manual flow payment failed or partial - keeping subscription status unchanged",
			"subscription_id", sub.ID,
			"current_status", sub.SubscriptionStatus,
			"amount_paid", result.AmountPaid,
		)
		return nil
	}

	// Handle different collection methods
	switch types.CollectionMethod(sub.CollectionMethod) {
	case types.CollectionMethodSendInvoice:
		return s.handleSendInvoiceMethod(ctx, sub, inv, behavior)
	case types.CollectionMethodChargeAutomatically:
		return s.handleChargeAutomaticallyMethod(ctx, sub, inv, behavior, flowType)
	default:
		return ierr.NewError("unsupported collection method").
			WithHint("Collection method not supported").
			WithReportableDetails(map[string]interface{}{
				"collection_method": sub.CollectionMethod,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
}

// handleSendInvoiceMethod handles send_invoice collection method
func (s *subscriptionPaymentProcessor) handleSendInvoiceMethod(
	ctx context.Context,
	sub *subscription.Subscription,
	inv *dto.InvoiceResponse,
	behavior types.PaymentBehavior,
) error {
	switch behavior {
	case types.PaymentBehaviorDefaultActive:
		// Default active behavior - always create active subscription without payment attempt
		s.Logger.Infow("send_invoice with default_active - activating subscription immediately",
			"subscription_id", sub.ID,
			"invoice_id", inv.ID,
			"amount_due", inv.AmountDue,
		)
		sub.SubscriptionStatus = types.SubscriptionStatusActive
		return s.SubRepo.Update(ctx, sub)

	case types.PaymentBehaviorDefaultIncomplete:
		// Default incomplete behavior - set subscription to incomplete without payment attempt
		s.Logger.Infow("send_invoice with default_incomplete - setting subscription to incomplete",
			"subscription_id", sub.ID,
			"invoice_id", inv.ID,
			"amount_due", inv.AmountDue,
		)
		sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
		return s.SubRepo.Update(ctx, sub)

	default:
		return ierr.NewError("unsupported payment behavior for send_invoice").
			WithHint("Only default_active and default_incomplete are supported for send_invoice collection method").
			WithReportableDetails(map[string]interface{}{
				"payment_behavior":  behavior,
				"collection_method": "send_invoice",
				"allowed_behaviors": []types.PaymentBehavior{
					types.PaymentBehaviorDefaultActive,
					types.PaymentBehaviorDefaultIncomplete,
				},
			}).
			Mark(ierr.ErrInvalidOperation)
	}
}

// handleChargeAutomaticallyMethod handles charge_automatically collection method
func (s *subscriptionPaymentProcessor) handleChargeAutomaticallyMethod(
	ctx context.Context,
	sub *subscription.Subscription,
	inv *dto.InvoiceResponse,
	behavior types.PaymentBehavior,
	flowType types.InvoiceFlowType,
) error {
	switch behavior {
	case types.PaymentBehaviorAllowIncomplete:
		return s.attemptPaymentAllowIncomplete(ctx, sub, inv, flowType)

	case types.PaymentBehaviorErrorIfIncomplete:
		return s.attemptPaymentErrorIfIncomplete(ctx, sub, inv, flowType)

	case types.PaymentBehaviorDefaultActive:
		return s.attemptPaymentDefaultActive(ctx, sub, inv, flowType)

	default:
		return ierr.NewError("unsupported payment behavior for charge_automatically").
			WithHint("Only allow_incomplete, error_if_incomplete, and default_active are supported for charge_automatically collection method").
			WithReportableDetails(map[string]interface{}{
				"payment_behavior":  behavior,
				"collection_method": "charge_automatically",
				"allowed_behaviors": []types.PaymentBehavior{
					types.PaymentBehaviorAllowIncomplete,
					types.PaymentBehaviorErrorIfIncomplete,
					types.PaymentBehaviorDefaultActive,
				},
			}).
			Mark(ierr.ErrInvalidOperation)
	}
}

// attemptPaymentAllowIncomplete attempts payment and allows incomplete status on failure
func (s *subscriptionPaymentProcessor) attemptPaymentAllowIncomplete(
	ctx context.Context,
	sub *subscription.Subscription,
	inv *dto.InvoiceResponse,
	flowType types.InvoiceFlowType,
) error {
	result := s.processPayment(ctx, sub, inv, types.PaymentBehaviorAllowIncomplete, flowType)

	// Get the latest subscription status to check if it was already activated
	// by payment reconciliation (this can happen when payment succeeds and
	// triggers subscription activation through payment service)
	latestSub, err := s.SubRepo.Get(ctx, sub.ID)
	if err != nil {
		s.Logger.Errorw("failed to get latest subscription status",
			"error", err,
			"subscription_id", sub.ID,
		)
		// Continue with original logic if we can't get latest status
		latestSub = sub
	}

	// Determine target status based on payment result
	var targetStatus types.SubscriptionStatus
	if result.Success {
		targetStatus = types.SubscriptionStatusActive
	} else {
		targetStatus = types.SubscriptionStatusIncomplete
	}

	s.Logger.Infow("allow_incomplete payment result",
		"subscription_id", sub.ID,
		"success", result.Success,
		"amount_paid", result.AmountPaid,
		"current_status", latestSub.SubscriptionStatus,
		"target_status", targetStatus,
	)

	// Only update if the subscription status needs to change
	if latestSub.SubscriptionStatus != targetStatus {
		latestSub.SubscriptionStatus = targetStatus
		err := s.SubRepo.Update(ctx, latestSub)
		if err != nil {
			return err
		}
		// Update the original subscription object for consistency
		sub.SubscriptionStatus = latestSub.SubscriptionStatus
		return nil
	}

	s.Logger.Infow("subscription status already matches target, skipping update",
		"subscription_id", sub.ID,
		"status", latestSub.SubscriptionStatus,
	)

	// Update the original subscription object for consistency
	sub.SubscriptionStatus = latestSub.SubscriptionStatus
	return nil
}

// attemptPaymentErrorIfIncomplete attempts payment and returns error on failure
func (s *subscriptionPaymentProcessor) attemptPaymentErrorIfIncomplete(
	ctx context.Context,
	sub *subscription.Subscription,
	inv *dto.InvoiceResponse,
	flowType types.InvoiceFlowType,
) error {
	result := s.processPayment(ctx, sub, inv, types.PaymentBehaviorErrorIfIncomplete, flowType)

	if result.Success {
		// Don't update subscription status here - let the payment processor handle it
		// This prevents version conflicts when both this method and payment processor try to update
		sub.SubscriptionStatus = types.SubscriptionStatusActive
		return nil
	}

	// Check the invoice flow type to determine error handling behavior
	// For subscription creation flow, return error to prevent subscription creation
	if flowType == types.InvoiceFlowSubscriptionCreation {
		return ierr.NewError("payment failed").
			WithHint("Subscription creation failed due to payment failure").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
				"invoice_id":      inv.ID,
				"amount_due":      inv.AmountDue,
				"amount_paid":     result.AmountPaid,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// For renewal flows, don't return error - let invoice remain in pending state
	s.Logger.Infow("payment failed for renewal flow, marking invoice as pending",
		"subscription_id", sub.ID,
		"invoice_id", inv.ID,
		"amount_due", inv.AmountDue,
		"amount_paid", result.AmountPaid,
		"flow_type", flowType)

	return nil
}

// attemptPaymentDefaultActive attempts payment and always marks subscription as active regardless of payment result
func (s *subscriptionPaymentProcessor) attemptPaymentDefaultActive(
	ctx context.Context,
	sub *subscription.Subscription,
	inv *dto.InvoiceResponse,
	flowType types.InvoiceFlowType,
) error {
	result := s.processPayment(ctx, sub, inv, types.PaymentBehaviorDefaultActive, flowType)

	// Get the latest subscription status to check if it was already activated
	// by payment reconciliation (this can happen when payment succeeds and
	// triggers subscription activation through payment service)
	latestSub, err := s.SubRepo.Get(ctx, sub.ID)
	if err != nil {
		s.Logger.Errorw("failed to get latest subscription status",
			"error", err,
			"subscription_id", sub.ID,
		)
		// Continue with original logic if we can't get latest status
		latestSub = sub
	}

	// For default_active behavior, always set to active regardless of payment result
	targetStatus := types.SubscriptionStatusActive

	s.Logger.Infow("default_active payment result",
		"subscription_id", sub.ID,
		"payment_success", result.Success,
		"amount_paid", result.AmountPaid,
		"current_status", latestSub.SubscriptionStatus,
		"target_status", targetStatus,
		"behavior", "always_active",
	)

	// Only update if the subscription status needs to change
	if latestSub.SubscriptionStatus != targetStatus {
		latestSub.SubscriptionStatus = targetStatus
		return s.SubRepo.Update(ctx, latestSub)
	}

	s.Logger.Infow("subscription status already matches target, skipping update",
		"subscription_id", sub.ID,
		"status", latestSub.SubscriptionStatus,
	)

	// Update the original subscription object for consistency
	sub.SubscriptionStatus = latestSub.SubscriptionStatus
	return nil
}

// processPayment processes payment with card-first logic
// This prioritizes card payments over wallet payments as per new requirements
func (s *subscriptionPaymentProcessor) processPayment(
	ctx context.Context,
	sub *subscription.Subscription,
	inv *dto.InvoiceResponse,
	behavior types.PaymentBehavior,
	flowType types.InvoiceFlowType,
) *PaymentResult {
	// Use AmountRemaining instead of AmountDue to account for any existing payments
	remainingAmount := inv.AmountRemaining

	result := &PaymentResult{
		AmountPaid:      decimal.Zero,
		RemainingAmount: remainingAmount,
		PaymentMethods:  []PaymentMethodUsed{},
	}

	s.Logger.Infow("processing payment with card-first logic",
		"subscription_id", sub.ID,
		"amount_due", inv.AmountDue,
		"amount_remaining", remainingAmount,
	)

	if remainingAmount.IsZero() {
		result.Success = true
		return result
	}

	// Step 1: Get the full invoice with line items to analyze price types
	fullInvoice, err := s.InvoiceRepo.Get(ctx, inv.ID)
	if err != nil {
		s.Logger.Errorw("failed to get invoice with line items for payment analysis",
			"error", err,
			"invoice_id", inv.ID)
		result.Success = false
		return result
	}

	// Step 2: Calculate price type breakdown
	walletPaymentService := &walletPaymentService{ServiceParams: *s.ServiceParams}
	priceTypeAmounts := walletPaymentService.calculatePriceTypeAmounts(fullInvoice.LineItems)
	s.Logger.Infow("calculated price type breakdown for payment split",
		"subscription_id", sub.ID,
		"invoice_id", inv.ID,
		"price_type_amounts", priceTypeAmounts)

	// Step 3: Check available credits and determine what they can pay for
	// Use invoicing customer ID for wallet operations - if invoice is for invoicing customer,
	// use invoicing customer's wallets; otherwise use subscription customer's wallets
	invoicingCustomerID := sub.GetInvoicingCustomerID()
	availableCredits := s.checkAvailableCredits(ctx, sub, inv)
	walletPayableAmount := s.calculateWalletPayableAmount(ctx, invoicingCustomerID, priceTypeAmounts, availableCredits)

	s.Logger.Infow("wallet payment analysis",
		"subscription_id", sub.ID,
		"available_credits", availableCredits,
		"wallet_payable_amount", walletPayableAmount,
		"invoice_amount", remainingAmount)

	// Step 4: Determine payment split based on price type restrictions
	var cardAmount, walletAmount decimal.Decimal

	cardAmount = remainingAmount.Sub(walletPayableAmount)
	walletAmount = walletPayableAmount

	if walletAmount.IsZero() {
		s.Logger.Infow("wallet cannot pay any amount due to price type restrictions, paying entirely with card",
			"subscription_id", sub.ID,
			"card_amount", cardAmount)
	} else if cardAmount.IsZero() {
		s.Logger.Infow("wallet can pay entire amount, paying entirely with wallet",
			"subscription_id", sub.ID,
			"wallet_amount", walletAmount)
	} else {
		s.Logger.Infow("splitting payment between card and wallet based on price types",
			"subscription_id", sub.ID,
			"card_amount", cardAmount,
			"wallet_amount", walletAmount)
	}

	// Step 3: Process card payment first (if needed)
	if cardAmount.GreaterThan(decimal.Zero) {
		s.Logger.Infow("attempting card payment",
			"subscription_id", sub.ID,
			"card_amount", cardAmount,
		)

		cardAmountPaid := s.processPaymentMethodCharge(ctx, sub, inv, cardAmount)
		if cardAmountPaid.GreaterThan(decimal.Zero) {
			result.AmountPaid = result.AmountPaid.Add(cardAmountPaid)
			result.RemainingAmount = result.RemainingAmount.Sub(cardAmountPaid)
			result.PaymentMethods = append(result.PaymentMethods, PaymentMethodUsed{
				Type:   "card",
				Amount: cardAmountPaid,
			})

			s.Logger.Infow("card payment successful",
				"subscription_id", sub.ID,
				"card_amount_paid", cardAmountPaid,
				"remaining_amount", result.RemainingAmount,
			)
		} else {
			// Card payment failed - check if we should allow partial wallet payment
			allowPartialWallet := s.shouldAllowPartialWalletPayment(
				behavior,
				flowType,
			)

			// If invoice is synced to Stripe, don't allow partial payments
			stripeIntegration, err := s.IntegrationFactory.GetStripeIntegration(ctx)
			if err == nil {
				if stripeIntegration.InvoiceSyncSvc.IsInvoiceSyncedToStripe(ctx, inv.ID) {
					s.Logger.Warnw("card payment failed, invoice is synced to Stripe - not allowing partial wallet payment",
						"subscription_id", sub.ID,
						"invoice_id", inv.ID,
						"attempted_card_amount", cardAmount,
						"wallet_amount_available", walletAmount,
					)
					allowPartialWallet = false
				}
			}

			if !allowPartialWallet {
				// Card payment failed - do not attempt wallet payment
				// The invoice cannot be fully paid, so we stop here
				s.Logger.Warnw("card payment failed, not attempting wallet payment",
					"subscription_id", sub.ID,
					"attempted_card_amount", cardAmount,
					"wallet_amount_available", walletAmount,
					"collection_method", sub.CollectionMethod,
					"behavior", behavior,
					"flow_type", flowType,
				)

				result.Success = false
				return result
			} else {
				// Card payment failed but we allow partial wallet payment
				s.Logger.Warnw("card payment failed, but allowing partial wallet payment",
					"subscription_id", sub.ID,
					"attempted_card_amount", cardAmount,
					"wallet_amount_available", walletAmount,
					"collection_method", sub.CollectionMethod,
					"behavior", behavior,
					"flow_type", flowType,
				)
			}
		}
	}

	// Step 4: Process wallet payment (only if card payment succeeded or not needed)
	if walletAmount.GreaterThan(decimal.Zero) {
		s.Logger.Infow("attempting wallet payment",
			"subscription_id", sub.ID,
			"wallet_amount", walletAmount,
		)

		creditsUsed := s.processCreditsPayment(ctx, sub, inv)
		if creditsUsed.GreaterThan(decimal.Zero) {
			result.AmountPaid = result.AmountPaid.Add(creditsUsed)
			result.RemainingAmount = result.RemainingAmount.Sub(creditsUsed)
			result.PaymentMethods = append(result.PaymentMethods, PaymentMethodUsed{
				Type:   "credits",
				Amount: creditsUsed,
			})

			s.Logger.Infow("wallet payment successful",
				"subscription_id", sub.ID,
				"credits_used", creditsUsed,
				"remaining_amount", result.RemainingAmount,
			)
		} else {
			s.Logger.Warnw("wallet payment failed",
				"subscription_id", sub.ID,
				"attempted_wallet_amount", walletAmount,
			)
		}
	}

	// Step 5: Determine final success
	result.Success = result.RemainingAmount.IsZero()

	s.Logger.Infow("payment processing completed",
		"subscription_id", sub.ID,
		"success", result.Success,
		"total_paid", result.AmountPaid,
		"remaining_amount", result.RemainingAmount,
		"payment_methods", len(result.PaymentMethods),
	)

	return result
}

// processCreditsPayment processes payment using customer's credits/wallets
func (s *subscriptionPaymentProcessor) processCreditsPayment(
	ctx context.Context,
	sub *subscription.Subscription,
	inv *dto.InvoiceResponse,
) decimal.Decimal {
	subscriptionID := ""
	if sub != nil {
		subscriptionID = sub.ID
	}
	s.Logger.Infow("processing credits payment",
		"subscription_id", subscriptionID,
		"invoice_id", inv.ID,
		"amount_due", inv.AmountDue,
	)

	// Get the full invoice with line items from the repository
	fullInvoice, err := s.InvoiceRepo.Get(ctx, inv.ID)
	if err != nil {
		s.Logger.Errorw("failed to get invoice with line items for wallet payment",
			"error", err,
			"invoice_id", inv.ID)
		return decimal.Zero
	}

	// Use the full domain invoice with line items for wallet payment service
	domainInvoice := fullInvoice

	// Use wallet payment service to process payment
	walletPaymentService := NewWalletPaymentService(*s.ServiceParams)
	metadata := types.Metadata{
		"payment_source": "subscription_auto_payment",
	}
	if sub != nil {
		metadata["subscription_id"] = sub.ID
	}

	amountPaid, err := walletPaymentService.ProcessInvoicePaymentWithWallets(ctx, domainInvoice, WalletPaymentOptions{
		Strategy:           PromotionalFirstStrategy,
		MaxWalletsToUse:    5,
		AdditionalMetadata: metadata,
	})

	if err != nil {
		s.Logger.Errorw("credits payment failed",
			"error", err,
			"subscription_id", subscriptionID,
			"invoice_id", inv.ID,
		)
		return decimal.Zero
	}

	s.Logger.Infow("credits payment completed",
		"subscription_id", subscriptionID,
		"invoice_id", inv.ID,
		"amount_paid", amountPaid,
	)

	return amountPaid
}

// ProcessCreditsPaymentForInvoice is a public wrapper for processCreditsPayment to be used by other services
func (s *subscriptionPaymentProcessor) ProcessCreditsPaymentForInvoice(
	ctx context.Context,
	inv *dto.InvoiceResponse,
	sub *subscription.Subscription,
) decimal.Decimal {
	return s.processCreditsPayment(ctx, sub, inv)
}

// calculateWalletPayableAmount determines how much wallets can pay based on price type restrictions
func (s *subscriptionPaymentProcessor) calculateWalletPayableAmount(
	ctx context.Context,
	customerID string,
	priceTypeAmounts map[string]decimal.Decimal,
	availableCredits decimal.Decimal,
) decimal.Decimal {
	if availableCredits.IsZero() {
		return decimal.Zero
	}

	// Get customer's wallets to check their configurations
	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		s.Logger.Errorw("failed to get wallets for payment analysis",
			"error", err,
			"customer_id", customerID)
		return decimal.Zero
	}

	// Filter active wallets with balance
	activeWallets := make([]*wallet.Wallet, 0)
	for _, w := range wallets {
		if w.WalletStatus == types.WalletStatusActive && w.Balance.GreaterThan(decimal.Zero) {
			activeWallets = append(activeWallets, w)
		}
	}
	wallets = activeWallets

	if len(wallets) == 0 {
		return decimal.Zero
	}

	// Calculate total payable amount across all wallets
	totalPayableAmount := decimal.Zero
	remainingCredits := availableCredits

	for _, wallet := range wallets {
		if remainingCredits.IsZero() {
			break
		}

		walletPayableAmount := s.calculateWalletAllowedAmount(wallet, priceTypeAmounts)
		actualPayableAmount := decimal.Min(walletPayableAmount, wallet.Balance)
		actualPayableAmount = decimal.Min(actualPayableAmount, remainingCredits)

		totalPayableAmount = totalPayableAmount.Add(actualPayableAmount)
		remainingCredits = remainingCredits.Sub(actualPayableAmount)

		s.Logger.Debugw("wallet payment analysis",
			"wallet_id", wallet.ID,
			"wallet_config", wallet.Config,
			"wallet_balance", wallet.Balance,
			"wallet_payable_amount", walletPayableAmount,
			"actual_payable_amount", actualPayableAmount)
	}

	return totalPayableAmount
}

// calculateWalletAllowedAmount calculates how much a single wallet can pay based on its price type restrictions
func (s *subscriptionPaymentProcessor) calculateWalletAllowedAmount(
	wallet *wallet.Wallet,
	priceTypeAmounts map[string]decimal.Decimal,
) decimal.Decimal {
	// If wallet has no allowed price types, use default (ALL)
	if len(wallet.Config.AllowedPriceTypes) == 0 {
		// Can pay for everything
		totalAmount := decimal.Zero
		for _, amount := range priceTypeAmounts {
			totalAmount = totalAmount.Add(amount)
		}
		return totalAmount
	}

	allowedAmount := decimal.Zero

	for _, allowedType := range wallet.Config.AllowedPriceTypes {
		allowedTypeStr := string(allowedType)
		if allowedTypeStr == "ALL" {
			// Can pay for everything
			for _, amount := range priceTypeAmounts {
				allowedAmount = allowedAmount.Add(amount)
			}
			break
		} else if amount, exists := priceTypeAmounts[allowedTypeStr]; exists {
			allowedAmount = allowedAmount.Add(amount)
		}
	}

	return allowedAmount
}

// processPaymentMethodCharge processes payment using payment method (card, etc.)
func (s *subscriptionPaymentProcessor) processPaymentMethodCharge(
	ctx context.Context,
	sub *subscription.Subscription,
	inv *dto.InvoiceResponse,
	amount decimal.Decimal,
) decimal.Decimal {
	s.Logger.Infow("processing payment method charge",
		"subscription_id", sub.ID,
		"invoice_id", inv.ID,
		"amount", amount,
	)

	// Check if tenant has Stripe connection
	if !s.hasStripeConnection(ctx) {
		s.Logger.Warnw("no Stripe connection available for payment method charge",
			"subscription_id", sub.ID,
		)
		return decimal.Zero
	}

	// Get Stripe integration
	stripeIntegration, err := s.IntegrationFactory.GetStripeIntegration(ctx)
	if err != nil {
		s.Logger.Warnw("failed to get Stripe integration",
			"subscription_id", sub.ID,
			"error", err,
		)
		return decimal.Zero
	}

	// Check if customer has Stripe entity mapping
	// Use invoicing customer ID for Stripe operations - payment should use invoicing customer's payment methods
	invoicingCustomerID := sub.GetInvoicingCustomerID()
	customerService := NewCustomerService(*s.ServiceParams)
	if !stripeIntegration.CustomerSvc.HasCustomerStripeMapping(ctx, invoicingCustomerID, customerService) {
		s.Logger.Warnw("no Stripe entity mapping found for invoicing customer",
			"subscription_id", sub.ID,
			"subscription_customer_id", sub.CustomerID,
			"invoicing_customer_id", invoicingCustomerID,
		)
		return decimal.Zero
	}

	// Get payment method ID - use invoicing customer's payment methods
	paymentMethodID := s.getPaymentMethodID(ctx, sub, invoicingCustomerID)
	if paymentMethodID == "" {
		s.Logger.Warnw("no payment method available for automatic charging",
			"subscription_id", sub.ID,
		)
		return decimal.Zero
	}

	// Create payment record for card payment
	paymentService := NewPaymentService(*s.ServiceParams)
	paymentReq := &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     inv.ID,
		PaymentMethodType: types.PaymentMethodTypeCard,
		PaymentMethodID:   paymentMethodID,
		Amount:            amount,
		Currency:          inv.Currency,
		ProcessPayment:    true,
		Metadata: types.Metadata{
			"customer_id":              sub.GetInvoicingCustomerID(), // Use invoicing customer ID for payment
			"subscription_customer_id": sub.CustomerID,               // Include subscription customer ID for reference
			"subscription_id":          sub.ID,
			"payment_source":           "subscription_auto_payment",
		},
	}

	paymentResp, err := paymentService.CreatePayment(ctx, paymentReq)
	if err != nil {
		s.Logger.Errorw("failed to create payment record for card charge",
			"error", err,
			"subscription_id", sub.ID,
			"subscription_customer_id", sub.CustomerID,
			"invoicing_customer_id", invoicingCustomerID,
			"payment_method_id", paymentMethodID,
			"amount", amount,
		)
		return decimal.Zero
	}

	s.Logger.Infow("created payment record for card charge",
		"subscription_id", sub.ID,
		"payment_id", paymentResp.ID,
		"amount", amount,
	)

	// Check if payment was successful
	if paymentResp.PaymentStatus == types.PaymentStatusSucceeded {
		s.Logger.Infow("payment method charge successful",
			"subscription_id", sub.ID,
			"payment_id", paymentResp.ID,
			"amount", amount,
		)
		return amount
	}

	s.Logger.Warnw("payment method charge not successful",
		"subscription_id", sub.ID,
		"payment_id", paymentResp.ID,
		"status", paymentResp.PaymentStatus,
	)
	return decimal.Zero
}

// getPaymentMethodID gets the payment method ID for the subscription
// Uses invoicing customer ID for payment method lookup - payment should use invoicing customer's payment methods
func (s *subscriptionPaymentProcessor) getPaymentMethodID(ctx context.Context, sub *subscription.Subscription, invoicingCustomerID string) string {
	// Use subscription's payment method if set
	if sub.GatewayPaymentMethodID != nil && *sub.GatewayPaymentMethodID != "" {
		s.Logger.Infow("using subscription gateway payment method",
			"subscription_id", sub.ID,
			"gateway_payment_method_id", *sub.GatewayPaymentMethodID,
		)
		return *sub.GatewayPaymentMethodID
	}

	// Get invoicing customer's default payment method from Stripe
	stripeIntegration, err := s.IntegrationFactory.GetStripeIntegration(ctx)
	if err != nil {
		s.Logger.Warnw("failed to get Stripe integration",
			"error", err,
			"subscription_id", sub.ID,
		)
		return ""
	}

	customerService := NewCustomerService(*s.ServiceParams)
	defaultPaymentMethod, err := stripeIntegration.CustomerSvc.GetDefaultPaymentMethod(ctx, invoicingCustomerID, customerService)
	if err != nil {
		s.Logger.Warnw("failed to get default payment method for invoicing customer",
			"error", err,
			"subscription_id", sub.ID,
			"subscription_customer_id", sub.CustomerID,
			"invoicing_customer_id", invoicingCustomerID,
		)
		return ""
	}

	if defaultPaymentMethod == nil {
		s.Logger.Warnw("invoicing customer has no default payment method",
			"subscription_id", sub.ID,
			"subscription_customer_id", sub.CustomerID,
			"invoicing_customer_id", invoicingCustomerID,
		)
		return ""
	}

	s.Logger.Infow("using invoicing customer default payment method",
		"subscription_id", sub.ID,
		"subscription_customer_id", sub.CustomerID,
		"invoicing_customer_id", invoicingCustomerID,
		"payment_method_id", defaultPaymentMethod.ID,
	)

	return defaultPaymentMethod.ID
}

// hasStripeConnection checks if the tenant has a Stripe connection available
func (s *subscriptionPaymentProcessor) hasStripeConnection(ctx context.Context) bool {
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		s.Logger.Debugw("no Stripe connection found",
			"error", err,
		)
		return false
	}

	if conn == nil {
		s.Logger.Debugw("Stripe connection is nil")
		return false
	}

	s.Logger.Debugw("Stripe connection found",
		"connection_id", conn.ID,
		"provider", conn.ProviderType,
	)

	return true
}

// shouldAllowPartialWalletPayment determines if partial wallet payment should be allowed
// when card payment fails based on behavior and flow type
func (s *subscriptionPaymentProcessor) shouldAllowPartialWalletPayment(
	behavior types.PaymentBehavior,
	flowType types.InvoiceFlowType,
) bool {
	switch flowType {
	case types.InvoiceFlowSubscriptionCreation:
		// For subscription_creation flow, only allow if behavior is default_active
		return behavior == types.PaymentBehaviorDefaultActive
	case types.InvoiceFlowRenewal, types.InvoiceFlowManual, types.InvoiceFlowCancel:
		// For renewal, manual, or cancel flows, always allow partial wallet payment
		return true
	default:
		// Default: don't allow partial wallet payment
		return false
	}
}

// checkAvailableCredits checks how much credits are available without consuming them
func (s *subscriptionPaymentProcessor) checkAvailableCredits(
	ctx context.Context,
	sub *subscription.Subscription,
	inv *dto.InvoiceResponse,
) decimal.Decimal {
	s.Logger.Infow("checking available credits",
		"subscription_id", sub.ID,
		"invoice_id", inv.ID,
	)

	// Use invoicing customer ID for wallet operations - if invoice is for invoicing customer,
	// use invoicing customer's wallets; otherwise use subscription customer's wallets
	invoicingCustomerID := sub.GetInvoicingCustomerID()
	currency := inv.Currency

	// Get wallets suitable for payment
	walletPaymentService := NewWalletPaymentService(*s.ServiceParams)
	wallets, err := walletPaymentService.GetWalletsForPayment(ctx, invoicingCustomerID, currency, WalletPaymentOptions{
		Strategy:        PromotionalFirstStrategy,
		MaxWalletsToUse: 5,
	})
	if err != nil {
		s.Logger.Errorw("failed to get wallets for payment",
			"error", err,
			"subscription_customer_id", sub.CustomerID,
			"invoicing_customer_id", invoicingCustomerID,
			"currency", currency,
		)
		return decimal.Zero
	}

	// Calculate total available credits
	totalAvailable := decimal.Zero
	for _, w := range wallets {
		totalAvailable = totalAvailable.Add(w.Balance)
		s.Logger.Debugw("wallet balance",
			"wallet_id", w.ID,
			"wallet_type", w.WalletType,
			"balance", w.Balance,
		)
	}

	s.Logger.Infow("total available credits calculated",
		"subscription_id", sub.ID,
		"subscription_customer_id", sub.CustomerID,
		"invoicing_customer_id", invoicingCustomerID,
		"currency", currency,
		"total_available", totalAvailable,
		"wallets_count", len(wallets),
	)

	return totalAvailable
}
