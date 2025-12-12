package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"

	"github.com/shopspring/decimal"
)

// SubscriptionPaymentProcessor handles payment processing for subscriptions
type SubscriptionPaymentProcessor interface {
	HandlePaymentBehavior(ctx context.Context, subscription *subscription.Subscription, invoice *dto.InvoiceResponse, behavior types.PaymentBehavior, flowType types.InvoiceFlowType) error
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

	// Process card payment for the full remaining amount
	if remainingAmount.GreaterThan(decimal.Zero) {
		s.Logger.Infow("attempting card payment",
			"subscription_id", sub.ID,
			"card_amount", remainingAmount,
		)

		cardAmountPaid := s.processPaymentMethodCharge(ctx, sub, inv, remainingAmount)
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
			// Card payment failed
			s.Logger.Warnw("card payment failed",
				"subscription_id", sub.ID,
				"attempted_card_amount", remainingAmount,
			)
			result.Success = false
			return result
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
