package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"
)

// PaymentService handles Stripe payment operations
type PaymentService struct {
	client         *Client
	customerSvc    *CustomerService
	invoiceSyncSvc *InvoiceSyncService
	invoiceRepo    invoice.Repository
	paymentRepo    payment.Repository
	logger         *logger.Logger
}

// NewPaymentService creates a new Stripe payment service
func NewPaymentService(
	client *Client,
	customerSvc *CustomerService,
	invoiceSyncSvc *InvoiceSyncService,
	invoiceRepo invoice.Repository,
	paymentRepo payment.Repository,
	logger *logger.Logger,
) *PaymentService {
	return &PaymentService{
		client:         client,
		customerSvc:    customerSvc,
		invoiceSyncSvc: invoiceSyncSvc,
		invoiceRepo:    invoiceRepo,
		paymentRepo:    paymentRepo,
		logger:         logger,
	}
}

// CreatePaymentLink creates a Stripe checkout session for payment
func (s *PaymentService) CreatePaymentLink(ctx context.Context, req *dto.CreateStripePaymentLinkRequest, customerService interfaces.CustomerService, invoiceService interfaces.InvoiceService) (*dto.StripePaymentLinkResponse, error) {
	s.logger.Infow("creating stripe payment link",
		"invoice_id", req.InvoiceID,
		"customer_id", req.CustomerID,
		"amount", req.Amount.String(),
		"currency", req.Currency,
		"environment_id", req.EnvironmentID,
	)

	// Get Stripe client and config
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// Validate invoice and check payment eligibility
	invoiceResp, err := invoiceService.GetInvoice(ctx, req.InvoiceID)
	if err != nil {
		return nil, ierr.NewError("failed to get invoice").
			WithHint("Invoice not found").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": req.InvoiceID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Validate invoice payment status
	if invoiceResp.PaymentStatus == types.PaymentStatusSucceeded {
		return nil, ierr.NewError("invoice is already paid").
			WithHint("Cannot create payment link for an already paid invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":     req.InvoiceID,
				"payment_status": invoiceResp.PaymentStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	if invoiceResp.InvoiceStatus == types.InvoiceStatusVoided {
		return nil, ierr.NewError("invoice is voided").
			WithHint("Cannot create payment link for a voided invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":     req.InvoiceID,
				"invoice_status": invoiceResp.InvoiceStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate payment amount against invoice remaining balance
	if req.Amount.GreaterThan(invoiceResp.AmountRemaining) {
		return nil, ierr.NewError("payment amount exceeds invoice remaining balance").
			WithHint("Payment amount cannot be greater than the remaining balance on the invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":        req.InvoiceID,
				"payment_amount":    req.Amount.String(),
				"invoice_remaining": invoiceResp.AmountRemaining.String(),
				"invoice_total":     invoiceResp.AmountDue.String(),
				"invoice_paid":      invoiceResp.AmountPaid.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate currency matches invoice currency
	if req.Currency != invoiceResp.Currency {
		return nil, ierr.NewError("payment currency does not match invoice currency").
			WithHint("Payment currency must match the invoice currency").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":       req.InvoiceID,
				"payment_currency": req.Currency,
				"invoice_currency": invoiceResp.Currency,
			}).
			Mark(ierr.ErrValidation)
	}

	// Ensure customer is synced to Stripe before creating payment link
	customerResp, err := s.customerSvc.EnsureCustomerSyncedToStripe(ctx, req.CustomerID, customerService)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get Stripe customer ID (should exist after sync)
	stripeCustomerID, exists := customerResp.Customer.Metadata["stripe_customer_id"]
	if !exists || stripeCustomerID == "" {
		return nil, ierr.NewError("customer does not have Stripe customer ID after sync").
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Convert amount to cents (Stripe expects amounts in smallest currency unit)
	amountCents := req.Amount.Mul(decimal.NewFromInt(100)).IntPart()

	// Build comprehensive product name with all information
	productName := fmt.Sprintf(customerResp.Customer.Name)

	// Build detailed description with all invoice information
	var descriptionParts []string

	// Add invoice information
	invoiceInfo := fmt.Sprintf("Invoice: %s", lo.FromPtrOr(invoiceResp.InvoiceNumber, req.InvoiceID))
	descriptionParts = append(descriptionParts, invoiceInfo)

	// Add invoice total
	totalInfo := fmt.Sprintf("Invoice Total: %s %s", invoiceResp.Total.String(), invoiceResp.Currency)
	descriptionParts = append(descriptionParts, totalInfo)

	// Add items details
	if len(invoiceResp.LineItems) > 0 {
		var itemDetails []string
		for _, lineItem := range invoiceResp.LineItems {
			if lineItem.Amount.IsZero() {
				continue // Skip zero-amount items
			}

			var entityType string
			var itemName string

			// Determine entity type and name using enums
			if lineItem.EntityType != nil {
				switch *lineItem.EntityType {
				case string(types.InvoiceLineItemEntityTypePlan):
					entityType = "Plan"
					itemName = lo.FromPtrOr(lineItem.DisplayName, "")
					if itemName == "" {
						itemName = lo.FromPtrOr(lineItem.PlanDisplayName, "Plan")
					}
				case string(types.InvoiceLineItemEntityTypeAddon):
					entityType = "Add-on"
					itemName = lo.FromPtrOr(lineItem.DisplayName, "Add-on")
				default:
					entityType = "Item"
					itemName = lo.FromPtrOr(lineItem.DisplayName, "Service")
				}
			}
			// Format as "Entity: Name ($Amount)"
			itemDetail := fmt.Sprintf("%s: %s ($%s)", entityType, itemName, lineItem.Amount.String())
			itemDetails = append(itemDetails, itemDetail)
		}

		if len(itemDetails) > 0 {
			descriptionParts = append(descriptionParts, itemDetails...)
		}
	}

	// Join all parts with separators for better readability
	productDescription := strings.Join(descriptionParts, " • ")

	// Create a single line item for the exact payment amount requested
	lineItems := []*stripe.CheckoutSessionCreateLineItemParams{
		{
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency: stripe.String(req.Currency),
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name:        stripe.String(productName),
					Description: stripe.String(productDescription),
				},
				UnitAmount: stripe.Int64(amountCents),
			},
			Quantity: stripe.Int64(1),
		},
	}

	// Build metadata for the session
	metadata := map[string]string{
		"flexprice_invoice_id": req.InvoiceID,
		"customer_id":          req.CustomerID,
		"environment_id":       req.EnvironmentID,
		"payment_source":       "flexprice",
		"payment_type":         "checkout",
		"flexprice_payment_id": req.PaymentID,
	}

	// Try to get Stripe invoice ID for attachment tracking
	if stripeInvoiceID, err := s.invoiceSyncSvc.GetStripeInvoiceID(ctx, req.InvoiceID); err == nil && stripeInvoiceID != "" {
		metadata["stripe_invoice_id"] = stripeInvoiceID
		s.logger.Infow("payment link will be tracked for Stripe invoice attachment",
			"flexprice_invoice_id", req.InvoiceID,
			"stripe_invoice_id", stripeInvoiceID)
	} else {
		s.logger.Debugw("no Stripe invoice found for payment link",
			"flexprice_invoice_id", req.InvoiceID,
			"error", err)
	}

	// Add custom metadata if provided
	for k, v := range req.Metadata {
		metadata[k] = v
	}

	// Provide default URLs if not provided
	successURL := req.SuccessURL
	if successURL == "" {
		successURL = "https://admin.flexprice.io/"
	}

	cancelURL := req.CancelURL
	if cancelURL == "" {
		cancelURL = "https://admin.flexprice.io/"
	}

	// Create checkout session parameters
	params := &stripe.CheckoutSessionCreateParams{
		LineItems:           lineItems,
		Mode:                stripe.String("payment"),
		AllowPromotionCodes: stripe.Bool(true),
		SuccessURL:          stripe.String(successURL),
		CancelURL:           stripe.String(cancelURL),
		Metadata:            metadata,
		Customer:            stripe.String(stripeCustomerID),
		PaymentIntentData: &stripe.CheckoutSessionCreatePaymentIntentDataParams{
			Metadata: metadata,
		},
	}

	// Only save payment method for future use if SaveCardAndMakeDefault is true
	if req.SaveCardAndMakeDefault {
		params.PaymentIntentData = &stripe.CheckoutSessionCreatePaymentIntentDataParams{
			SetupFutureUsage: stripe.String("off_session"),
			Metadata:         metadata,
		}
		s.logger.Infow("payment link configured to save card and make default",
			"invoice_id", req.InvoiceID,
			"customer_id", req.CustomerID,
		)
	} else {
		s.logger.Infow("payment link configured for one-time payment only",
			"invoice_id", req.InvoiceID,
			"customer_id", req.CustomerID,
		)
	}

	// Create the checkout session
	session, err := stripeClient.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		s.logger.Errorw("failed to create Stripe checkout session",
			"error", err,
			"invoice_id", req.InvoiceID)
		return nil, ierr.NewError("failed to create payment link").
			WithHint("Unable to create Stripe checkout session").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": req.InvoiceID,
				"error":      err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	response := &dto.StripePaymentLinkResponse{
		ID:         session.ID,
		PaymentURL: session.URL,
		PaymentIntentID: func() string {
			if session.PaymentIntent != nil {
				return session.PaymentIntent.ID
			}
			return ""
		}(),
		Amount:    req.Amount,
		Currency:  req.Currency,
		Status:    string(session.Status),
		CreatedAt: session.Created,
		PaymentID: "", // Payment ID will be set by the calling code
	}

	s.logger.Infow("successfully created stripe payment link",
		"payment_id", response.PaymentID,
		"session_id", session.ID,
		"payment_url", session.URL,
		"invoice_id", req.InvoiceID,
		"amount", req.Amount.String(),
		"currency", req.Currency,
	)

	return response, nil
}

// ChargeSavedPaymentMethod charges a customer using their saved payment method
func (s *PaymentService) ChargeSavedPaymentMethod(ctx context.Context, req *dto.ChargeSavedPaymentMethodRequest, customerService interfaces.CustomerService, invoiceService interfaces.InvoiceService) (*dto.PaymentIntentResponse, error) {
	// Get Stripe client
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// Ensure customer is synced to Stripe before charging saved payment method
	ourCustomerResp, err := s.customerSvc.EnsureCustomerSyncedToStripe(ctx, req.CustomerID, customerService)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}
	ourCustomer := ourCustomerResp.Customer

	stripeCustomerID, exists := ourCustomer.Metadata["stripe_customer_id"]
	if !exists || stripeCustomerID == "" {
		return nil, ierr.NewError("customer not found in Stripe after sync").
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get invoice to validate payment amount
	invoiceResp, err := invoiceService.GetInvoice(ctx, req.InvoiceID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get invoice for payment validation").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": req.InvoiceID,
			}).
			Mark(ierr.ErrNotFound)
	}
	// Validate payment amount against invoice remaining balance
	if req.Amount.GreaterThan(invoiceResp.AmountRemaining) {
		return nil, ierr.NewError("payment amount exceeds invoice remaining balance").
			WithHint("Payment amount cannot be greater than the remaining balance on the invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":        invoiceResp.ID,
				"payment_amount":    req.Amount.String(),
				"invoice_remaining": invoiceResp.AmountRemaining.String(),
				"invoice_total":     invoiceResp.AmountDue.String(),
				"invoice_paid":      invoiceResp.AmountPaid.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	// Create PaymentIntent with saved payment method
	amountInCents := req.Amount.Mul(decimal.NewFromInt(100)).IntPart()
	params := &stripe.PaymentIntentCreateParams{
		Amount:        stripe.Int64(amountInCents),
		Currency:      stripe.String(req.Currency),
		Customer:      stripe.String(stripeCustomerID),
		PaymentMethod: stripe.String(req.PaymentMethodID),
		OffSession:    stripe.Bool(true), // Important: indicates off-session payment
		Confirm:       stripe.Bool(true), // Confirm immediately
		Metadata: map[string]string{
			"flexprice_customer_id": req.CustomerID,
			"environment_id":        types.GetEnvironmentID(ctx),
			"invoice_id":            req.InvoiceID,
			"payment_source":        "flexprice", // KEY DIFFERENTIATOR
			"payment_type":          "charge",
			"flexprice_payment_id":  req.PaymentID,
		},
	}

	// Try to get Stripe invoice ID for later attachment
	stripeInvoiceID, err := s.invoiceSyncSvc.GetStripeInvoiceID(ctx, req.InvoiceID)
	if err != nil {
		s.logger.Debugw("no Stripe invoice found, creating standalone payment",
			"flexprice_invoice_id", req.InvoiceID,
			"error", err)
		stripeInvoiceID = "" // Clear any partial value
	} else {
		s.logger.Infow("will attach payment to Stripe invoice after successful payment",
			"flexprice_invoice_id", req.InvoiceID,
			"stripe_invoice_id", stripeInvoiceID,
			"payment_method_id", req.PaymentMethodID)
		// Add to metadata for tracking
		params.Metadata["stripe_invoice_id"] = stripeInvoiceID
	}

	paymentIntent, err := stripeClient.V1PaymentIntents.Create(ctx, params)
	if err != nil {
		// Handle specific error cases
		if stripeErr, ok := err.(*stripe.Error); ok {
			switch stripeErr.Code {
			case stripe.ErrorCodeAuthenticationRequired:
				// Payment requires authentication - customer needs to return to complete
				return nil, ierr.NewError("payment requires authentication").
					WithHint("Customer must return to complete payment authentication").
					WithReportableDetails(map[string]interface{}{
						"customer_id":       req.CustomerID,
						"payment_method_id": req.PaymentMethodID,
						"stripe_error_code": stripeErr.Code,
						"payment_intent_id": stripeErr.PaymentIntent.ID,
					}).
					Mark(ierr.ErrInvalidOperation)
			case stripe.ErrorCodeCardDeclined:
				// Card was declined
				return nil, ierr.NewError("payment method declined").
					WithHint("The saved payment method was declined").
					WithReportableDetails(map[string]interface{}{
						"customer_id":       req.CustomerID,
						"payment_method_id": req.PaymentMethodID,
						"stripe_error_code": stripeErr.Code,
					}).
					Mark(ierr.ErrInvalidOperation)
			}
		}

		s.logger.Errorw("failed to create PaymentIntent with saved payment method",
			"error", err,
			"customer_id", req.CustomerID,
			"payment_method_id", req.PaymentMethodID,
			"amount", req.Amount.String(),
		)
		return nil, ierr.NewError("failed to charge saved payment method").
			WithHint("Unable to process payment with saved payment method").
			WithReportableDetails(map[string]interface{}{
				"customer_id":       req.CustomerID,
				"payment_method_id": req.PaymentMethodID,
				"error":             err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	// If payment succeeded and we have a Stripe invoice, attach the payment to the invoice
	if paymentIntent.Status == stripe.PaymentIntentStatusSucceeded && stripeInvoiceID != "" {
		if err := s.AttachPaymentToStripeInvoice(ctx, stripeClient, paymentIntent.ID, stripeInvoiceID); err != nil {
			s.logger.Errorw("failed to attach payment to Stripe invoice",
				"error", err,
				"payment_intent_id", paymentIntent.ID,
				"stripe_invoice_id", stripeInvoiceID)
			// Don't fail the whole payment, just log the error
			// The payment was successful, attachment is a bonus feature
		}
	}

	response := &dto.PaymentIntentResponse{
		ID:            paymentIntent.ID,
		Status:        string(paymentIntent.Status),
		Amount:        req.Amount,
		Currency:      req.Currency,
		CustomerID:    stripeCustomerID,
		PaymentMethod: req.PaymentMethodID,
		CreatedAt:     paymentIntent.Created,
	}

	s.logger.Infow("successfully charged saved payment method",
		"payment_intent_id", paymentIntent.ID,
		"customer_id", req.CustomerID,
		"payment_method_id", req.PaymentMethodID,
		"amount", req.Amount.String(),
		"status", paymentIntent.Status,
		"stripe_invoice_id", stripeInvoiceID,
		"attached_to_invoice", stripeInvoiceID != "",
	)

	return response, nil
}

// GetPaymentStatus gets the payment status from Stripe checkout session
func (s *PaymentService) GetPaymentStatus(ctx context.Context, sessionID string, environmentID string) (*dto.PaymentStatusResponse, error) {
	// Get Stripe client
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// Get the checkout session with expanded fields
	params := &stripe.CheckoutSessionRetrieveParams{
		Expand: []*string{
			stripe.String("payment_intent"),
			stripe.String("line_items"),
			stripe.String("customer"),
		},
	}
	session, err := stripeClient.V1CheckoutSessions.Retrieve(ctx, sessionID, params)
	if err != nil {
		s.logger.Errorw("failed to get Stripe checkout session",
			"error", err,
			"session_id", sessionID)
		return nil, ierr.NewError("failed to get payment status").
			WithHint("Unable to retrieve Stripe checkout session").
			WithReportableDetails(map[string]interface{}{
				"session_id": sessionID,
				"error":      err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	// Log session details for debugging
	s.logger.Debugw("retrieved Stripe checkout session",
		"session_id", session.ID,
		"status", session.Status,
		"has_payment_intent", session.PaymentIntent != nil,
		"has_line_items", session.LineItems != nil,
		"line_items_count", func() int {
			if session.LineItems != nil {
				return len(session.LineItems.Data)
			}
			return 0
		}(),
		"has_customer", session.Customer != nil,
	)

	// Get payment intent if available
	var paymentIntentID string
	var paymentStatus string
	var amount decimal.Decimal
	var currency string
	var paymentMethodID string

	// First try to get data from payment intent
	if session.PaymentIntent != nil {
		paymentIntentID = session.PaymentIntent.ID
		paymentStatus = string(session.PaymentIntent.Status)
		if session.PaymentIntent.Amount > 0 {
			amount = decimal.NewFromInt(session.PaymentIntent.Amount).Div(decimal.NewFromInt(100))
		}
		if session.PaymentIntent.Currency != "" {
			currency = string(session.PaymentIntent.Currency)
		}

		// Get payment method ID from payment intent
		if paymentIntentID != "" {
			paymentIntent, err := stripeClient.V1PaymentIntents.Retrieve(ctx, paymentIntentID, nil)
			if err != nil {
				s.logger.Warnw("failed to get payment intent details",
					"error", err,
					"payment_intent_id", paymentIntentID)
				// Don't fail the entire request if we can't get payment intent details
			} else {
				// Get the payment method ID from the payment intent
				if paymentIntent.PaymentMethod != nil {
					paymentMethodID = paymentIntent.PaymentMethod.ID
				}
			}
		}
	}

	// If payment intent data is incomplete, try to get from session
	if paymentStatus == "" {
		paymentStatus = string(session.Status)
	}

	// If amount is still 0, try to get from line items
	if amount.IsZero() && session.LineItems != nil && len(session.LineItems.Data) > 0 {
		item := session.LineItems.Data[0]
		if item.AmountTotal > 0 {
			amount = decimal.NewFromInt(item.AmountTotal).Div(decimal.NewFromInt(100))
		}
		if item.Currency != "" && currency == "" {
			currency = string(item.Currency)
		}
	}

	// If currency is still empty, try to get from session metadata or default
	if currency == "" {
		// Check if currency is in metadata
		if session.Metadata != nil {
			if curr, exists := session.Metadata["currency"]; exists {
				currency = curr
			}
		}
		// Default to USD if still empty
		if currency == "" {
			currency = "usd"
		}
	}

	// Log extracted values for debugging
	s.logger.Debugw("extracted payment status values",
		"session_id", session.ID,
		"payment_intent_id", paymentIntentID,
		"status", paymentStatus,
		"amount", amount.String(),
		"currency", currency,
		"customer_id", func() string {
			if session.Customer != nil {
				return session.Customer.ID
			}
			return ""
		}(),
	)

	return &dto.PaymentStatusResponse{
		SessionID:       session.ID,
		PaymentIntentID: paymentIntentID,
		PaymentMethodID: paymentMethodID,
		Status:          paymentStatus,
		Amount:          amount,
		Currency:        currency,
		CustomerID: func() string {
			if session.Customer != nil {
				return session.Customer.ID
			}
			return ""
		}(),
		CreatedAt: session.Created,
		ExpiresAt: session.ExpiresAt,
		Metadata:  session.Metadata,
	}, nil
}

// GetPaymentStatusByPaymentIntent gets payment status directly from a payment intent ID
func (s *PaymentService) GetPaymentStatusByPaymentIntent(ctx context.Context, paymentIntentID string, environmentID string) (*dto.PaymentStatusResponse, error) {
	// Get Stripe client
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// Get the payment intent with expanded fields
	params := &stripe.PaymentIntentRetrieveParams{
		Expand: []*string{
			stripe.String("payment_method"),
			stripe.String("customer"),
		},
	}
	paymentIntent, err := stripeClient.V1PaymentIntents.Retrieve(ctx, paymentIntentID, params)
	if err != nil {
		s.logger.Errorw("failed to get Stripe payment intent",
			"error", err,
			"payment_intent_id", paymentIntentID)
		return nil, ierr.NewError("failed to get payment status").
			WithHint("Unable to retrieve Stripe payment intent").
			WithReportableDetails(map[string]interface{}{
				"payment_intent_id": paymentIntentID,
				"error":             err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	// Log payment intent details for debugging
	s.logger.Debugw("retrieved Stripe payment intent",
		"payment_intent_id", paymentIntent.ID,
		"status", paymentIntent.Status,
		"has_payment_method", paymentIntent.PaymentMethod != nil,
		"has_customer", paymentIntent.Customer != nil,
	)

	// Extract payment method ID
	var paymentMethodID string
	if paymentIntent.PaymentMethod != nil {
		paymentMethodID = paymentIntent.PaymentMethod.ID
	}

	// Convert amount from cents to decimal
	var amount decimal.Decimal
	if paymentIntent.Amount > 0 {
		amount = decimal.NewFromInt(paymentIntent.Amount).Div(decimal.NewFromInt(100))
	}

	// Get currency
	currency := string(paymentIntent.Currency)
	if currency == "" {
		currency = "usd" // Default to USD
	}

	// Log extracted values for debugging
	s.logger.Debugw("extracted payment intent status values",
		"payment_intent_id", paymentIntent.ID,
		"status", string(paymentIntent.Status),
		"amount", amount.String(),
		"currency", currency,
		"payment_method_id", paymentMethodID,
		"customer_id", func() string {
			if paymentIntent.Customer != nil {
				return paymentIntent.Customer.ID
			}
			return ""
		}(),
	)

	return &dto.PaymentStatusResponse{
		SessionID:       "", // No session ID for direct payment intent
		PaymentIntentID: paymentIntent.ID,
		PaymentMethodID: paymentMethodID,
		Status:          string(paymentIntent.Status),
		Amount:          amount,
		Currency:        currency,
		CustomerID: func() string {
			if paymentIntent.Customer != nil {
				return paymentIntent.Customer.ID
			}
			return ""
		}(),
		CreatedAt: paymentIntent.Created,
		ExpiresAt: 0, // Payment intents don't have expires_at
		Metadata:  paymentIntent.Metadata,
	}, nil
}

// GetPaymentIntent gets a payment intent from Stripe
func (s *PaymentService) GetPaymentIntent(ctx context.Context, paymentIntentID string, environmentID string) (*stripe.PaymentIntent, error) {
	// Get Stripe client
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// Get the payment intent with expanded fields
	params := &stripe.PaymentIntentRetrieveParams{}
	params.AddExpand("invoice")
	params.AddExpand("payment_method")
	params.AddExpand("customer")
	paymentIntent, err := stripeClient.V1PaymentIntents.Retrieve(ctx, paymentIntentID, params)
	if err != nil {
		s.logger.Errorw("failed to get Stripe payment intent",
			"error", err,
			"payment_intent_id", paymentIntentID)
		return nil, ierr.NewError("failed to get payment intent").
			WithHint("Unable to retrieve Stripe payment intent").
			WithReportableDetails(map[string]interface{}{
				"payment_intent_id": paymentIntentID,
				"error":             err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	return paymentIntent, nil
}

// ParseWebhookEvent parses a Stripe webhook event with signature verification
func (s *PaymentService) ParseWebhookEvent(payload []byte, signature string, webhookSecret string) (*stripe.Event, error) {
	// Verify the webhook signature, ignoring API version mismatch
	options := webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	}
	event, err := webhook.ConstructEventWithOptions(payload, signature, webhookSecret, options)
	if err != nil {
		// Log the error using structured logging
		s.logger.Errorw("Stripe webhook verification failed", "error", err)
		return nil, ierr.NewError("failed to verify webhook signature").
			WithHint("Invalid webhook signature or payload").
			Mark(ierr.ErrValidation)
	}
	return &event, nil
}

// GetCustomerPaymentMethods retrieves saved payment methods for a customer
func (s *PaymentService) GetCustomerPaymentMethods(ctx context.Context, req *dto.GetCustomerPaymentMethodsRequest, customerService interfaces.CustomerService) ([]*dto.PaymentMethodResponse, error) {
	// Get Stripe client
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// Get our customer to find Stripe customer ID
	ourCustomerResp, err := customerService.GetCustomer(ctx, req.CustomerID)
	if err != nil {
		return nil, err
	}
	ourCustomer := ourCustomerResp.Customer

	stripeCustomerID, exists := ourCustomer.Metadata["stripe_customer_id"]
	if !exists || stripeCustomerID == "" {
		// No Stripe customer ID means no saved payment methods
		s.logger.Warnw("customer has no stripe_customer_id in metadata",
			"customer_id", req.CustomerID,
			"customer_metadata", ourCustomer.Metadata,
		)
		return []*dto.PaymentMethodResponse{}, nil
	}

	s.logger.Infow("retrieving payment methods for stripe customer",
		"customer_id", req.CustomerID,
		"stripe_customer_id", stripeCustomerID,
	)

	// List payment methods for the customer
	params := &stripe.PaymentMethodListParams{
		Customer: stripe.String(stripeCustomerID),
		Type:     stripe.String("card"),
	}

	paymentMethods := stripeClient.V1PaymentMethods.List(ctx, params)
	var responses []*dto.PaymentMethodResponse

	for pm, err := range paymentMethods {
		if err != nil {
			s.logger.Errorw("failed to list payment methods",
				"error", err,
				"customer_id", req.CustomerID,
				"stripe_customer_id", stripeCustomerID)
			break
		}

		response := &dto.PaymentMethodResponse{
			ID:       pm.ID,
			Type:     string(pm.Type),
			Customer: pm.Customer.ID,
			Created:  pm.Created,
			Metadata: make(map[string]interface{}),
		}

		// Convert metadata from map[string]string to map[string]interface{}
		for k, v := range pm.Metadata {
			response.Metadata[k] = v
		}

		if pm.Card != nil {
			response.Card = &dto.CardDetails{
				Brand:       string(pm.Card.Brand),
				Last4:       pm.Card.Last4,
				ExpMonth:    int(pm.Card.ExpMonth),
				ExpYear:     int(pm.Card.ExpYear),
				Fingerprint: pm.Card.Fingerprint,
			}
		}

		responses = append(responses, response)
	}

	if len(responses) == 0 {
		s.logger.Warnw("no payment methods found for customer",
			"customer_id", req.CustomerID,
			"stripe_customer_id", stripeCustomerID)
		return responses, nil // Return empty list instead of error
	}

	s.logger.Infow("successfully retrieved payment methods",
		"customer_id", req.CustomerID,
		"stripe_customer_id", stripeCustomerID,
		"payment_methods_count", len(responses),
	)

	return responses, nil
}

// SetDefaultPaymentMethod sets a payment method as default in Stripe
func (s *PaymentService) SetDefaultPaymentMethod(ctx context.Context, customerID, paymentMethodID string, customerService interfaces.CustomerService) error {
	// Get Stripe client
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return err
	}

	// Get our customer to find Stripe customer ID
	ourCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return err
	}
	ourCustomer := ourCustomerResp.Customer

	stripeCustomerID, exists := ourCustomer.Metadata["stripe_customer_id"]
	if !exists || stripeCustomerID == "" {
		return ierr.NewError("customer not found in Stripe").
			WithHint("Customer must have a Stripe account").
			Mark(ierr.ErrNotFound)
	}

	s.logger.Infow("setting default payment method in Stripe",
		"customer_id", customerID,
		"stripe_customer_id", stripeCustomerID,
		"payment_method_id", paymentMethodID,
	)

	// Update customer's default payment method in Stripe
	params := &stripe.CustomerUpdateParams{
		InvoiceSettings: &stripe.CustomerUpdateInvoiceSettingsParams{
			DefaultPaymentMethod: stripe.String(paymentMethodID),
		},
	}

	_, err = stripeClient.V1Customers.Update(ctx, stripeCustomerID, params)
	if err != nil {
		s.logger.Errorw("failed to set default payment method in Stripe",
			"error", err,
			"customer_id", customerID,
			"stripe_customer_id", stripeCustomerID,
			"payment_method_id", paymentMethodID,
		)
		return ierr.NewError("failed to set default payment method").
			WithHint("Could not update default payment method in Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id":       customerID,
				"payment_method_id": paymentMethodID,
			}).
			Mark(ierr.ErrSystem)
	}

	s.logger.Infow("successfully set default payment method in Stripe",
		"customer_id", customerID,
		"stripe_customer_id", stripeCustomerID,
		"payment_method_id", paymentMethodID,
	)

	return nil
}

// HasSavedPaymentMethods checks if a customer has any saved payment methods
func (s *PaymentService) HasSavedPaymentMethods(ctx context.Context, customerID string, customerService interfaces.CustomerService) (bool, error) {
	req := &dto.GetCustomerPaymentMethodsRequest{
		CustomerID: customerID,
	}

	paymentMethods, err := s.GetCustomerPaymentMethods(ctx, req, customerService)
	if err != nil {
		return false, err
	}

	return len(paymentMethods) > 0, nil
}

// ReconcilePaymentWithInvoice updates the invoice payment status and amounts when a payment succeeds
func (s *PaymentService) ReconcilePaymentWithInvoice(ctx context.Context, paymentID string, paymentAmount decimal.Decimal, paymentService interfaces.PaymentService, invoiceService interfaces.InvoiceService) error {
	s.logger.Infow("starting payment reconciliation with invoice",
		"payment_id", paymentID,
		"payment_amount", paymentAmount.String(),
	)

	// Get the payment record
	payment, err := paymentService.GetPayment(ctx, paymentID)
	if err != nil {
		s.logger.Errorw("failed to get payment record for reconciliation",
			"error", err,
			"payment_id", paymentID,
		)
		return err
	}

	s.logger.Infow("got payment record for reconciliation",
		"payment_id", paymentID,
		"invoice_id", payment.DestinationID,
		"payment_amount", payment.Amount.String(),
	)

	// Get the invoice
	invoiceResp, err := invoiceService.GetInvoice(ctx, payment.DestinationID)
	if err != nil {
		s.logger.Errorw("failed to get invoice for payment reconciliation",
			"error", err,
			"payment_id", paymentID,
			"invoice_id", payment.DestinationID,
		)
		return ierr.WithError(err).
			WithHint("Failed to get invoice for payment reconciliation").
			WithReportableDetails(map[string]interface{}{
				"payment_id": paymentID,
				"invoice_id": payment.DestinationID,
			}).
			Mark(ierr.ErrSystem)
	}

	s.logger.Infow("got invoice for reconciliation",
		"payment_id", paymentID,
		"invoice_id", payment.DestinationID,
		"invoice_amount_due", invoiceResp.AmountDue.String(),
		"invoice_amount_paid", invoiceResp.AmountPaid.String(),
		"invoice_amount_remaining", invoiceResp.AmountRemaining.String(),
		"invoice_payment_status", invoiceResp.PaymentStatus,
		"invoice_status", invoiceResp.InvoiceStatus,
	)

	// Calculate new amounts
	newAmountPaid := invoiceResp.AmountPaid.Add(paymentAmount)
	newAmountRemaining := invoiceResp.AmountDue.Sub(newAmountPaid)

	// Determine payment status
	var newPaymentStatus types.PaymentStatus
	if newAmountRemaining.IsZero() {
		newPaymentStatus = types.PaymentStatusSucceeded
	} else if newAmountRemaining.IsNegative() {
		// Invoice is overpaid
		newPaymentStatus = types.PaymentStatusOverpaid
		// For overpaid invoices, amount_remaining should be 0
		newAmountRemaining = decimal.Zero
	} else {
		newPaymentStatus = types.PaymentStatusPending
	}

	s.logger.Infow("calculated new amounts for reconciliation",
		"payment_id", paymentID,
		"invoice_id", payment.DestinationID,
		"payment_amount", paymentAmount.String(),
		"new_amount_paid", newAmountPaid.String(),
		"new_amount_remaining", newAmountRemaining.String(),
		"new_payment_status", newPaymentStatus,
	)

	// Update invoice payment status and amounts using reconciliation method
	s.logger.Infow("calling invoice reconciliation",
		"payment_id", paymentID,
		"invoice_id", payment.DestinationID,
		"payment_amount", paymentAmount.String(),
		"new_payment_status", newPaymentStatus,
	)

	err = invoiceService.ReconcilePaymentStatus(ctx, payment.DestinationID, newPaymentStatus, &paymentAmount)
	if err != nil {
		s.logger.Errorw("failed to update invoice payment status during reconciliation",
			"error", err,
			"payment_id", paymentID,
			"invoice_id", payment.DestinationID,
			"payment_amount", paymentAmount.String(),
			"new_payment_status", newPaymentStatus,
		)
		return ierr.WithError(err).
			WithHint("Failed to update invoice payment status").
			WithReportableDetails(map[string]interface{}{
				"payment_id":         paymentID,
				"invoice_id":         payment.DestinationID,
				"payment_amount":     paymentAmount.String(),
				"new_payment_status": newPaymentStatus,
			}).
			Mark(ierr.ErrSystem)
	}

	s.logger.Infow("successfully reconciled payment with invoice",
		"payment_id", paymentID,
		"invoice_id", payment.DestinationID,
		"payment_amount", paymentAmount.String(),
		"new_payment_status", newPaymentStatus,
		"new_amount_paid", newAmountPaid.String(),
		"new_amount_remaining", newAmountRemaining.String(),
	)

	return nil
}

// AttachPaymentToStripeInvoice attaches a successful PaymentIntent to a Stripe invoice
func (s *PaymentService) AttachPaymentToStripeInvoice(ctx context.Context, stripeClient *stripe.Client, paymentIntentID, stripeInvoiceID string) error {
	s.logger.Infow("attaching payment to Stripe invoice",
		"payment_intent_id", paymentIntentID,
		"stripe_invoice_id", stripeInvoiceID)

	// Use the invoice.AttachPayment method as per Stripe documentation
	attachParams := &stripe.InvoiceAttachPaymentParams{
		PaymentIntent: stripe.String(paymentIntentID),
	}

	_, err := stripeClient.V1Invoices.AttachPayment(ctx, stripeInvoiceID, attachParams)
	if err != nil {
		return ierr.NewError("failed to attach payment to Stripe invoice").
			WithHint("Payment succeeded but couldn't be attached to invoice").
			WithReportableDetails(map[string]interface{}{
				"stripe_invoice_id": stripeInvoiceID,
				"payment_intent_id": paymentIntentID,
				"error":             err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	s.logger.Infow("successfully attached payment to Stripe invoice",
		"payment_intent_id", paymentIntentID,
		"stripe_invoice_id", stripeInvoiceID)

	return nil
}

// PaymentExistsByGatewayPaymentID checks if a payment already exists with the given gateway payment ID
func (s *PaymentService) PaymentExistsByGatewayPaymentID(ctx context.Context, gatewayPaymentID string) (bool, error) {
	if gatewayPaymentID == "" {
		return false, nil
	}

	filter := types.NewNoLimitPaymentFilter()
	if filter.QueryFilter != nil {
		limit := 1
		filter.QueryFilter.Limit = &limit
	}
	filter.GatewayPaymentID = &gatewayPaymentID

	payments, err := s.paymentRepo.List(ctx, filter)
	if err != nil {
		return false, err
	}

	// Check if any payment has matching gateway_payment_id
	for _, p := range payments {
		if p.GatewayPaymentID != nil && *p.GatewayPaymentID == gatewayPaymentID {
			return true, nil
		}
	}

	return false, nil
}

// SetupIntent creates a Setup Intent with Stripe for saving payment methods
func (s *PaymentService) SetupIntent(ctx context.Context, customerID string, req *dto.CreateSetupIntentRequest, customerService interfaces.CustomerService) (*dto.SetupIntentResponse, error) {
	s.logger.Infow("creating stripe setup intent",
		"customer_id", customerID,
		"usage", req.Usage,
		"payment_method_types", req.PaymentMethodTypes,
	)

	// Get Stripe client
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// Ensure customer is synced to Stripe before creating setup intent
	customerResp, err := s.customerSvc.EnsureCustomerSyncedToStripe(ctx, customerID, customerService)
	if err != nil {
		s.logger.Errorw("failed to sync customer to Stripe",
			"error", err,
			"customer_id", customerID)
		return nil, ierr.WithError(err).
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get Stripe customer ID (should exist after sync)
	stripeCustomerID, exists := customerResp.Customer.Metadata["stripe_customer_id"]
	if !exists || stripeCustomerID == "" {
		return nil, ierr.NewError("customer does not have Stripe customer ID after sync").
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Set default values
	usage := req.Usage
	if usage == "" {
		usage = "off_session" // Default as per Stripe documentation
	}

	paymentMethodTypes := req.PaymentMethodTypes
	// If no payment method types specified, let Stripe use its defaults (supports all available types)

	// Use user-provided URLs directly (no defaults)
	successURL := req.SuccessURL
	cancelURL := req.CancelURL

	// Build metadata for the setup intent
	metadata := map[string]string{
		"customer_id":    customerID,
		"environment_id": types.GetEnvironmentID(ctx),
		"usage":          usage,
	}

	// Add custom metadata if provided (exclude internal connection fields)
	for k, v := range req.Metadata {
		if k != "connection_id" && k != "connection_name" {
			metadata[k] = v
		}
	}

	// Add set_default flag to metadata if requested
	if req.SetDefault {
		metadata["set_default"] = "true"
		s.logger.Infow("setup intent will be set as default when succeeded",
			"customer_id", customerID,
			"set_default", "true",
			"metadata", metadata)
	}

	// Create Setup Intent first
	setupIntentParams := &stripe.SetupIntentCreateParams{
		Customer: stripe.String(stripeCustomerID),
		Usage:    stripe.String(usage),
		Metadata: metadata,
	}

	// Add payment method types if specified (if empty, Stripe will use all available types)
	if len(paymentMethodTypes) > 0 {
		for _, pmType := range paymentMethodTypes {
			setupIntentParams.PaymentMethodTypes = append(setupIntentParams.PaymentMethodTypes, stripe.String(pmType))
		}
	}

	setupIntent, err := stripeClient.V1SetupIntents.Create(ctx, setupIntentParams)
	if err != nil {
		s.logger.Errorw("failed to create Stripe setup intent",
			"error", err,
			"customer_id", customerID)
		return nil, ierr.NewError("failed to create setup intent").
			WithHint("Unable to create Stripe setup intent").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
				"error":       err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	s.logger.Infow("created stripe setup intent",
		"setup_intent_id", setupIntent.ID,
		"customer_id", customerID,
		"usage", usage,
	)

	// Create Checkout Session in setup mode
	checkoutParams := &stripe.CheckoutSessionCreateParams{
		Mode:       stripe.String("setup"),
		Customer:   stripe.String(stripeCustomerID),
		Currency:   stripe.String("usd"), // Required by Stripe even for setup mode
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		Metadata:   metadata,
	}

	// Add payment method types if specified (if empty, Stripe will use all available types)
	if len(paymentMethodTypes) > 0 {
		var pmTypes []*string
		for _, pmType := range paymentMethodTypes {
			pmTypes = append(pmTypes, stripe.String(pmType))
		}
		checkoutParams.PaymentMethodTypes = pmTypes
	}

	// Link the setup intent to the checkout session
	checkoutParams.SetupIntentData = &stripe.CheckoutSessionCreateSetupIntentDataParams{
		Metadata: metadata,
	}

	checkoutSession, err := stripeClient.V1CheckoutSessions.Create(ctx, checkoutParams)
	if err != nil {
		s.logger.Errorw("failed to create Stripe checkout session for setup intent",
			"error", err,
			"setup_intent_id", setupIntent.ID,
			"customer_id", customerID)
		return nil, ierr.NewError("failed to create setup intent checkout session").
			WithHint("Unable to create Stripe checkout session").
			WithReportableDetails(map[string]interface{}{
				"setup_intent_id": setupIntent.ID,
				"customer_id":     customerID,
				"error":           err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	response := &dto.SetupIntentResponse{
		SetupIntentID:     setupIntent.ID,
		CheckoutSessionID: checkoutSession.ID,
		CheckoutURL:       checkoutSession.URL,
		ClientSecret:      setupIntent.ClientSecret,
		Status:            string(setupIntent.Status),
		Usage:             usage,
		CustomerID:        customerID,
		CreatedAt:         setupIntent.Created,
		ExpiresAt:         checkoutSession.ExpiresAt,
	}

	s.logger.Infow("successfully created stripe setup intent with checkout session",
		"setup_intent_id", setupIntent.ID,
		"checkout_session_id", checkoutSession.ID,
		"checkout_url", checkoutSession.URL,
		"customer_id", customerID,
		"usage", usage,
	)

	return response, nil
}

// ListCustomerPaymentMethods lists only the successfully saved payment methods for a customer
func (s *PaymentService) ListCustomerPaymentMethods(ctx context.Context, customerID string, req *dto.ListPaymentMethodsRequest, customerService interfaces.CustomerService) (*dto.MultiProviderPaymentMethodsResponse, error) {
	response := &dto.MultiProviderPaymentMethodsResponse{}

	stripePaymentMethods, err := s.listStripeCustomerPaymentMethods(ctx, customerID, req, customerService)
	if err != nil {
		s.logger.Errorw("failed to get Stripe payment methods", "error", err, "customer_id", customerID)
		return nil, err
	}
	response.Stripe = stripePaymentMethods.Data

	return response, nil
}

// listStripeCustomerPaymentMethods lists only successfully saved payment methods using Stripe's Customer.ListPaymentMethods
func (s *PaymentService) listStripeCustomerPaymentMethods(ctx context.Context, customerID string, req *dto.ListPaymentMethodsRequest, customerService interfaces.CustomerService) (*dto.ListSetupIntentsResponse, error) {
	s.logger.Infow("listing stripe customer payment methods",
		"customer_id", customerID,
		"limit", req.Limit,
	)

	// Get Stripe client
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// Ensure customer is synced to Stripe
	customerResp, err := s.customerSvc.EnsureCustomerSyncedToStripe(ctx, customerID, customerService)
	if err != nil {
		s.logger.Errorw("failed to sync customer to Stripe",
			"error", err,
			"customer_id", customerID,
		)
		return nil, ierr.WithError(err).
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get Stripe customer ID
	stripeCustomerID, exists := customerResp.Customer.Metadata["stripe_customer_id"]
	if !exists || stripeCustomerID == "" {
		return nil, ierr.NewError("customer does not have Stripe customer ID after sync").
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get customer's default payment method ID
	var defaultPaymentMethodID string
	customer, err := stripeClient.V1Customers.Retrieve(ctx, stripeCustomerID, nil)
	if err == nil && customer.InvoiceSettings != nil && customer.InvoiceSettings.DefaultPaymentMethod != nil {
		defaultPaymentMethodID = customer.InvoiceSettings.DefaultPaymentMethod.ID
		s.logger.Infow("found default payment method for customer",
			"customer_id", customerID,
			"default_payment_method_id", defaultPaymentMethodID)
	}

	// Set default limit if not provided
	limit := req.Limit
	if limit <= 0 {
		limit = 10 // Default limit
	}
	if limit > 100 {
		limit = 100 // Max limit
	}

	// Build Stripe API parameters for listing payment methods
	params := &stripe.PaymentMethodListParams{
		Customer: stripe.String(stripeCustomerID),
		// No Type restriction - list ALL payment method types (card, us_bank_account, sepa_debit, etc.)
	}
	params.Limit = stripe.Int64(int64(limit))

	// Add pagination parameters
	if req.StartingAfter != "" {
		params.StartingAfter = stripe.String(req.StartingAfter)
	}
	if req.EndingBefore != "" {
		params.EndingBefore = stripe.String(req.EndingBefore)
	}

	// List Payment Methods from Stripe (only successfully saved ones)
	paymentMethodsList := stripeClient.V1PaymentMethods.List(ctx, params)

	var setupIntents []dto.SetupIntentListItem
	var hasMore bool
	var totalCount int

	// Iterate through the payment methods
	for pm, err := range paymentMethodsList {
		if err != nil {
			s.logger.Errorw("failed to list payment methods",
				"error", err,
				"customer_id", customerID,
				"stripe_customer_id", stripeCustomerID)
			return nil, ierr.NewError("failed to list payment methods").
				WithHint("Unable to retrieve payment methods from Stripe").
				WithReportableDetails(map[string]interface{}{
					"customer_id": customerID,
					"error":       err.Error(),
				}).
				Mark(ierr.ErrSystem)
		}

		// Convert to SetupIntentListItem format
		setupIntentItem := dto.SetupIntentListItem{
			ID:                   pm.ID,
			Status:               "succeeded",   // Payment methods are only returned if they're successfully saved
			Usage:                "off_session", // Default usage for saved payment methods
			CustomerID:           customerID,
			PaymentMethodID:      pm.ID,
			PaymentMethodDetails: s.convertPaymentMethodToResponse(pm),
			IsDefault:            pm.ID == defaultPaymentMethodID,
			CreatedAt:            pm.Created,
		}

		setupIntents = append(setupIntents, setupIntentItem)
		totalCount++
	}

	// Check if there are more results
	if len(setupIntents) == limit {
		hasMore = true
	}

	response := &dto.ListSetupIntentsResponse{
		Data:       setupIntents,
		HasMore:    hasMore,
		TotalCount: totalCount,
	}

	s.logger.Infow("successfully listed stripe customer payment methods",
		"customer_id", customerID,
		"stripe_customer_id", stripeCustomerID,
		"payment_methods_count", len(setupIntents),
		"has_more", hasMore,
	)

	return response, nil
}

// convertPaymentMethodToResponse converts a Stripe PaymentMethod to our PaymentMethodResponse format
func (s *PaymentService) convertPaymentMethodToResponse(pm *stripe.PaymentMethod) *dto.PaymentMethodResponse {
	response := &dto.PaymentMethodResponse{
		ID:       pm.ID,
		Type:     string(pm.Type),
		Customer: pm.Customer.ID,
		Created:  pm.Created,
		Metadata: make(map[string]interface{}),
	}

	// Convert metadata from map[string]string to map[string]interface{}
	for k, v := range pm.Metadata {
		response.Metadata[k] = v
	}

	if pm.Card != nil {
		response.Card = &dto.CardDetails{
			Brand:       string(pm.Card.Brand),
			Last4:       pm.Card.Last4,
			ExpMonth:    int(pm.Card.ExpMonth),
			ExpYear:     int(pm.Card.ExpYear),
			Fingerprint: pm.Card.Fingerprint,
		}
	}

	return response
}

// GetPaymentMethodDetails retrieves payment method details from Stripe
func (s *PaymentService) GetPaymentMethodDetails(ctx context.Context, paymentMethodID string) (*dto.PaymentMethodResponse, error) {
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	paymentMethod, err := stripeClient.V1PaymentMethods.Retrieve(ctx, paymentMethodID, nil)
	if err != nil {
		return nil, ierr.NewError("failed to retrieve payment method").
			WithHint("Unable to get payment method from Stripe").
			WithReportableDetails(map[string]interface{}{
				"payment_method_id": paymentMethodID,
				"error":             err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	return s.convertPaymentMethodToResponse(paymentMethod), nil
}

// HandleExternalStripePaymentFromWebhook handles external Stripe payment from webhook event
func (s *PaymentService) HandleExternalStripePaymentFromWebhook(ctx context.Context, paymentIntent *stripe.PaymentIntent, webhookRawData []byte, paymentService interfaces.PaymentService, invoiceService interfaces.InvoiceService) error {
	s.logger.Infow("no FlexPrice payment ID found, processing as external Stripe payment",
		"payment_intent_id", paymentIntent.ID,
		"metadata", paymentIntent.Metadata)

	// Check if invoice sync is enabled for this connection
	conn, err := s.client.connectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		s.logger.Errorw("failed to get connection for invoice sync check, skipping external payment",
			"error", err,
			"payment_intent_id", paymentIntent.ID)
		return nil
	}

	if !conn.IsInvoiceOutboundEnabled() {
		s.logger.Infow("invoice outbound sync disabled, skipping external payment",
			"payment_intent_id", paymentIntent.ID,
			"connection_id", conn.ID)
		return nil
	}

	// Parse the raw webhook data to get the invoice ID
	var webhookData struct {
		Invoice string `json:"invoice"`
	}
	err = json.Unmarshal(webhookRawData, &webhookData)
	if err != nil {
		s.logger.Errorw("failed to parse webhook data for invoice ID", "error", err)
		return ierr.WithError(err).Mark(ierr.ErrValidation)
	}

	stripeInvoiceID := webhookData.Invoice
	if stripeInvoiceID == "" {
		s.logger.Warnw("no Stripe invoice ID found in external payment",
			"payment_intent_id", paymentIntent.ID)
		return nil
	}

	s.logger.Infow("found Stripe invoice ID from webhook data",
		"payment_intent_id", paymentIntent.ID,
		"stripe_invoice_id", stripeInvoiceID)

	// Process external Stripe payment
	if err := s.ProcessExternalStripePayment(ctx, paymentIntent, stripeInvoiceID, paymentService, invoiceService); err != nil {
		s.logger.Errorw("failed to process external Stripe payment",
			"error", err,
			"payment_intent_id", paymentIntent.ID,
			"stripe_invoice_id", stripeInvoiceID)
		return ierr.WithError(err).
			WithHint("Failed to process external payment").
			Mark(ierr.ErrSystem)
	}

	s.logger.Infow("successfully processed external Stripe payment",
		"payment_intent_id", paymentIntent.ID,
		"stripe_invoice_id", stripeInvoiceID)
	return nil
}

// ProcessExternalStripePayment processes a payment that was made directly in Stripe (external to FlexPrice)
func (s *PaymentService) ProcessExternalStripePayment(ctx context.Context, paymentIntent *stripe.PaymentIntent, stripeInvoiceID string, paymentService interfaces.PaymentService, invoiceService interfaces.InvoiceService) error {
	// Get FlexPrice invoice ID from Stripe invoice
	flexpriceInvoiceID, err := s.getFlexPriceInvoiceID(ctx, stripeInvoiceID)
	if err != nil {
		s.logger.Errorw("failed to get FlexPrice invoice ID",
			"error", err,
			"stripe_invoice_id", stripeInvoiceID)
		return err
	}

	// Create external payment record
	err = s.createExternalPaymentRecord(ctx, paymentIntent, flexpriceInvoiceID, paymentService)
	if err != nil {
		s.logger.Errorw("failed to create external payment record",
			"error", err,
			"payment_intent_id", paymentIntent.ID)
		return err
	}

	// Reconcile invoice with external payment
	amount := decimal.NewFromInt(paymentIntent.Amount).Div(decimal.NewFromInt(100))
	err = s.reconcileInvoiceWithExternalPayment(ctx, flexpriceInvoiceID, amount, invoiceService)
	if err != nil {
		s.logger.Errorw("failed to reconcile invoice with external payment",
			"error", err,
			"invoice_id", flexpriceInvoiceID,
			"payment_amount", amount)
		return err
	}

	return nil
}

// getFlexPriceInvoiceID gets the FlexPrice invoice ID from a Stripe invoice ID
func (s *PaymentService) getFlexPriceInvoiceID(ctx context.Context, stripeInvoiceID string) (string, error) {
	return s.invoiceSyncSvc.GetFlexPriceInvoiceID(ctx, stripeInvoiceID)
}

// createExternalPaymentRecord creates a payment record for an external Stripe payment
func (s *PaymentService) createExternalPaymentRecord(ctx context.Context, paymentIntent *stripe.PaymentIntent, invoiceID string, paymentService interfaces.PaymentService) error {
	// Convert amount from cents to decimal
	amount := decimal.NewFromInt(paymentIntent.Amount).Div(decimal.NewFromInt(100))

	// Create payment record
	gatewayType := types.PaymentGatewayTypeStripe
	createReq := &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     invoiceID,
		PaymentMethodType: types.PaymentMethodTypeCard, // Mark as card payment
		Amount:            amount,
		Currency:          strings.ToUpper(string(paymentIntent.Currency)),
		PaymentGateway:    &gatewayType,
		ProcessPayment:    false, // Don't process - already succeeded in Stripe
		Metadata: types.Metadata{
			"payment_source":        "stripe_external",
			"stripe_payment_intent": paymentIntent.ID,
			"webhook_event_id":      paymentIntent.ID, // For idempotency
		},
	}

	// Add customer ID if available
	if paymentIntent.Customer != nil {
		createReq.Metadata["stripe_customer_id"] = paymentIntent.Customer.ID
	}

	// Add Stripe payment method ID (required for CARD payments)
	if paymentIntent.PaymentMethod != nil {
		createReq.PaymentMethodID = paymentIntent.PaymentMethod.ID
	}

	paymentResp, err := paymentService.CreatePayment(ctx, createReq)
	if err != nil {
		s.logger.Errorw("failed to create external payment record",
			"error", err,
			"payment_intent_id", paymentIntent.ID,
			"invoice_id", invoiceID)
		return err
	}

	// Update payment to succeeded status with all Stripe details (same as regular Stripe charge)
	now := time.Now().UTC()
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusSucceeded)),
		GatewayPaymentID: lo.ToPtr(paymentIntent.ID),
		PaymentGateway:   lo.ToPtr(string(types.PaymentGatewayTypeStripe)),
		SucceededAt:      lo.ToPtr(now),
	}

	// Add payment method ID if available
	if paymentIntent.PaymentMethod != nil {
		updateReq.PaymentMethodID = lo.ToPtr(paymentIntent.PaymentMethod.ID)
	}

	_, err = paymentService.UpdatePayment(ctx, paymentResp.ID, updateReq)
	if err != nil {
		s.logger.Errorw("failed to update external payment status",
			"error", err,
			"payment_id", paymentResp.ID,
			"payment_intent_id", paymentIntent.ID)
		return err
	}

	s.logger.Infow("successfully created external payment record",
		"payment_id", paymentResp.ID,
		"payment_intent_id", paymentIntent.ID,
		"invoice_id", invoiceID,
		"amount", amount)

	return nil
}

// reconcileInvoiceWithExternalPayment reconciles an invoice with an external payment
func (s *PaymentService) reconcileInvoiceWithExternalPayment(ctx context.Context, invoiceID string, paymentAmount decimal.Decimal, invoiceService interfaces.InvoiceService) error {
	// Get invoice to calculate new payment status
	invoiceResp, err := invoiceService.GetInvoice(ctx, invoiceID)
	if err != nil {
		s.logger.Errorw("failed to get invoice for external payment reconciliation",
			"error", err,
			"invoice_id", invoiceID)
		return err
	}

	// Calculate new amounts
	newAmountPaid := invoiceResp.AmountPaid.Add(paymentAmount)
	newAmountRemaining := invoiceResp.AmountDue.Sub(newAmountPaid)

	// Determine payment status
	var newPaymentStatus types.PaymentStatus
	if newAmountRemaining.IsZero() {
		newPaymentStatus = types.PaymentStatusSucceeded // Fully paid
	} else if newAmountRemaining.IsNegative() {
		newPaymentStatus = types.PaymentStatusOverpaid // Overpaid
	} else {
		newPaymentStatus = types.PaymentStatusPending // Partial payment
	}

	s.logger.Infow("calculated payment status for external payment",
		"invoice_id", invoiceID,
		"payment_amount", paymentAmount,
		"current_amount_paid", invoiceResp.AmountPaid,
		"new_amount_paid", newAmountPaid,
		"amount_due", invoiceResp.AmountDue,
		"new_amount_remaining", newAmountRemaining,
		"new_payment_status", newPaymentStatus)

	// Use the existing ReconcilePaymentStatus method with calculated status
	err = invoiceService.ReconcilePaymentStatus(ctx, invoiceID, newPaymentStatus, &paymentAmount)
	if err != nil {
		s.logger.Errorw("failed to update invoice payment status",
			"error", err,
			"invoice_id", invoiceID,
			"payment_amount", paymentAmount,
			"payment_status", newPaymentStatus)
		return err
	}

	s.logger.Infow("successfully reconciled invoice with external payment",
		"invoice_id", invoiceID,
		"payment_amount", paymentAmount,
		"payment_status", newPaymentStatus)

	return nil
}

// VerifyWebhookSignature verifies a Stripe webhook signature
func (s *PaymentService) VerifyWebhookSignature(payload []byte, signature string, webhookSecret string) error {
	_, err := webhook.ConstructEvent(payload, signature, webhookSecret)
	if err != nil {
		s.logger.Errorw("Stripe webhook verification failed", "error", err)
		return ierr.NewError("failed to verify webhook signature").
			WithHint("Invalid webhook signature or payload").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// HandleFlexPriceCheckoutPayment handles payment intents from FlexPrice checkout sessions
// paymentIntent is optional and can be nil
func (s *PaymentService) HandleFlexPriceCheckoutPayment(
	ctx context.Context,
	paymentIntent *stripe.PaymentIntent,
	payment *dto.PaymentResponse,
	customerService interfaces.CustomerService,
	invoiceService interfaces.InvoiceService,
	paymentService interfaces.PaymentService,
) error {
	s.logger.Infow("processing FlexPrice checkout payment",
		"flexprice_payment_id", payment.ID,
		"has_payment_intent", paymentIntent != nil)

	// Mark payment as succeeded
	paymentStatus := string(types.PaymentStatusSucceeded)

	// Update payment record
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: &paymentStatus,
	}

	// If payment intent exists, extract payment method and gateway payment ID
	if paymentIntent != nil {
		s.logger.Infow("processing with payment intent",
			"payment_intent_id", paymentIntent.ID,
			"amount", paymentIntent.Amount,
			"currency", paymentIntent.Currency)

		updateReq.GatewayPaymentID = &paymentIntent.ID

		// Extract payment method ID from payment intent
		if paymentIntent.PaymentMethod != nil {
			paymentMethodID := paymentIntent.PaymentMethod.ID
			updateReq.PaymentMethodID = &paymentMethodID

			s.logger.Infow("extracted payment method from payment intent",
				"payment_intent_id", paymentIntent.ID,
				"payment_method_id", paymentMethodID)

			// Set payment method as default if save_card_and_make_default was requested
			if payment.GatewayMetadata != nil {
				if saveCard, exists := payment.GatewayMetadata["save_card_and_make_default"]; exists && saveCard == "true" {
					// Get customer ID from invoice
					invoiceResp, err := invoiceService.GetInvoice(ctx, payment.DestinationID)
					if err != nil {
						s.logger.Errorw("failed to get invoice for customer ID",
							"error", err,
							"payment_id", payment.ID,
							"invoice_id", payment.DestinationID)
					} else {
						s.logger.Infow("setting payment method as default for customer",
							"payment_id", payment.ID,
							"customer_id", invoiceResp.CustomerID,
							"payment_method_id", paymentMethodID)

						err := s.SetDefaultPaymentMethod(ctx, invoiceResp.CustomerID, paymentMethodID, customerService)
						if err != nil {
							s.logger.Errorw("failed to set default payment method",
								"error", err,
								"payment_id", payment.ID,
								"customer_id", invoiceResp.CustomerID,
								"payment_method_id", paymentMethodID)
							// Don't fail the entire webhook processing
						} else {
							s.logger.Infow("successfully set default payment method",
								"payment_id", payment.ID,
								"customer_id", invoiceResp.CustomerID,
								"payment_method_id", paymentMethodID)
						}
					}
				}
			}
		}
	}

	_, err := paymentService.UpdatePayment(ctx, payment.ID, updateReq)
	if err != nil {
		s.logger.Errorw("failed to update payment record",
			"error", err,
			"payment_id", payment.ID,
			"new_status", paymentStatus)
		return ierr.WithError(err).
			WithHint("Failed to update payment record").
			Mark(ierr.ErrSystem)
	}

	s.logger.Infow("successfully updated payment record",
		"payment_id", payment.ID,
		"new_status", paymentStatus)

	// get the amount from the payment
	amount := payment.Amount
	err = s.ReconcilePaymentWithInvoice(ctx, payment.ID, amount, paymentService, invoiceService)
	if err != nil {
		s.logger.Errorw("failed to reconcile payment with invoice",
			"error", err,
			"payment_id", payment.ID,
			"amount", amount.String())
		// Don't fail the entire webhook processing
	} else {
		s.logger.Infow("successfully reconciled payment with invoice",
			"payment_id", payment.ID,
			"amount", amount.String())
	}

	// Only attach to Stripe invoice and reconcile if we have a payment intent
	if paymentIntent != nil {
		s.AttachPaymentToStripeInvoiceAndReconcile(ctx, payment, paymentIntent, paymentService, invoiceService)
	} else {
		s.logger.Infow("no payment intent available, skipping Stripe invoice attachment and reconciliation",
			"payment_id", payment.ID)
	}

	return nil
}

// AttachPaymentToStripeInvoiceAndReconcile attaches payment to Stripe invoice and reconciles with FlexPrice invoice
func (s *PaymentService) AttachPaymentToStripeInvoiceAndReconcile(
	ctx context.Context,
	payment *dto.PaymentResponse,
	paymentIntent *stripe.PaymentIntent,
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
) {
	// Find Stripe invoice ID using invoice sync service
	stripeInvoiceID, err := s.invoiceSyncSvc.GetStripeInvoiceID(ctx, payment.DestinationID)
	if err != nil {
		s.logger.Debugw("no Stripe invoice found for FlexPrice invoice, skipping attachment",
			"flexprice_invoice_id", payment.DestinationID,
			"payment_id", payment.ID,
			"error", err)
	}

	s.logger.Infow("attempting to attach payment to Stripe invoice",
		"payment_id", payment.ID,
		"payment_intent_id", paymentIntent.ID,
		"stripe_invoice_id", stripeInvoiceID)

	// Get Stripe client
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		s.logger.Errorw("failed to get Stripe client for invoice attachment",
			"error", err,
			"payment_id", payment.ID)
		return
	}

	if stripeInvoiceID != "" {
		err = s.AttachPaymentToStripeInvoice(ctx, stripeClient, paymentIntent.ID, stripeInvoiceID)
	} else {
		s.logger.Warnw("no Stripe invoice ID found, skipping Stripe invoice attachment",
			"payment_intent_id", paymentIntent.ID,
			"stripe_invoice_id", stripeInvoiceID)
		err = nil
	}

	if err != nil {
		s.logger.Errorw("failed to attach payment to Stripe invoice",
			"error", err,
			"payment_id", payment.ID,
			"payment_intent_id", paymentIntent.ID,
			"stripe_invoice_id", stripeInvoiceID)
		// Don't fail the entire webhook processing
	} else {
		s.logger.Infow("successfully attached payment to Stripe invoice",
			"payment_id", payment.ID,
			"payment_intent_id", paymentIntent.ID,
			"stripe_invoice_id", stripeInvoiceID)
	}
}
