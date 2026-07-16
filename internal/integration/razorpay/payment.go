package razorpay

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// AutoChargeRequest is the input for PaymentService.AutoCharge.
type AutoChargeRequest struct {
	InvoiceID          string
	RazorpayCustomerID string
	TokenID            string          // Razorpay token ID (from GetCustomerTokens)
	Amount             decimal.Decimal // in major units (e.g. rupees)
	Currency           string          // ISO code, e.g. "INR"
	FlexPricePaymentID string          // FlexPrice payment.ID — embedded in Razorpay notes for webhook reconciliation
	Contact            string          // Customer contact number — required by Razorpay for recurring/token-based charges
	Email              string          // Customer email — sent alongside contact for Razorpay-side validation
}

// AutoChargeResult is the output of PaymentService.AutoCharge.
type AutoChargeResult struct {
	RazorpayPaymentID string // may be empty if the payment was already in-flight (AlreadySubmitted=true)
	RazorpayOrderID   string // Razorpay order_xxx — always populated when a new or existing order was resolved
	AlreadySubmitted  bool   // true = payment was previously submitted; webhook will reconcile
}

// PaymentService handles Razorpay payment operations
type PaymentService struct {
	client         RazorpayClient
	customerSvc    RazorpayCustomerService
	invoiceSyncSvc *InvoiceSyncService
	locker         cache.Locker
	logger         *logger.Logger
}

// NewPaymentService creates a Razorpay payment service. locker is required.
func NewPaymentService(
	client RazorpayClient,
	customerSvc RazorpayCustomerService,
	invoiceSyncSvc *InvoiceSyncService,
	locker cache.Locker,
	logger *logger.Logger,
) *PaymentService {
	return &PaymentService{
		client:         client,
		customerSvc:    customerSvc,
		invoiceSyncSvc: invoiceSyncSvc,
		locker:         locker,
		logger:         logger,
	}
}

