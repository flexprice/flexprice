package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/razorpay"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type PaymentProcessorService interface {
	ProcessPayment(ctx context.Context, id string) (*payment.Payment, error)
}

type paymentProcessor struct {
	ServiceParams
}

func NewPaymentProcessorService(params ServiceParams) PaymentProcessorService {
	return &paymentProcessor{
		ServiceParams: params,
	}
}

func (p *paymentProcessor) ProcessPayment(ctx context.Context, id string) (*payment.Payment, error) {
	paymentObj, err := p.PaymentRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// For payment links, accept both INITIATED and PENDING statuses
	// For other payment methods, only accept PENDING status
	if paymentObj.PaymentMethodType == types.PaymentMethodTypePaymentLink {
		if paymentObj.PaymentStatus != types.PaymentStatusInitiated && paymentObj.PaymentStatus != types.PaymentStatusPending {
			return paymentObj, ierr.NewError("payment is not in initiated or pending state").
				WithHint("Invalid payment status for processing payment link").
				WithReportableDetails(map[string]interface{}{
					"payment_id": paymentObj.ID,
					"status":     paymentObj.PaymentStatus,
				}).
				Mark(ierr.ErrInvalidOperation)
		}
	} else {
		// If payment is not in pending state, return error
		if paymentObj.PaymentStatus != types.PaymentStatusPending {
			return paymentObj, ierr.NewError("payment is not in pending state").
				WithHint("Invalid payment status for processing payment").
				WithReportableDetails(map[string]interface{}{
					"payment_id": paymentObj.ID,
					"status":     paymentObj.PaymentStatus,
				}).
				Mark(ierr.ErrInvalidOperation)
		}
	}

	// Create a new payment attempt if tracking is enabled
	var attempt *payment.PaymentAttempt
	if paymentObj.TrackAttempts {
		attempt, err = p.createNewAttempt(ctx, paymentObj)
		if err != nil {
			return paymentObj, err
		}
	}

	// For payment links, if status is INITIATED, we need to create the payment link
	// If status is already PENDING, we don't need to do anything more
	if paymentObj.PaymentMethodType == types.PaymentMethodTypePaymentLink && paymentObj.PaymentStatus == types.PaymentStatusInitiated {
		// Update payment status to processing temporarily
		paymentObj.PaymentStatus = types.PaymentStatusProcessing
		paymentObj.UpdatedAt = time.Now().UTC()
		if err := p.PaymentRepo.Update(ctx, paymentObj); err != nil {
			return paymentObj, err
		}
		p.publishWebhookEvent(ctx, types.WebhookEventPaymentPending, paymentObj.ID)
	} else if paymentObj.PaymentMethodType != types.PaymentMethodTypePaymentLink {
		// Update payment status to processing and fire pending event
		// TODO: take a lock on the payment object here to avoid race conditions
		paymentObj.PaymentStatus = types.PaymentStatusProcessing
		paymentObj.UpdatedAt = time.Now().UTC()
		if err := p.PaymentRepo.Update(ctx, paymentObj); err != nil {
			return paymentObj, err
		}
		p.publishWebhookEvent(ctx, types.WebhookEventPaymentPending, paymentObj.ID)
	}

	// Process payment based on payment method type
	var processErr error
	switch paymentObj.PaymentMethodType {
	case types.PaymentMethodTypeOffline:
		// For offline payments, we just mark them as succeeded immediately
		processErr = nil
	case types.PaymentMethodTypeCredits:
		processErr = p.handleCreditsPayment(ctx, paymentObj)
	case types.PaymentMethodTypePaymentLink:
		// For payment links, create the actual payment link
		// If status is already PENDING, skip creation
		if paymentObj.PaymentStatus == types.PaymentStatusPending {
			processErr = nil // Already processed
		} else {
			processErr = p.handlePaymentLinkCreation(ctx, paymentObj)
		}
	case types.PaymentMethodTypeCard:
		// Handle card payment processing
		processErr = p.handleCardPayment(ctx, paymentObj)
	case types.PaymentMethodTypeACH:
		// TODO: Implement ACH payment processing
		processErr = ierr.NewError("ACH payment processing not implemented").
			WithHint("ACH payment processing is not supported yet").
			WithReportableDetails(map[string]interface{}{
				"payment_id": paymentObj.ID,
			}).
			Mark(ierr.ErrInvalidOperation)
	default:
		processErr = ierr.NewError(fmt.Sprintf("unsupported payment method type: %s", paymentObj.PaymentMethodType)).
			WithHint("Unsupported payment method type").
			WithReportableDetails(map[string]interface{}{
				"payment_id": paymentObj.ID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Update attempt status if tracking is enabled
	if paymentObj.TrackAttempts && attempt != nil {
		if processErr != nil {
			// For payment links, keep attempt as pending even on error
			if paymentObj.PaymentMethodType == types.PaymentMethodTypePaymentLink {
				attempt.PaymentStatus = types.PaymentStatusPending
				attempt.ErrorMessage = lo.ToPtr(processErr.Error())
			} else {
				attempt.PaymentStatus = types.PaymentStatusFailed
				attempt.ErrorMessage = lo.ToPtr(processErr.Error())
			}
		} else {
			// For payment links, keep attempt as pending
			if paymentObj.PaymentMethodType == types.PaymentMethodTypePaymentLink {
				attempt.PaymentStatus = types.PaymentStatusPending
			} else {
				attempt.PaymentStatus = types.PaymentStatusSucceeded
			}
		}
		attempt.UpdatedAt = time.Now().UTC()
		if err := p.PaymentRepo.UpdateAttempt(ctx, attempt); err != nil {
			p.Logger.Errorw("failed to update payment attempt", "error", err)
		}
	}

	// Update payment status based on processing result
	if processErr != nil {
		// For payment links, if the error occurred during payment link creation,
		// keep the status as INITIATED instead of marking as FAILED
		if paymentObj.PaymentMethodType == types.PaymentMethodTypePaymentLink {
			// Keep status as INITIATED when Stripe SDK fails for payment links
			p.Logger.Infow("keeping payment link as initiated due to Stripe SDK failure",
				"payment_id", paymentObj.ID,
				"status", paymentObj.PaymentStatus,
				"error", processErr.Error())
			// Reset status back to INITIATED if it was changed to PROCESSING
			if paymentObj.PaymentStatus == types.PaymentStatusProcessing {
				paymentObj.PaymentStatus = types.PaymentStatusInitiated
			}
			// Don't set failed_at or error_message for payment links
		} else {
			// For other cases, mark as failed
			paymentObj.PaymentStatus = types.PaymentStatusFailed
			errMsg := processErr.Error()
			paymentObj.ErrorMessage = lo.ToPtr(errMsg)
			failedAt := time.Now().UTC()
			paymentObj.FailedAt = &failedAt
			p.publishWebhookEvent(ctx, types.WebhookEventPaymentFailed, paymentObj.ID)
		}
	} else {
		// For payment links, keep as pending until external payment completion
		if paymentObj.PaymentMethodType == types.PaymentMethodTypePaymentLink {
			paymentObj.PaymentStatus = types.PaymentStatusPending
			paymentObj.SucceededAt = nil // Keep succeeded_at as nil
			p.Logger.Infow("keeping payment link as pending", "payment_id", paymentObj.ID, "status", paymentObj.PaymentStatus)
			p.publishWebhookEvent(ctx, types.WebhookEventPaymentPending, paymentObj.ID)
		} else {
			paymentObj.PaymentStatus = types.PaymentStatusSucceeded
			succeededAt := time.Now().UTC()
			paymentObj.SucceededAt = &succeededAt
			p.Logger.Infow("marking payment as succeeded", "payment_id", paymentObj.ID, "status", paymentObj.PaymentStatus)
			p.publishWebhookEvent(ctx, types.WebhookEventPaymentSuccess, paymentObj.ID)
		}
	}

	paymentObj.UpdatedAt = time.Now().UTC()
	if err := p.PaymentRepo.Update(ctx, paymentObj); err != nil {
		return paymentObj, err
	}

	// If payment succeeded, handle post-processing
	if paymentObj.PaymentStatus == types.PaymentStatusSucceeded {
		if err := p.handlePostProcessing(ctx, paymentObj); err != nil {
			p.Logger.Errorw("failed to handle post-processing", "error", err, "payment_id", paymentObj.ID)
			// Note: We don't return this error as the payment itself was successful
		}
	}

	if processErr != nil {
		return paymentObj, processErr
	}

	return paymentObj, nil
}

func (p *paymentProcessor) handlePaymentLinkCreation(ctx context.Context, paymentObj *payment.Payment) error {
	// Get the invoice to get customer information
	invoice, err := p.InvoiceRepo.Get(ctx, paymentObj.DestinationID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get invoice for payment link creation").
			WithReportableDetails(map[string]interface{}{
				"payment_id": paymentObj.ID,
				"invoice_id": paymentObj.DestinationID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Determine which gateway to use based on payment_gateway field
	gatewayType := types.PaymentGatewayTypeStripe // Default to Stripe for backward compatibility
	if paymentObj.PaymentGateway != nil && *paymentObj.PaymentGateway != "" {
		gatewayType = types.PaymentGatewayType(*paymentObj.PaymentGateway)
	}

	p.Logger.Infow("creating payment link",
		"payment_id", paymentObj.ID,
		"gateway_type", gatewayType)

	// Route to appropriate gateway
	switch gatewayType {
	case types.PaymentGatewayTypeStripe:
		return p.handleStripePaymentLinkCreation(ctx, paymentObj, invoice)
	case types.PaymentGatewayTypeRazorpay:
		return p.handleRazorpayPaymentLinkCreation(ctx, paymentObj, invoice)
	default:
		return ierr.NewError("unsupported payment gateway").
			WithHint("Payment gateway not supported for payment links").
			WithReportableDetails(map[string]interface{}{
				"payment_gateway": gatewayType,
				"payment_id":      paymentObj.ID,
			}).
			Mark(ierr.ErrValidation)
	}
}

func (p *paymentProcessor) handleStripePaymentLinkCreation(ctx context.Context, paymentObj *payment.Payment, invoice *invoice.Invoice) error {
	// Extract success URL and cancel URL from gateway metadata
	successURL := ""
	cancelURL := ""
	if paymentObj.GatewayMetadata != nil {
		if url, exists := paymentObj.GatewayMetadata["success_url"]; exists {
			successURL = url
		}
		if url, exists := paymentObj.GatewayMetadata["cancel_url"]; exists {
			cancelURL = url
		}
	}

	// Prepare metadata for payment link request
	linkMetadata := types.Metadata{}
	if paymentObj.Metadata != nil {
		// Copy metadata but exclude connection-related fields (internal use only)
		for k, v := range paymentObj.Metadata {
			if k != "connection_id" && k != "connection_name" {
				linkMetadata[k] = v
			}
		}
	}

	// Add FlexPrice payment ID to metadata for new payment_intent.succeeded webhook
	linkMetadata["flexprice_payment_id"] = paymentObj.ID

	// Convert to Stripe payment link request
	paymentLinkReq := &dto.CreateStripePaymentLinkRequest{
		InvoiceID:  paymentObj.DestinationID,
		CustomerID: invoice.CustomerID,
		Amount:     paymentObj.Amount,
		Currency:   paymentObj.Currency,
		SuccessURL: successURL,
		CancelURL:  cancelURL,
		SaveCardAndMakeDefault: func() bool {
			if paymentObj.GatewayMetadata != nil {
				if saveCardStr, exists := paymentObj.GatewayMetadata["save_card_and_make_default"]; exists {
					return saveCardStr == "true"
				}
			}
			return false
		}(),
		Metadata:      linkMetadata,
		PaymentID:     paymentObj.ID,
		EnvironmentID: types.GetEnvironmentID(ctx),
	}

	// Get Stripe integration for creating payment link
	stripeIntegration, err := p.IntegrationFactory.GetStripeIntegration(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get Stripe integration").
			Mark(ierr.ErrSystem)
	}

	customerService := NewCustomerService(p.ServiceParams)
	invoiceService := NewInvoiceService(p.ServiceParams)

	paymentLinkResp, err := stripeIntegration.PaymentSvc.CreatePaymentLink(ctx, paymentLinkReq, customerService, invoiceService)
	if err != nil {
		// If Stripe SDK fails, keep payment status as INITIATED and return error
		p.Logger.Errorw("failed to create payment link via Stripe SDK",
			"error", err,
			"payment_id", paymentObj.ID,
			"invoice_id", paymentObj.DestinationID)
		return err
	}

	// If Stripe SDK succeeds, update payment status to PENDING
	paymentObj.PaymentStatus = types.PaymentStatusPending

	// Update payment with gateway information
	paymentObj.GatewayTrackingID = &paymentLinkResp.ID // Store session_id in gateway_tracking_id
	paymentObj.GatewayPaymentID = &paymentLinkResp.PaymentIntentID
	if paymentObj.GatewayMetadata == nil {
		paymentObj.GatewayMetadata = types.Metadata{}
	}
	// Merge with existing gateway metadata (preserving save_card_and_make_default if set)
	paymentObj.GatewayMetadata["payment_url"] = paymentLinkResp.PaymentURL
	paymentObj.GatewayMetadata["gateway"] = string(types.PaymentGatewayTypeStripe)
	paymentObj.GatewayMetadata["session_id"] = paymentLinkResp.ID

	// Update the payment record
	if err := p.PaymentRepo.Update(ctx, paymentObj); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update payment with payment link information").
			WithReportableDetails(map[string]interface{}{
				"payment_id": paymentObj.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	p.Logger.Infow("successfully created stripe payment link and updated status to pending",
		"payment_id", paymentObj.ID,
		"session_id", paymentLinkResp.ID,
		"payment_url", paymentLinkResp.PaymentURL)

	return nil
}

func (p *paymentProcessor) handleRazorpayPaymentLinkCreation(ctx context.Context, paymentObj *payment.Payment, invoice *invoice.Invoice) error {
	// Extract success URL and cancel URL from metadata
	successURL := ""
	cancelURL := ""

	if paymentObj.Metadata != nil {
		if url, exists := paymentObj.Metadata["success_url"]; exists {
			successURL = url
		}
		if url, exists := paymentObj.Metadata["cancel_url"]; exists {
			cancelURL = url
		}
	}

	// Prepare metadata for payment link request
	linkMetadata := make(map[string]string)
	if paymentObj.Metadata != nil {
		// Copy metadata but exclude connection-related fields (internal use only)
		for k, v := range paymentObj.Metadata {
			if k != "connection_id" && k != "connection_name" {
				linkMetadata[k] = v
			}
		}
	}

	// Add FlexPrice payment ID to metadata
	linkMetadata["flexprice_payment_id"] = paymentObj.ID

	// Convert to Razorpay payment link request
	paymentLinkReq := &razorpay.CreatePaymentLinkRequest{
		InvoiceID:     paymentObj.DestinationID,
		CustomerID:    invoice.CustomerID,
		Amount:        paymentObj.Amount,
		Currency:      paymentObj.Currency,
		SuccessURL:    successURL,
		CancelURL:     cancelURL,
		Metadata:      linkMetadata,
		PaymentID:     paymentObj.ID,
		EnvironmentID: types.GetEnvironmentID(ctx),
	}

	// Get Razorpay integration for creating payment link
	razorpayIntegration, err := p.IntegrationFactory.GetRazorpayIntegration(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get Razorpay integration").
			Mark(ierr.ErrSystem)
	}

	customerService := NewCustomerService(p.ServiceParams)
	invoiceService := NewInvoiceService(p.ServiceParams)

	paymentLinkResp, err := razorpayIntegration.PaymentSvc.CreatePaymentLink(ctx, paymentLinkReq, customerService, invoiceService)
	if err != nil {
		// If Razorpay SDK fails, keep payment status as INITIATED and return error
		p.Logger.Errorw("failed to create payment link via Razorpay SDK",
			"error", err,
			"payment_id", paymentObj.ID,
			"invoice_id", paymentObj.DestinationID)
		return err
	}

	// If Razorpay SDK succeeds, update payment status to PENDING
	paymentObj.PaymentStatus = types.PaymentStatusPending

	// Update payment with gateway information
	paymentObj.GatewayTrackingID = &paymentLinkResp.ID // Store payment_link_id in gateway_tracking_id
	if paymentObj.GatewayMetadata == nil {
		paymentObj.GatewayMetadata = types.Metadata{}
	}
	// Merge with existing gateway metadata
	paymentObj.GatewayMetadata["payment_url"] = paymentLinkResp.PaymentURL
	paymentObj.GatewayMetadata["gateway"] = string(types.PaymentGatewayTypeRazorpay)
	paymentObj.GatewayMetadata["payment_link_id"] = paymentLinkResp.ID

	// Update the payment record
	if err := p.PaymentRepo.Update(ctx, paymentObj); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update payment with payment link information").
			WithReportableDetails(map[string]interface{}{
				"payment_id": paymentObj.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	p.Logger.Infow("successfully created razorpay payment link and updated status to pending",
		"payment_id", paymentObj.ID,
		"payment_link_id", paymentLinkResp.ID,
		"payment_url", paymentLinkResp.PaymentURL)

	return nil
}

func (p *paymentProcessor) handleCreditsPayment(ctx context.Context, paymentObj *payment.Payment) error {
	// In case of credits, we need to find the wallet by the set payment method id
	selectedWallet, err := p.WalletRepo.GetWalletByID(ctx, paymentObj.PaymentMethodID)
	if err != nil {
		return err
	}

	// Validate wallet status
	if selectedWallet.WalletStatus != types.WalletStatusActive {
		return ierr.NewError("wallet is not active").
			WithHint("Wallet is not active").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": selectedWallet.ID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Validate currency match
	if !types.IsMatchingCurrency(selectedWallet.Currency, paymentObj.Currency) {
		return ierr.NewError("wallet currency does not match payment currency").
			WithHint("Wallet currency does not match payment currency").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": selectedWallet.ID,
				"currency":  paymentObj.Currency,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Validate sufficient balance
	if selectedWallet.Balance.LessThan(paymentObj.Amount) {
		return ierr.NewError("wallet balance is less than payment amount").
			WithHint("Wallet balance is less than payment amount").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": selectedWallet.ID,
				"amount":    paymentObj.Amount,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Create wallet operation
	operation := &wallet.WalletOperation{
		WalletID:          selectedWallet.ID,
		Type:              types.TransactionTypeDebit,
		Amount:            paymentObj.Amount,
		ReferenceType:     types.WalletTxReferenceTypePayment,
		ReferenceID:       paymentObj.ID,
		Description:       fmt.Sprintf("Payment for invoice %s", paymentObj.DestinationID),
		TransactionReason: types.TransactionReasonInvoicePayment,
		Metadata: types.Metadata{
			"payment_id":     paymentObj.ID,
			"invoice_id":     paymentObj.DestinationID,
			"payment_method": string(paymentObj.PaymentMethodType),
			"wallet_type":    string(selectedWallet.WalletType),
		},
	}

	// Create wallet service
	walletService := NewWalletService(p.ServiceParams)

	// Transactional workflow begins here
	err = p.DB.WithTx(ctx, func(ctx context.Context) error {
		// Debit wallet
		if err := walletService.DebitWallet(ctx, operation); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

func (p *paymentProcessor) handlePostProcessing(ctx context.Context, paymentObj *payment.Payment) error {
	switch paymentObj.DestinationType {
	case types.PaymentDestinationTypeInvoice:
		return p.handleInvoicePostProcessing(ctx, paymentObj)
	default:
		return ierr.NewError("unsupported payment destination type").
			WithHint("Unsupported payment destination type").
			WithReportableDetails(map[string]interface{}{
				"payment_id":       paymentObj.ID,
				"destination_type": paymentObj.DestinationType,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
}

func (p *paymentProcessor) handleInvoicePostProcessing(ctx context.Context, paymentObj *payment.Payment) error {
	// Get the invoice
	invoice, err := p.InvoiceRepo.Get(ctx, paymentObj.DestinationID)
	if err != nil {
		return err
	}

	// Get all successful payments for this invoice
	filter := &types.PaymentFilter{
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
		DestinationID:   lo.ToPtr(invoice.ID),
		PaymentStatus:   lo.ToPtr(string(types.PaymentStatusSucceeded)),
	}
	payments, err := p.PaymentRepo.List(ctx, filter)
	if err != nil {
		return err
	}

	// Calculate total paid amount
	totalPaid := decimal.Zero
	for _, p := range payments {
		if p.Currency != invoice.Currency {
			return ierr.NewError("payment currency does not match invoice currency").
				WithHint("Payment currency does not match invoice currency").
				WithReportableDetails(map[string]interface{}{
					"payment_id":       paymentObj.ID,
					"invoice_id":       invoice.ID,
					"payment_currency": p.Currency,
					"invoice_currency": invoice.Currency,
				}).
				Mark(ierr.ErrInvalidOperation)
		}
		totalPaid = totalPaid.Add(p.Amount)
	}

	// Update invoice payment status and amounts
	invoice.AmountPaid = totalPaid
	invoice.AmountRemaining = invoice.AmountDue.Sub(totalPaid)
	if invoice.AmountRemaining.IsZero() {
		paidAt := time.Now().UTC()
		invoice.PaidAt = &paidAt
	}

	// Ensure amount remaining is not less than zero even in case of excess payments.
	// TODO: think of a better way to handle Partial payments
	if invoice.AmountRemaining.LessThan(decimal.Zero) {
		invoice.AmountRemaining = decimal.Zero
	}

	if invoice.AmountRemaining.IsZero() {
		invoice.PaymentStatus = types.PaymentStatusSucceeded
		// Finalize invoice if it's still in draft state
		if invoice.InvoiceStatus == types.InvoiceStatusDraft {
			invoice.InvoiceStatus = types.InvoiceStatusFinalized
			finalizedAt := time.Now().UTC()
			invoice.FinalizedAt = &finalizedAt
		}
	} else if invoice.AmountRemaining.LessThan(invoice.AmountDue) {
		invoice.PaymentStatus = types.PaymentStatusPending // Partial payment still keeps it pending
	}

	// Update the invoice
	if err := p.InvoiceRepo.Update(ctx, invoice); err != nil {
		return err
	}

	// Check if this invoice is for a purchased credit (has wallet_transaction_id in metadata)
	// If so, complete the wallet transaction to credit the wallet
	if invoice.Metadata != nil {
		if walletTransactionID, ok := invoice.Metadata["wallet_transaction_id"]; ok && walletTransactionID != "" {
			// Only complete the transaction if payment is fully succeeded
			if invoice.PaymentStatus == types.PaymentStatusSucceeded {
				walletService := NewWalletService(p.ServiceParams)
				if err := walletService.CompletePurchasedCreditTransactionWithRetry(ctx, walletTransactionID); err != nil {
					p.Logger.Errorw("failed to complete purchased credit transaction",
						"error", err,
						"invoice_id", invoice.ID,
						"wallet_transaction_id", walletTransactionID,
					)
					// Don't fail the payment, but log the error
					// The transaction can be manually completed later
				} else {
					p.Logger.Infow("successfully completed purchased credit transaction",
						"invoice_id", invoice.ID,
						"wallet_transaction_id", walletTransactionID,
					)
				}
			}
		}
	}

	// Check if this is the first invoice payment for an incomplete subscription
	if err := p.handleIncompleteSubscriptionPayment(ctx, invoice); err != nil {
		p.Logger.Errorw("failed to handle incomplete subscription payment",
			"invoice_id", invoice.ID,
			"error", err)
	}

	return nil
}

func (p *paymentProcessor) createNewAttempt(ctx context.Context, paymentObj *payment.Payment) (*payment.PaymentAttempt, error) {
	// Get latest attempt to determine attempt number
	latestAttempt, err := p.PaymentRepo.GetLatestAttempt(ctx, paymentObj.ID)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}

	attemptNumber := 1
	if latestAttempt != nil {
		attemptNumber = latestAttempt.AttemptNumber + 1
	}

	attempt := &payment.PaymentAttempt{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PAYMENT_ATTEMPT),
		PaymentID:     paymentObj.ID,
		AttemptNumber: attemptNumber,
		PaymentStatus: types.PaymentStatusProcessing,
		Metadata:      types.Metadata{},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	if err := p.PaymentRepo.CreateAttempt(ctx, attempt); err != nil {
		return nil, err
	}

	return attempt, nil
}

func (p *paymentProcessor) publishWebhookEvent(ctx context.Context, eventName string, paymentID string) {
	webhookPayload, err := json.Marshal(webhookDto.InternalPaymentEvent{
		PaymentID: paymentID,
		TenantID:  types.GetTenantID(ctx),
	})

	if err != nil {
		p.Logger.Errorw("failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
	}
	if err := p.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		p.Logger.Errorf("failed to publish %s event: %v", webhookEvent.EventName, err)
	}
}

// handleCardPayment processes card payments using saved payment methods
func (p *paymentProcessor) handleCardPayment(ctx context.Context, paymentObj *payment.Payment) error {
	p.Logger.Infow("processing card payment",
		"payment_id", paymentObj.ID,
		"customer_id", paymentObj.Metadata["customer_id"],
		"payment_method_id", paymentObj.PaymentMethodID,
		"amount", paymentObj.Amount.String(),
	)

	// Get customer ID from metadata or destination
	customerID, exists := paymentObj.Metadata["customer_id"]
	if !exists || customerID == "" {
		// Try to get customer from invoice if destination is invoice
		if paymentObj.DestinationType == types.PaymentDestinationTypeInvoice {
			invoiceService := NewInvoiceService(p.ServiceParams)
			invoiceResp, err := invoiceService.GetInvoice(ctx, paymentObj.DestinationID)
			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to get invoice for card payment").
					WithReportableDetails(map[string]interface{}{
						"payment_id": paymentObj.ID,
						"invoice_id": paymentObj.DestinationID,
					}).
					Mark(ierr.ErrValidation)
			}
			customerID = invoiceResp.CustomerID
		} else {
			return ierr.NewError("customer_id not found").
				WithHint("Customer ID is required for card payments").
				WithReportableDetails(map[string]interface{}{
					"payment_id": paymentObj.ID,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	p.Logger.Infow("processing card payment for customer",
		"customer_id", customerID,
		"payment_id", paymentObj.ID,
	)

	// Get Stripe integration
	stripeIntegration, err := p.IntegrationFactory.GetStripeIntegration(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get Stripe integration").
			Mark(ierr.ErrSystem)
	}

	// If no specific payment method ID is provided, we need to get one
	if paymentObj.PaymentMethodID == "" {
		// Get the default payment method - this is required for card payments
		customerService := NewCustomerService(p.ServiceParams)
		defaultPaymentMethod, err := stripeIntegration.CustomerSvc.GetDefaultPaymentMethod(ctx, customerID, customerService)
		if err != nil || defaultPaymentMethod == nil {
			p.Logger.Warnw("customer has no default payment method for card payment",
				"customer_id", customerID,
				"payment_id", paymentObj.ID,
				"error", err,
			)
			return ierr.NewError("no default payment method").
				WithHint("Customer must have a default payment method to make card payments. Please make a payment using payment link with save_card_and_make_default=true first.").
				WithReportableDetails(map[string]interface{}{
					"customer_id": customerID,
					"payment_id":  paymentObj.ID,
				}).
				Mark(ierr.ErrValidation)
		}

		// Use the default payment method
		paymentObj.PaymentMethodID = defaultPaymentMethod.ID
		p.Logger.Infow("using default payment method for card payment",
			"customer_id", customerID,
			"payment_id", paymentObj.ID,
			"payment_method_id", paymentObj.PaymentMethodID,
			"card_last4", func() string {
				if defaultPaymentMethod.Card != nil {
					return defaultPaymentMethod.Card.Last4
				}
				return ""
			}(),
		)
	}

	// Charge the saved payment method
	chargeReq := &dto.ChargeSavedPaymentMethodRequest{
		CustomerID:      customerID,
		PaymentMethodID: paymentObj.PaymentMethodID,
		Amount:          paymentObj.Amount,
		Currency:        paymentObj.Currency,
		InvoiceID:       paymentObj.DestinationID,
		PaymentID:       paymentObj.ID,
	}

	// Create service instances for charging
	custSvc := NewCustomerService(p.ServiceParams)
	invSvc := NewInvoiceService(p.ServiceParams)

	paymentIntentResp, err := stripeIntegration.PaymentSvc.ChargeSavedPaymentMethod(ctx, chargeReq, custSvc, invSvc)
	if err != nil {
		// Update payment status to failed
		updateReq := &dto.UpdatePaymentRequest{
			PaymentStatus: lo.ToPtr(string(types.PaymentStatusFailed)),
			ErrorMessage:  lo.ToPtr(err.Error()),
			FailedAt:      lo.ToPtr(time.Now().UTC()),
		}

		paymentService := NewPaymentService(p.ServiceParams)
		if _, updateErr := paymentService.UpdatePayment(ctx, paymentObj.ID, *updateReq); updateErr != nil {
			p.Logger.Errorw("failed to update payment status to failed",
				"error", updateErr,
				"payment_id", paymentObj.ID,
			)
		}

		return err
	}

	// Update payment with Stripe details
	updateReq := &dto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusSucceeded)),
		GatewayPaymentID: lo.ToPtr(paymentIntentResp.ID),
		PaymentGateway:   lo.ToPtr(string(types.PaymentGatewayTypeStripe)),
		PaymentMethodID:  lo.ToPtr(paymentObj.PaymentMethodID),
		SucceededAt:      lo.ToPtr(time.Now().UTC()),
	}

	paymentService := NewPaymentService(p.ServiceParams)
	if _, err := paymentService.UpdatePayment(ctx, paymentObj.ID, *updateReq); err != nil {
		p.Logger.Errorw("failed to update payment status to succeeded",
			"error", err,
			"payment_id", paymentObj.ID,
		)
		return err
	}

	p.Logger.Infow("successfully processed card payment",
		"payment_id", paymentObj.ID,
		"customer_id", customerID,
		"payment_method_id", paymentObj.PaymentMethodID,
		"payment_intent_id", paymentIntentResp.ID,
		"amount", paymentObj.Amount.String(),
	)

	return nil
}

// handleIncompleteSubscriptionPayment checks if the paid invoice is the first invoice for a subscription
// and activates the subscription if it's currently in incomplete status
func (p *paymentProcessor) handleIncompleteSubscriptionPayment(ctx context.Context, invoice *invoice.Invoice) error {
	// Only process subscription invoices that are fully paid
	if invoice.SubscriptionID == nil || !invoice.AmountRemaining.IsZero() {
		return nil
	}

	// Check if this is the first invoice (billing_reason = subscription_create)
	if invoice.BillingReason != string(types.InvoiceBillingReasonSubscriptionCreate) {
		return nil
	}

	p.Logger.Infow("processing first invoice payment for subscription activation",
		"invoice_id", invoice.ID,
		"subscription_id", *invoice.SubscriptionID,
		"billing_reason", invoice.BillingReason)

	// Get the subscription service
	subscriptionService := NewSubscriptionService(p.ServiceParams)

	// Activate the incomplete subscription
	err := subscriptionService.ActivateIncompleteSubscription(ctx, *invoice.SubscriptionID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to activate incomplete subscription after first invoice payment").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": *invoice.SubscriptionID,
				"invoice_id":      invoice.ID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	p.Logger.Infow("successfully activated subscription after first invoice payment",
		"invoice_id", invoice.ID,
		"subscription_id", *invoice.SubscriptionID)

	return nil
}
