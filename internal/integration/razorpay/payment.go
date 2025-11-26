package razorpay

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// PaymentService handles Razorpay payment operations
type PaymentService struct {
	client         RazorpayClient
	customerSvc    RazorpayCustomerService
	invoiceSyncSvc *InvoiceSyncService
	logger         *logger.Logger
}

// NewPaymentService creates a new Razorpay payment service
func NewPaymentService(
	client RazorpayClient,
	customerSvc RazorpayCustomerService,
	invoiceSyncSvc *InvoiceSyncService,
	logger *logger.Logger,
) *PaymentService {
	return &PaymentService{
		client:         client,
		customerSvc:    customerSvc,
		invoiceSyncSvc: invoiceSyncSvc,
		logger:         logger,
	}
}

// CreatePaymentLink creates a Razorpay payment link
func (s *PaymentService) CreatePaymentLink(ctx context.Context, req *CreatePaymentLinkRequest, customerService interfaces.CustomerService, invoiceService interfaces.InvoiceService) (*RazorpayPaymentLinkResponse, error) {
	s.logger.Infow("creating razorpay payment link",
		"invoice_id", req.InvoiceID,
		"customer_id", req.CustomerID,
		"amount", req.Amount.String(),
		"currency", req.Currency,
		"environment_id", req.EnvironmentID,
	)

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

	// Check if invoice is already synced to Razorpay
	// If yes, return the Razorpay invoice payment URL from entity mapping metadata
	if s.invoiceSyncSvc != nil {
		razorpayInvoiceMapping, err := s.invoiceSyncSvc.GetExistingRazorpayMapping(ctx, req.InvoiceID)
		if err == nil && razorpayInvoiceMapping != nil {
			// Check if payment URL is stored in metadata
			if paymentURL, ok := razorpayInvoiceMapping.Metadata["razorpay_payment_url"].(string); ok && paymentURL != "" {
				razorpayInvoiceID := razorpayInvoiceMapping.ProviderEntityID

				s.logger.Infow("invoice already synced to Razorpay, returning stored payment URL",
					"flexprice_invoice_id", req.InvoiceID,
					"razorpay_invoice_id", razorpayInvoiceID,
					"payment_url", paymentURL)

				// Return the Razorpay invoice payment URL
				// Payments made through this URL will automatically be associated with the Razorpay invoice
				return &RazorpayPaymentLinkResponse{
					ID:                    razorpayInvoiceID,
					PaymentURL:            paymentURL,
					Amount:                req.Amount,
					Currency:              req.Currency,
					Status:                "created",
					PaymentID:             req.PaymentID,
					IsRazorpayInvoiceLink: true,
				}, nil
			}

			// If no payment URL in metadata, log and continue to create separate payment link
			s.logger.Debugw("invoice synced to Razorpay but no payment URL found in metadata",
				"flexprice_invoice_id", req.InvoiceID,
				"razorpay_invoice_id", razorpayInvoiceMapping.ProviderEntityID)
		}
	}

	// Continue with creating separate payment link...
	// Ensure customer is synced to Razorpay before creating payment link
	flexpriceCustomer, err := s.customerSvc.EnsureCustomerSyncedToRazorpay(ctx, req.CustomerID, customerService)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to sync customer to Razorpay").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get Razorpay customer ID (should exist after sync)
	razorpayCustomerID, exists := flexpriceCustomer.Metadata["razorpay_customer_id"]
	if !exists || razorpayCustomerID == "" {
		return nil, ierr.NewError("customer does not have Razorpay customer ID after sync").
			WithHint("Failed to sync customer to Razorpay").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Convert amount to smallest currency unit (paise for INR, cents for USD, etc.)
	// Razorpay expects amounts in smallest currency unit
	amountInSmallestUnit := req.Amount.Mul(decimal.NewFromInt(100)).IntPart()

	// Build notes with metadata and line items
	notes := map[string]interface{}{
		"flexprice_invoice_id":     req.InvoiceID,
		"flexprice_customer_id":    req.CustomerID,
		"flexprice_payment_id":     req.PaymentID,
		"flexprice_environment_id": req.EnvironmentID,
		"payment_source":           "flexprice",
	}

	// Add all line items to notes with name as key and amount as value
	if len(invoiceResp.LineItems) > 0 {
		for i, item := range invoiceResp.LineItems {
			// Get display name with fallback
			itemName := fmt.Sprintf("Item %d", i+1)
			if item.DisplayName != nil && *item.DisplayName != "" {
				itemName = *item.DisplayName
			} else if item.PlanDisplayName != nil && *item.PlanDisplayName != "" {
				itemName = *item.PlanDisplayName
			}

			// Add line item with amount in the format "name: amount currency"
			notes[itemName] = fmt.Sprintf("%s %s", item.Amount.StringFixed(2), strings.ToUpper(item.Currency))
		}

		s.logger.Infow("added line items to notes",
			"invoice_id", req.InvoiceID,
			"line_items_count", len(invoiceResp.LineItems))
	}

	// Add custom metadata if provided
	for k, v := range req.Metadata {
		notes[k] = v
	}

	// Build a clean, concise description with customer name, plan name and invoice number
	// Format: "Customer Name - Plan Name - Invoice Number"
	var descriptionWithLineItems string

	// Get customer name
	customerName := flexpriceCustomer.Name

	// Get invoice number for reference
	invoiceNumber := DefaultInvoiceLabel
	if invoiceResp.InvoiceNumber != nil && *invoiceResp.InvoiceNumber != "" {
		invoiceNumber = *invoiceResp.InvoiceNumber
	}

	// Build description based on line items
	if len(invoiceResp.LineItems) > 0 {
		if len(invoiceResp.LineItems) == 1 {
			// Single item: "Customer Name - Plan Name - Invoice Number"
			item := invoiceResp.LineItems[0]
			itemName := DefaultItemName
			if item.DisplayName != nil && *item.DisplayName != "" {
				itemName = *item.DisplayName
			} else if item.PlanDisplayName != nil && *item.PlanDisplayName != "" {
				itemName = *item.PlanDisplayName
			}
			descriptionWithLineItems = fmt.Sprintf("%s | %s | %s", customerName, itemName, invoiceNumber)
		} else {
			// Multiple items: "Customer Name - Plan Name +X more - Invoice Number"
			item := invoiceResp.LineItems[0]
			itemName := DefaultItemName
			if item.DisplayName != nil && *item.DisplayName != "" {
				itemName = *item.DisplayName
			} else if item.PlanDisplayName != nil && *item.PlanDisplayName != "" {
				itemName = *item.PlanDisplayName
			}
			remainingCount := len(invoiceResp.LineItems) - 1
			descriptionWithLineItems = fmt.Sprintf("%s | %s +%d more | %s", customerName, itemName, remainingCount, invoiceNumber)
		}
	} else {
		// No line items, use customer name with generic payment label and invoice number
		descriptionWithLineItems = fmt.Sprintf("%s | Payment | %s", customerName, invoiceNumber)
	}

	s.logger.Infow("formatted payment description",
		"invoice_id", req.InvoiceID,
		"description", descriptionWithLineItems)

	// Build customer info object
	// Razorpay payment links require customer object with name, email, and optionally contact
	customerInfo := map[string]interface{}{
		"name": flexpriceCustomer.Name,
	}
	if flexpriceCustomer.Email != "" {
		customerInfo["email"] = flexpriceCustomer.Email
	}
	// Note: contact/phone not available in FlexPrice customer model

	// Prepare payment link data according to Razorpay API format
	// Following the exact format from Razorpay documentation
	paymentLinkData := map[string]interface{}{
		"amount":      amountInSmallestUnit,
		"currency":    strings.ToUpper(req.Currency),
		"description": descriptionWithLineItems,
		"customer":    customerInfo,
		"notify": map[string]interface{}{
			"sms":   true,
			"email": true,
		},
		"reminder_enable": true,
		"notes":           notes,
	}

	// Razorpay only supports a single callback_url (unlike Stripe's success_url and cancel_url)
	// The customer will be redirected to this URL after completing OR cancelling the payment
	// Use callback_method: "get" as required by Razorpay for payment links
	if req.SuccessURL != "" {
		paymentLinkData["callback_url"] = req.SuccessURL
		paymentLinkData["callback_method"] = "get" // Only "get" is supported by Razorpay payment links
		s.logger.Infow("callback URL configured for payment link",
			"invoice_id", req.InvoiceID,
			"callback_url", req.SuccessURL)
	} else {
		s.logger.Warnw("no callback URL provided - customer will not be redirected after payment",
			"invoice_id", req.InvoiceID)
	}
	// Note: CancelURL is not supported by Razorpay - callback_url is used for both success and cancel

	s.logger.Infow("creating payment link in Razorpay",
		"invoice_id", req.InvoiceID,
		"customer_id", req.CustomerID,
		"razorpay_customer_id", razorpayCustomerID,
		"amount", amountInSmallestUnit,
		"currency", req.Currency)

	// Create payment link in Razorpay using wrapper function
	razorpayPaymentLink, err := s.client.CreatePaymentLink(ctx, paymentLinkData)
	if err != nil {
		s.logger.Errorw("failed to create Razorpay payment link",
			"error", err,
			"invoice_id", req.InvoiceID)
		return nil, err
	}

	// Safely extract response fields with type assertions
	paymentLinkID, ok := razorpayPaymentLink["id"].(string)
	if !ok || paymentLinkID == "" {
		s.logger.Errorw("missing payment link id in Razorpay response",
			"invoice_id", req.InvoiceID)
		return nil, ierr.NewError("razorpay payment link id missing in response").
			WithHint("Check Razorpay CreatePaymentLink response payload").
			Mark(ierr.ErrSystem)
	}

	paymentLinkURL, ok := razorpayPaymentLink["short_url"].(string)
	if !ok || paymentLinkURL == "" {
		s.logger.Errorw("missing payment link URL in Razorpay response",
			"invoice_id", req.InvoiceID,
			"payment_link_id", paymentLinkID)
		return nil, ierr.NewError("razorpay payment link URL missing in response").
			WithHint("Check Razorpay CreatePaymentLink response payload").
			Mark(ierr.ErrSystem)
	}

	status, ok := razorpayPaymentLink["status"].(string)
	if !ok {
		// Default to "created" if status is missing
		status = "created"
		s.logger.Warnw("missing status in Razorpay payment link response, using default",
			"invoice_id", req.InvoiceID,
			"payment_link_id", paymentLinkID)
	}

	createdAtFloat, ok := razorpayPaymentLink["created_at"].(float64)
	var createdAt int64
	if ok {
		createdAt = int64(createdAtFloat)
	} else {
		// Fallback to current time if created_at is missing
		createdAt = time.Now().Unix()
		s.logger.Warnw("missing created_at in Razorpay payment link response, using current time",
			"invoice_id", req.InvoiceID,
			"payment_link_id", paymentLinkID)
	}

	response := &RazorpayPaymentLinkResponse{
		ID:         paymentLinkID,
		PaymentURL: paymentLinkURL,
		Amount:     req.Amount,
		Currency:   req.Currency,
		Status:     status,
		CreatedAt:  createdAt,
		PaymentID:  req.PaymentID,
	}

	s.logger.Infow("successfully created razorpay payment link",
		"payment_id", response.PaymentID,
		"payment_link_id", paymentLinkID,
		"payment_url", paymentLinkURL,
		"invoice_id", req.InvoiceID,
		"amount", req.Amount.String(),
		"currency", req.Currency,
	)

	return response, nil
}

// ReconcilePaymentWithInvoice updates the invoice payment status and amounts when a payment succeeds
func (s *PaymentService) ReconcilePaymentWithInvoice(ctx context.Context, paymentID string, paymentAmount decimal.Decimal, paymentService interfaces.PaymentService, invoiceService interfaces.InvoiceService) error {
	s.logger.Infow("starting payment reconciliation with invoice",
		"payment_id", paymentID,
		"payment_amount", paymentAmount.String())

	// Get the payment record
	payment, err := paymentService.GetPayment(ctx, paymentID)
	if err != nil {
		s.logger.Errorw("failed to get payment record for reconciliation",
			"error", err,
			"payment_id", paymentID)
		return err
	}

	// Reconcile the invoice
	return s.reconcileInvoice(ctx, payment.DestinationID, paymentAmount, invoiceService)
}

// reconcileInvoice is the shared logic for invoice reconciliation
func (s *PaymentService) reconcileInvoice(ctx context.Context, invoiceID string, paymentAmount decimal.Decimal, invoiceService interfaces.InvoiceService) error {
	// Get the invoice
	invoiceResp, err := invoiceService.GetInvoice(ctx, invoiceID)
	if err != nil {
		s.logger.Errorw("failed to get invoice for reconciliation",
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
		newPaymentStatus = types.PaymentStatusSucceeded
	} else if newAmountRemaining.IsNegative() {
		newPaymentStatus = types.PaymentStatusOverpaid
		newAmountRemaining = decimal.Zero
	} else {
		newPaymentStatus = types.PaymentStatusPending
	}

	s.logger.Infow("calculated new amounts for reconciliation",
		"invoice_id", invoiceID,
		"payment_amount", paymentAmount.String(),
		"new_amount_paid", newAmountPaid.String(),
		"new_amount_remaining", newAmountRemaining.String(),
		"new_payment_status", newPaymentStatus)

	// Update invoice
	err = invoiceService.ReconcilePaymentStatus(ctx, invoiceID, newPaymentStatus, &paymentAmount)
	if err != nil {
		s.logger.Errorw("failed to update invoice payment status",
			"error", err,
			"invoice_id", invoiceID)
		return err
	}

	s.logger.Infow("successfully reconciled invoice",
		"invoice_id", invoiceID,
		"payment_amount", paymentAmount.String(),
		"new_payment_status", newPaymentStatus)

	return nil
}

// HandleExternalRazorpayPaymentFromWebhook handles external Razorpay payment from webhook event
// This is called when a payment.captured webhook is received without a flexprice_payment_id
func (s *PaymentService) HandleExternalRazorpayPaymentFromWebhook(
	ctx context.Context,
	payment map[string]interface{},
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
) error {
	razorpayPaymentID := lo.FromPtrOr(extractStringFromMap(payment, "id"), "")
	razorpayInvoiceID := lo.FromPtrOr(extractStringFromMap(payment, "invoice_id"), "")

	s.logger.Infow("no FlexPrice payment ID found, processing as external Razorpay payment",
		"razorpay_payment_id", razorpayPaymentID,
		"razorpay_invoice_id", razorpayInvoiceID)

	// Check if invoice ID exists (payment must be linked to an invoice)
	if razorpayInvoiceID == "" {
		s.logger.Infow("no Razorpay invoice ID found in external payment, skipping",
			"razorpay_payment_id", razorpayPaymentID)
		return nil
	}

	// Check if invoice sync is enabled for this connection
	conn, err := s.client.GetConnection(ctx)
	if err != nil {
		s.logger.Errorw("failed to get connection for invoice sync check, skipping external payment",
			"error", err,
			"razorpay_payment_id", razorpayPaymentID)
		return nil
	}

	if !conn.IsInvoiceOutboundEnabled() {
		s.logger.Infow("invoice outbound sync disabled, skipping external payment",
			"razorpay_payment_id", razorpayPaymentID,
			"razorpay_invoice_id", razorpayInvoiceID,
			"connection_id", conn.ID)
		return nil
	}

	// Process external Razorpay payment
	if err := s.ProcessExternalRazorpayPayment(ctx, payment, razorpayInvoiceID, paymentService, invoiceService); err != nil {
		s.logger.Errorw("failed to process external Razorpay payment",
			"error", err,
			"razorpay_payment_id", razorpayPaymentID,
			"razorpay_invoice_id", razorpayInvoiceID)
		return ierr.WithError(err).
			WithHint("Failed to process external payment").
			Mark(ierr.ErrSystem)
	}

	s.logger.Infow("successfully processed external Razorpay payment",
		"razorpay_payment_id", razorpayPaymentID,
		"razorpay_invoice_id", razorpayInvoiceID)
	return nil
}

// ProcessExternalRazorpayPayment processes a payment that was made directly in Razorpay (external to FlexPrice)
func (s *PaymentService) ProcessExternalRazorpayPayment(
	ctx context.Context,
	payment map[string]interface{},
	razorpayInvoiceID string,
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
) error {
	razorpayPaymentID := lo.FromPtrOr(extractStringFromMap(payment, "id"), "")

	s.logger.Infow("processing external Razorpay payment",
		"razorpay_payment_id", razorpayPaymentID,
		"razorpay_invoice_id", razorpayInvoiceID)

	// Step 1: Check if payment already exists (idempotency check)
	exists, err := s.PaymentExistsByGatewayPaymentID(ctx, razorpayPaymentID, paymentService)
	if err != nil {
		s.logger.Errorw("failed to check if payment exists",
			"error", err,
			"razorpay_payment_id", razorpayPaymentID)
		// Continue processing on error
	} else if exists {
		s.logger.Infow("payment already exists for this Razorpay payment, skipping",
			"razorpay_payment_id", razorpayPaymentID,
			"razorpay_invoice_id", razorpayInvoiceID)
		return nil
	}

	// Step 2: Get FlexPrice invoice ID from Razorpay invoice

	flexpriceInvoiceID, err := s.invoiceSyncSvc.GetFlexPriceInvoiceID(ctx, razorpayInvoiceID)
	if err != nil {
		s.logger.Errorw("failed to get FlexPrice invoice ID",
			"error", err,
			"razorpay_invoice_id", razorpayInvoiceID)
		return err
	}

	s.logger.Infow("found FlexPrice invoice for external payment",
		"razorpay_payment_id", razorpayPaymentID,
		"razorpay_invoice_id", razorpayInvoiceID,
		"flexprice_invoice_id", flexpriceInvoiceID)

	// Step 3: Create external payment record
	err = s.createExternalPaymentRecord(ctx, payment, flexpriceInvoiceID, paymentService)
	if err != nil {
		s.logger.Errorw("failed to create external payment record",
			"error", err,
			"razorpay_payment_id", razorpayPaymentID)
		return err
	}

	// Step 4: Reconcile invoice with external payment
	amount := extractAmountFromPayment(payment)
	err = s.reconcileInvoice(ctx, flexpriceInvoiceID, amount, invoiceService)
	if err != nil {
		s.logger.Errorw("failed to reconcile invoice with external payment",
			"error", err,
			"invoice_id", flexpriceInvoiceID,
			"amount", amount.String())
		return err
	}

	s.logger.Infow("successfully processed external Razorpay payment",
		"razorpay_payment_id", razorpayPaymentID,
		"flexprice_invoice_id", flexpriceInvoiceID,
		"amount", amount.String())

	return nil
}

// createExternalPaymentRecord creates a payment record for an external Razorpay payment
func (s *PaymentService) createExternalPaymentRecord(
	ctx context.Context,
	payment map[string]interface{},
	invoiceID string,
	paymentService interfaces.PaymentService,
) error {
	razorpayPaymentID := lo.FromPtrOr(extractStringFromMap(payment, "id"), "")
	amount := extractAmountFromPayment(payment)
	currency := lo.FromPtrOr(extractStringFromMap(payment, "currency"), "INR")
	method := lo.FromPtrOr(extractStringFromMap(payment, "method"), "")
	email := lo.FromPtrOr(extractStringFromMap(payment, "email"), "")
	contact := lo.FromPtrOr(extractStringFromMap(payment, "contact"), "")

	s.logger.Infow("creating external payment record",
		"razorpay_payment_id", razorpayPaymentID,
		"invoice_id", invoiceID,
		"amount", amount.String(),
		"currency", currency,
		"method", method)

	// Extract payment method ID based on method type
	paymentMethodID := extractPaymentMethodID(payment, method)

	// Create payment record with all details in metadata (for traceability)
	now := time.Now().UTC()
	gatewayType := types.PaymentGatewayTypeRazorpay
	createReq := &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     invoiceID,
		PaymentMethodType: types.PaymentMethodTypeCard, // Default to card
		Amount:            amount,
		Currency:          strings.ToUpper(currency),
		PaymentGateway:    &gatewayType,
		ProcessPayment:    false, // Don't process - already succeeded in Razorpay
		PaymentMethodID:   paymentMethodID,
		Metadata: types.Metadata{
			"payment_source":      "razorpay_external",
			"razorpay_payment_id": razorpayPaymentID,
			"razorpay_method":     method,
			"webhook_event_id":    razorpayPaymentID, // For idempotency
			"succeeded_at":        now.Format(time.RFC3339),
			"customer_email":      email,
			"customer_contact":    contact,
		},
	}

	paymentResp, err := paymentService.CreatePayment(ctx, createReq)
	if err != nil {
		s.logger.Errorw("failed to create external payment record",
			"error", err,
			"razorpay_payment_id", razorpayPaymentID,
			"invoice_id", invoiceID)
		return err
	}

	// Update payment to succeeded status
	// Note: We need to update because CreatePaymentRequest doesn't support setting status directly
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusSucceeded)),
		GatewayPaymentID: lo.ToPtr(razorpayPaymentID),
		SucceededAt:      lo.ToPtr(now),
	}

	_, err = paymentService.UpdatePayment(ctx, paymentResp.ID, updateReq)
	if err != nil {
		s.logger.Errorw("failed to update external payment status",
			"error", err,
			"payment_id", paymentResp.ID,
			"razorpay_payment_id", razorpayPaymentID)
		return err
	}

	s.logger.Infow("successfully created external payment record",
		"payment_id", paymentResp.ID,
		"razorpay_payment_id", razorpayPaymentID,
		"invoice_id", invoiceID,
		"amount", amount.String())

	return nil
}