// CreatePaymentLink creates a Razorpay payment link
func (s *PaymentService) CreatePaymentLink(ctx context.Context, req *CreatePaymentLinkRequest, customerService interfaces.CustomerService, invoiceService interfaces.InvoiceService) (*RazorpayPaymentLinkResponse, error) {
	s.logger.Info(ctx, "creating razorpay payment link",
		"invoice_id", req.InvoiceID,
		"customer_id", req.CustomerID,
		"amount", req.Amount.String(),
		"currency", req.Currency,
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

				s.logger.Info(ctx, "invoice already synced to Razorpay, returning stored payment URL",
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
			s.logger.Debug(ctx, "invoice synced to Razorpay but no payment URL found in metadata",
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
		"flexprice_invoice_id":  req.InvoiceID,
		"flexprice_customer_id": req.CustomerID,
		"flexprice_payment_id":  req.PaymentID,
		"payment_source":        "flexprice",
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

		s.logger.Info(ctx, "added line items to notes",
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

	s.logger.Info(ctx, "formatted payment description",
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
	if flexpriceCustomer.Contact != nil && *flexpriceCustomer.Contact != "" {
		customerInfo["contact"] = *flexpriceCustomer.Contact
	}

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
		s.logger.Info(ctx, "callback URL configured for payment link",
			"invoice_id", req.InvoiceID,
			"callback_url", req.SuccessURL)
	} else {
		s.logger.Info(ctx, "no callback URL provided - customer will not be redirected after payment",
			"invoice_id", req.InvoiceID)
	}
	// Note: CancelURL is not supported by Razorpay - callback_url is used for both success and cancel

	s.logger.Info(ctx, "creating payment link in Razorpay",
		"invoice_id", req.InvoiceID,
		"customer_id", req.CustomerID,
		"razorpay_customer_id", razorpayCustomerID,
		"amount", amountInSmallestUnit,
		"currency", req.Currency)

	// Create payment link in Razorpay using wrapper function
	razorpayPaymentLink, err := s.client.CreatePaymentLink(ctx, paymentLinkData)
	if err != nil {
		s.logger.Error(ctx, "failed to create Razorpay payment link",
			"error", err,
			"invoice_id", req.InvoiceID)
		return nil, err
	}

	// Safely extract response fields with type assertions
	paymentLinkID, ok := razorpayPaymentLink["id"].(string)
	if !ok || paymentLinkID == "" {
		s.logger.Error(ctx, "missing payment link id in Razorpay response",
			"error", err,
			"invoice_id", req.InvoiceID)
		return nil, ierr.NewError("razorpay payment link id missing in response").
			WithHint("Check Razorpay CreatePaymentLink response payload").
			Mark(ierr.ErrSystem)
	}

	paymentLinkURL, ok := razorpayPaymentLink["short_url"].(string)
	if !ok || paymentLinkURL == "" {
		s.logger.Error(ctx, "missing payment link URL in Razorpay response",
			"error", err,
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
		s.logger.Info(ctx, "missing status in Razorpay payment link response, using default",
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
		s.logger.Info(ctx, "missing created_at in Razorpay payment link response, using current time",
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

	s.logger.Info(ctx, "successfully created razorpay payment link",
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
	s.logger.Info(ctx, "starting payment reconciliation with invoice",
		"payment_id", paymentID,
		"payment_amount", paymentAmount.String())

	// Get the payment record
	payment, err := paymentService.GetPayment(ctx, paymentID)
	if err != nil {
		s.logger.Error(ctx, "failed to get payment record for reconciliation",
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
		s.logger.Error(ctx, "failed to get invoice for reconciliation",
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

	s.logger.Info(ctx, "calculated new amounts for reconciliation",
		"invoice_id", invoiceID,
		"payment_amount", paymentAmount.String(),
		"new_amount_paid", newAmountPaid.String(),
		"new_amount_remaining", newAmountRemaining.String(),
		"new_payment_status", newPaymentStatus)

	// Update invoice
	err = invoiceService.ReconcilePaymentStatus(ctx, invoiceID, newPaymentStatus, &paymentAmount)
	if err != nil {
		s.logger.Error(ctx, "failed to update invoice payment status",
			"error", err,
			"invoice_id", invoiceID)
		return err
	}

	s.logger.Info(ctx, "successfully reconciled invoice",
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
	description := lo.FromPtrOr(extractStringFromMap(payment, "description"), "")
	orderID := lo.FromPtrOr(extractStringFromMap(payment, "order_id"), "")

	s.logger.Info(ctx, "no FlexPrice payment ID found, processing as external Razorpay payment",
		"razorpay_payment_id", razorpayPaymentID,
		"razorpay_invoice_id", razorpayInvoiceID,
		"order_id", orderID,
		"description", description,
	)

	// When payment goes through a Razorpay order (e.g. paid via invoice short-link),
	// Razorpay does NOT populate payment.invoice_id — it only puts the Razorpay invoice ID
	// in the description as "Invoice #inv_xxx". Extract it as a fallback.
	if razorpayInvoiceID == "" && strings.HasPrefix(description, "Invoice #") {
		razorpayInvoiceID = strings.TrimPrefix(description, "Invoice #")
		s.logger.Info(ctx, "extracted Razorpay invoice ID from payment description",
			"razorpay_payment_id", razorpayPaymentID,
			"razorpay_invoice_id", razorpayInvoiceID,
			"order_id", orderID,
		)
	}

	// Check if invoice ID exists (payment must be linked to an invoice)
	if razorpayInvoiceID == "" {
		s.logger.Info(ctx, "no Razorpay invoice ID found in external payment, skipping",
			"razorpay_payment_id", razorpayPaymentID,
			"order_id", orderID,
			"description", description)
		return nil
	}

	// Check if invoice sync is enabled for this connection
	conn, err := s.client.GetConnection(ctx)
	if err != nil {
		s.logger.Error(ctx, "failed to get connection for invoice sync check, skipping external payment",
			"error", err,
			"razorpay_payment_id", razorpayPaymentID)
		return nil
	}

	if !conn.IsInvoiceOutboundEnabled() {
		s.logger.Info(ctx, "invoice outbound sync disabled, skipping external payment",
			"razorpay_payment_id", razorpayPaymentID,
			"razorpay_invoice_id", razorpayInvoiceID,
			"connection_id", conn.ID)
		return nil
	}

	// Process external Razorpay payment
	if err := s.ProcessExternalRazorpayPayment(ctx, payment, razorpayInvoiceID, paymentService, invoiceService); err != nil {
		s.logger.Error(ctx, "failed to process external Razorpay payment",
			"error", err,
			"razorpay_payment_id", razorpayPaymentID,
			"razorpay_invoice_id", razorpayInvoiceID)
		return ierr.WithError(err).
			WithHint("Failed to process external payment").
			Mark(ierr.ErrSystem)
	}

	s.logger.Info(ctx, "successfully processed external Razorpay payment",
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

	s.logger.Info(ctx, "processing external Razorpay payment",
		"razorpay_payment_id", razorpayPaymentID,
		"razorpay_invoice_id", razorpayInvoiceID)

	// Step 1: Check if payment already exists (idempotency check)
	exists, err := s.PaymentExistsByGatewayPaymentID(ctx, razorpayPaymentID, paymentService)
	if err != nil {
		s.logger.Error(ctx, "failed to check if payment exists",
			"error", err,
			"razorpay_payment_id", razorpayPaymentID)
		// Continue processing on error
	} else if exists {
		s.logger.Info(ctx, "payment already exists for this Razorpay payment, skipping",
			"razorpay_payment_id", razorpayPaymentID,
			"razorpay_invoice_id", razorpayInvoiceID)
		return nil
	}

	// Step 2: Get FlexPrice invoice ID from Razorpay invoice

	flexpriceInvoiceID, err := s.invoiceSyncSvc.GetFlexPriceInvoiceID(ctx, razorpayInvoiceID)
	if err != nil {
		s.logger.Error(ctx, "failed to get FlexPrice invoice ID",
			"error", err,
			"razorpay_invoice_id", razorpayInvoiceID)
		return err
	}

	s.logger.Info(ctx, "found FlexPrice invoice for external payment",
		"razorpay_payment_id", razorpayPaymentID,
		"razorpay_invoice_id", razorpayInvoiceID,
		"flexprice_invoice_id", flexpriceInvoiceID)

	// Step 3: Create external payment record
	err = s.createExternalPaymentRecord(ctx, payment, flexpriceInvoiceID, paymentService)
	if err != nil {
		s.logger.Error(ctx, "failed to create external payment record",
			"error", err,
			"razorpay_payment_id", razorpayPaymentID)
		return err
	}

	// Step 4: Reconcile invoice with external payment
	amount := extractAmountFromPayment(payment)
	err = s.reconcileInvoice(ctx, flexpriceInvoiceID, amount, invoiceService)
	if err != nil {
		s.logger.Error(ctx, "failed to reconcile invoice with external payment",
			"error", err,
			"invoice_id", flexpriceInvoiceID,
			"amount", amount.String())
		return err
	}

	s.logger.Info(ctx, "successfully processed external Razorpay payment",
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

	s.logger.Info(ctx, "creating external payment record",
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
		s.logger.Error(ctx, "failed to create external payment record",
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
		s.logger.Error(ctx, "failed to update external payment status",
			"error", err,
			"payment_id", paymentResp.ID,
			"razorpay_payment_id", razorpayPaymentID)
		return err
	}

	s.logger.Info(ctx, "successfully created external payment record",
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

// AutoCharge submits a server-initiated (off-session) recurring charge against an
// existing Razorpay UPI mandate token. It uses receipt=invoiceID as a Razorpay-native
// idempotency key so retries are safe.
//
// Returns AlreadySubmitted=true when a prior attempt already reached Razorpay — in
// that case the webhook (payment.captured / payment.failed) will reconcile state.
func (s *PaymentService) AutoCharge(ctx context.Context, req AutoChargeRequest) (*AutoChargeResult, error) {
	s.logger.Info(ctx, "initiating Razorpay auto-charge",
		"invoice_id", req.InvoiceID,
		"razorpay_customer_id", req.RazorpayCustomerID,
		"amount", req.Amount.String(),
		"currency", req.Currency)

	amountPaise := toPaise(req.Amount)

	orderData := map[string]interface{}{
		"amount":          amountPaise,
		"currency":        strings.ToUpper(req.Currency),
		"payment_capture": true,
		"receipt":         req.InvoiceID, // Razorpay-native idempotency key
		"notes": map[string]interface{}{
			"flexprice_invoice_id": req.InvoiceID,
			"flexprice_payment_id": req.FlexPricePaymentID,
		},
	}

	order, err := s.client.CreateOrder(ctx, orderData)
	if err != nil {
		// Razorpay returns BAD_REQUEST_ERROR when an order with the same receipt already exists.
		if isReceiptDuplicateError(err) {
			return s.handleExistingOrder(ctx, req)
		}
		return nil, err
	}

	orderID, ok := order["id"].(string)
	if !ok || orderID == "" {
		s.logger.Error(ctx, "Razorpay CreateOrder response missing order ID",
			"error", err,
			"invoice_id", req.InvoiceID)
		return nil, ierr.NewError("razorpay order ID missing in response").Mark(ierr.ErrInternal)
	}
	return s.submitRecurringPayment(ctx, req, orderID)
}

// isReceiptDuplicateError reports whether err is Razorpay's "duplicate receipt" error
// ("An order with the same receipt value has already been created").
// We match a narrow phrase to avoid false-positives on other receipt-related messages.
func isReceiptDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "same receipt") || strings.Contains(msg, "receipt already")
}

// handleExistingOrder fetches the existing Razorpay order (matched by receipt) and
// decides how to proceed based on its status.
func (s *PaymentService) handleExistingOrder(ctx context.Context, req AutoChargeRequest) (*AutoChargeResult, error) {
	order, err := s.client.FetchOrdersByReceipt(ctx, req.InvoiceID)
	if err != nil {
		return nil, err
	}

	status, _ := order["status"].(string)
	orderID, ok := order["id"].(string)
	if !ok || orderID == "" {
		return nil, ierr.NewError("Razorpay order missing ID in FetchOrdersByReceipt response").Mark(ierr.ErrInternal)
	}

	switch status {
	case "created":
		// Order exists but payment was never submitted — submit now using the same order.
		return s.submitRecurringPayment(ctx, req, orderID)
	case "attempted", "paid":
		// Payment was already submitted or fully captured. Return AlreadySubmitted so the
		// caller marks the FlexPrice payment as PROCESSING and lets the webhook reconcile.
		s.logger.Info(ctx, "razorpay order already attempted/paid, returning AlreadySubmitted",
			"invoice_id", req.InvoiceID,
			"order_id", orderID,
			"status", status)
		return &AutoChargeResult{
			AlreadySubmitted: true,
			RazorpayOrderID:  orderID,
		}, nil
	default:
		return nil, ierr.NewErrorf("unexpected Razorpay order status %q for receipt %s", status, req.InvoiceID).
			Mark(ierr.ErrInternal)
	}
}

// submitRecurringPayment calls Razorpay's recurring payment API against orderID.
func (s *PaymentService) submitRecurringPayment(ctx context.Context, req AutoChargeRequest, orderID string) (*AutoChargeResult, error) {
	amountPaise := toPaise(req.Amount)

	paymentData := map[string]interface{}{
		"amount":      amountPaise,
		"currency":    strings.ToUpper(req.Currency),
		"order_id":    orderID,
		"customer_id": req.RazorpayCustomerID,
		"token":       req.TokenID,
		"recurring":   true,
		"description": "Auto-charge for invoice " + req.InvoiceID,
		"notes": map[string]interface{}{
			"flexprice_invoice_id": req.InvoiceID,
			"flexprice_payment_id": req.FlexPricePaymentID,
		},
	}
	// Razorpay requires a contact number for recurring/token-based charges, and
	// validates it (plus email, when present) against the stored customer_id/token.
	if req.Contact != "" {
		paymentData["contact"] = req.Contact
	}
	if req.Email != "" {
		paymentData["email"] = req.Email
	}

	payment, err := s.client.CreateRecurringPayment(ctx, paymentData)
	if err != nil {
		// If Razorpay rejects because the order is already being processed, treat as AlreadySubmitted.
		if isOrderAlreadyProcessingError(err) {
			s.logger.Info(ctx, "razorpay order already being processed, returning AlreadySubmitted",
				"invoice_id", req.InvoiceID,
				"order_id", orderID)
			return &AutoChargeResult{AlreadySubmitted: true}, nil
		}
		return nil, err
	}

	razorpayPaymentID, _ := payment["id"].(string)
	s.logger.Info(ctx, "recurring payment submitted to Razorpay",
		"invoice_id", req.InvoiceID,
		"order_id", orderID,
		"razorpay_payment_id", razorpayPaymentID)
	return &AutoChargeResult{
		RazorpayPaymentID: razorpayPaymentID,
		RazorpayOrderID:   orderID,
	}, nil
}

// isOrderAlreadyProcessingError reports whether the Razorpay error indicates the order
// already has a payment in progress ("Order already has a payment").
// We match the specific Razorpay phrase to avoid false-positives on generic
// "order ... payment" text (e.g. "payment failed for the order").
func isOrderAlreadyProcessingError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "order already")
}

const refundLockTTL = 15 * time.Minute

// Full refund when payment lands after checkout expired or failed.
func (s *PaymentService) RefundLateCapturedPayment(
	ctx context.Context,
	flexpricePaymentID string,
	razorpayPaymentID string,
	paymentService interfaces.PaymentService,
) error {
	lockKey := cache.GenerateKey(ctx, cache.PrefixRazorpayWebhookRefundLock, flexpricePaymentID)
	lock, err := s.locker.AcquireLock(ctx, lockKey, refundLockTTL)
	if err != nil {
		return ierr.WithError(err).
			WithMessage("failed to acquire refund lock").
			WithReportableDetails(map[string]interface{}{"payment_id": flexpricePaymentID}).
			Mark(ierr.ErrInternal)
	}
	if !lock.AcquiredSuccessfully() {
		s.logger.Info(ctx, "refund already in progress for this payment, skipping", "payment_id", flexpricePaymentID)
		return nil
	}
	defer func() {
		if releaseErr := lock.Release(ctx); releaseErr != nil {
			s.logger.Error(ctx, "failed to release refund lock", "payment_id", flexpricePaymentID, "error", releaseErr)
		}
	}()

	existingPayment, err := paymentService.GetPayment(ctx, flexpricePaymentID)
	if err != nil {
		return ierr.WithError(err).
			WithMessage("failed to get payment record for refund").
			WithReportableDetails(map[string]interface{}{"payment_id": flexpricePaymentID}).
			Mark(ierr.ErrInternal)
	}
	if existingPayment.PaymentStatus == types.PaymentStatusRefunded || existingPayment.PaymentStatus == types.PaymentStatusPartiallyRefunded {
		s.logger.Info(ctx, "payment already refunded, skipping", "payment_id", flexpricePaymentID, "status", existingPayment.PaymentStatus)
		return nil
	}

	refundID, err := s.ensureRefunded(ctx, razorpayPaymentID, toPaise(existingPayment.Amount))
	if err != nil {
		return ierr.WithError(err).
			WithMessage("failed to refund late-captured payment at Razorpay").
			WithReportableDetails(map[string]interface{}{
				"payment_id":          flexpricePaymentID,
				"razorpay_payment_id": razorpayPaymentID,
			}).
			Mark(ierr.ErrInternal)
	}

	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusRefunded)),
		RefundedAt:       lo.ToPtr(time.Now()),
		GatewayPaymentID: lo.ToPtr(razorpayPaymentID),
	}
	if refundID != "" {
		metadata := lo.Assign(existingPayment.Metadata, types.Metadata{"razorpay_refund_id": refundID})
		updateReq.Metadata = &metadata
	}
	if _, err := paymentService.UpdatePayment(ctx, flexpricePaymentID, updateReq); err != nil {
		return ierr.WithError(err).
			WithMessage("refund confirmed at Razorpay but failed to update FlexPrice payment status").
			WithReportableDetails(map[string]interface{}{
				"payment_id":         flexpricePaymentID,
				"razorpay_refund_id": refundID,
			}).
			Mark(ierr.ErrInternal)
	}

	return nil
}

// Skip if Razorpay already shows this payment as fully refunded. Razorpay's Payment
// entity has no boolean "refunded" field — refund state is reported via
// "refund_status" (null/"partial"/"full").
func (s *PaymentService) ensureRefunded(ctx context.Context, razorpayPaymentID string, amountPaise int64) (string, error) {
	if current, err := s.client.FetchPayment(ctx, razorpayPaymentID); err != nil {
		s.logger.Info(ctx, "failed to check current Razorpay refund status before submitting, proceeding anyway",
			"razorpay_payment_id", razorpayPaymentID, "error", err)
	} else if refundStatus, _ := current["refund_status"].(string); refundStatus == "full" {
		s.logger.Info(ctx, "payment already fully refunded at Razorpay, skipping duplicate submission",
			"razorpay_payment_id", razorpayPaymentID)
		return "", nil
	}

	refundResp, err := s.client.RefundPayment(ctx, razorpayPaymentID, amountPaise)
	if err != nil {
		return "", err
	}
	refundID, _ := refundResp["id"].(string)
	return refundID, nil
}

// GetPaymentStatus fetches the current status of a Razorpay payment.
// Returns the raw Razorpay status string: "created", "authorized", "captured", "refunded", "failed".
func (s *PaymentService) GetPaymentStatus(ctx context.Context, razorpayPaymentID string) (string, error) {
	if razorpayPaymentID == "" {
		return "", ierr.NewError("razorpay_payment_id is required").Mark(ierr.ErrValidation)
	}
	result, err := s.client.FetchPayment(ctx, razorpayPaymentID)
	if err != nil {
		s.logger.Error(ctx, "failed to fetch Razorpay payment status",
			"razorpay_payment_id", razorpayPaymentID,
			"error", err)
		return "", err
	}
	status, _ := result["status"].(string)
	return status, nil
}