// PaymentExistsByGatewayPaymentID checks if a payment already exists with the given gateway payment ID
func (s *PaymentService) PaymentExistsByGatewayPaymentID(
	ctx context.Context,
	gatewayPaymentID string,
	paymentService interfaces.PaymentService,
) (bool, error) {
	if gatewayPaymentID == "" {
		return false, nil
	}

	// Create filter to query payments by gateway_payment_id
	filter := types.NewNoLimitPaymentFilter()
	limit := 1
	filter.QueryFilter.Limit = &limit
	filter.GatewayPaymentID = &gatewayPaymentID

	// Query payments
	payments, err := paymentService.ListPayments(ctx, filter)
	if err != nil {
		return false, err
	}

	// Return true if any payment exists with this gateway payment ID
	return len(payments.Items) > 0, nil
}

// extractStringFromMap safely extracts a string value from map
func extractStringFromMap(data map[string]interface{}, key string) *string {
	if val, ok := data[key].(string); ok {
		return &val
	}
	return nil
}

// extractAmountFromPayment extracts and converts amount from payment data
func extractAmountFromPayment(payment map[string]interface{}) decimal.Decimal {
	// Razorpay amount is in smallest currency unit (paise)
	if amountInt, ok := payment["amount"].(int64); ok {
		return decimal.NewFromInt(amountInt).Div(decimal.NewFromInt(100))
	}
	if amountFloat, ok := payment["amount"].(float64); ok {
		return decimal.NewFromFloat(amountFloat).Div(decimal.NewFromInt(100))
	}
	return decimal.Zero
}

// extractPaymentMethodID extracts payment method ID based on method type
func extractPaymentMethodID(payment map[string]interface{}, method string) string {
	switch method {
	case "card":
		if cardID, ok := payment["card_id"].(string); ok {
			return cardID
		}
	case "upi":
		if vpa, ok := payment["vpa"].(string); ok {
			return vpa
		}
	case "wallet":
		if wallet, ok := payment["wallet"].(string); ok {
			return wallet
		}
	case "netbanking":
		if bank, ok := payment["bank"].(string); ok {
			return bank
		}
	}
	return ""
}
