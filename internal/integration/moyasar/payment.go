package moyasar

import (
	"context"
	"fmt"
	"strings"
	"time"

	apidto "github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// PaymentService handles Moyasar payment operations
type PaymentService struct {
	client         MoyasarClient
	customerSvc    MoyasarCustomerService
	invoiceSyncSvc *InvoiceSyncService
	logger         *logger.Logger
}

// NewPaymentService creates a new Moyasar payment service
func NewPaymentService(
	client MoyasarClient,
	customerSvc MoyasarCustomerService,
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

// CreatePaymentLink creates a payment link in Moyasar
// This creates a payment with a callback URL that acts as a hosted payment page
func (s *PaymentService) CreatePaymentLink(
	ctx context.Context,
	req CreatePaymentLinkRequest,
	customerService interfaces.CustomerService,
	invoiceService interfaces.InvoiceService,
) (*CreatePaymentLinkResponse, error) {
	// Validate request
	if req.InvoiceID == "" {
		return nil, ierr.NewError("invoice_id is required").Mark(ierr.ErrValidation)
	}
	if req.Amount.IsZero() || req.Amount.IsNegative() {
		return nil, ierr.NewError("amount must be positive").Mark(ierr.ErrValidation)
	}

	// Default currency to SAR if not provided
	currency := strings.ToUpper(req.Currency)
	if currency == "" {
		currency = DefaultCurrency
	}

	// Convert amount to smallest currency unit (halalah for SAR)
	// 1 SAR = 100 halalah
	// Round to nearest integer to avoid truncation errors
	amountInSmallestUnit := req.Amount.Mul(decimal.NewFromInt(100)).Round(0).IntPart()

	// Determine callback URL
	callbackURL := req.SuccessURL
	if callbackURL == "" {
		callbackURL = req.CancelURL
	}

	// Build metadata
	metadata := make(map[string]string)
	if req.Metadata != nil {
		for k, v := range req.Metadata {
			metadata[k] = v
		}
	}
	// Add Flexprice payment ID for webhook reconciliation
	if req.PaymentID != "" {
		metadata["flexprice_payment_id"] = req.PaymentID
	}
	if req.InvoiceID != "" {
		metadata["flexprice_invoice_id"] = req.InvoiceID
	}
	if req.CustomerID != "" {
		metadata["flexprice_customer_id"] = req.CustomerID
	}
	if req.EnvironmentID != "" {
		metadata["flexprice_environment_id"] = req.EnvironmentID
	}

	// Build description
	description := req.Description
	if description == "" {
		description = fmt.Sprintf("%s: %s", DefaultInvoiceLabel, req.InvoiceID)
	}

	// Create payment request
	paymentReq := &CreatePaymentRequest{
		Amount:      int(amountInSmallestUnit),
		Currency:    currency,
		Description: description,
		CallbackURL: callbackURL,
		Metadata:    metadata,
		GivenID:     req.PaymentID, // Use Flexprice payment ID for idempotency
	}

	s.logger.Info(ctx, "creating Moyasar payment link",
		"invoice_id", req.InvoiceID,
		"customer_id", req.CustomerID,
		"amount", req.Amount.String(),
		"currency", currency,
		"callback_url", callbackURL)

	// Create payment in Moyasar
	payment, err := s.client.CreatePayment(ctx, paymentReq)
	if err != nil {
		s.logger.Error(ctx, "failed to create Moyasar payment",
			"invoice_id", req.InvoiceID,
			"error", err)
		return nil, err
	}

	// Build response
	response := &CreatePaymentLinkResponse{
		ID:         payment.ID,
		PaymentURL: payment.TransactionURL,
		Amount:     req.Amount,
		Currency:   currency,
		Status:     payment.Status,
		CreatedAt:  payment.CreatedAt,
		PaymentID:  req.PaymentID,
	}

	s.logger.Info(ctx, "successfully created Moyasar payment link",
		"moyasar_payment_id", payment.ID,
		"flexprice_payment_id", req.PaymentID,
		"status", payment.Status,
		"payment_url_present", payment.TransactionURL != "")

	return response, nil
}

// ReconcilePaymentWithInvoice updates the invoice payment status and amounts when a payment succeeds
func (s *PaymentService) ReconcilePaymentWithInvoice(
	ctx context.Context,
	paymentID string,
	paymentAmount decimal.Decimal,
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
) error {
	// Get the payment record to find the invoice
	paymentRecord, err := paymentService.GetPayment(ctx, paymentID)
	if err != nil {
		s.logger.Error(ctx, "failed to get payment record for reconciliation",
			"payment_id", paymentID,
			"error", err)
		return err
	}

	if paymentRecord == nil {
		s.logger.Error(ctx, "payment record not found for reconciliation",
			"payment_id", paymentID)
		return ierr.NewError("payment not found").Mark(ierr.ErrNotFound)
	}

	if paymentRecord.DestinationType != types.PaymentDestinationTypeInvoice {
		s.logger.Warn(ctx, "payment destination is not an invoice, skipping reconciliation",
			"payment_id", paymentID,
			"destination_type", paymentRecord.DestinationType)
		return nil
	}

	invoiceID := paymentRecord.DestinationID
	if invoiceID == "" {
		s.logger.Warn(ctx, "payment has no invoice destination_id, skipping reconciliation",
			"payment_id", paymentID)
		return nil
	}

	// Update invoice payment status
	err = invoiceService.ReconcilePaymentStatus(ctx, invoiceID, types.PaymentStatusSucceeded, &paymentAmount)
	if err != nil {
		s.logger.Error(ctx, "failed to reconcile invoice payment status",
			"payment_id", paymentID,
			"invoice_id", invoiceID,
			"amount", paymentAmount.String(),
			"error", err)
		return err
	}

	s.logger.Info(ctx, "successfully reconciled payment with invoice",
		"payment_id", paymentID,
		"invoice_id", invoiceID,
		"amount", paymentAmount.String())

	return nil
}

// PaymentExistsByGatewayPaymentID checks if a payment already exists with the given gateway payment ID
func (s *PaymentService) PaymentExistsByGatewayPaymentID(
	ctx context.Context,
	gatewayPaymentID string,
	paymentService interfaces.PaymentService,
) (bool, error) {
	exists, err := paymentService.PaymentExistsByGatewayPaymentID(ctx, gatewayPaymentID)
	if err != nil {
		s.logger.Error(ctx, "failed to check if payment exists",
			"gateway_payment_id", gatewayPaymentID,
			"error", err)
		return false, err
	}
	return exists, nil
}

// GetPaymentStatus gets the payment status from Moyasar
func (s *PaymentService) GetPaymentStatus(
	ctx context.Context,
	moyasarPaymentID string,
) (*PaymentStatusResponse, error) {
	if moyasarPaymentID == "" {
		return nil, ierr.NewError("moyasar_payment_id is required").Mark(ierr.ErrValidation)
	}

	s.logger.Debug(ctx, "getting payment status from Moyasar",
		"moyasar_payment_id", moyasarPaymentID)

	payment, err := s.client.GetPayment(ctx, moyasarPaymentID)
	if err != nil {
		s.logger.Error(ctx, "failed to get payment from Moyasar",
			"moyasar_payment_id", moyasarPaymentID,
			"error", err)
		return nil, err
	}

	// Convert amount from smallest currency unit to standard unit using currency-aware conversion
	amount := convertFromSmallestUnit(int64(payment.Amount), payment.Currency)

	response := &PaymentStatusResponse{
		ID:          payment.ID,
		Status:      payment.Status,
		Amount:      amount,
		Currency:    payment.Currency,
		Description: payment.Description,
		CreatedAt:   payment.CreatedAt,
		UpdatedAt:   payment.UpdatedAt,
	}

	// Extract source information if available
	if payment.Source != nil {
		response.PaymentMethod = payment.Source.Type
		response.PaymentMethodID = payment.Source.GatewayID
		if response.PaymentMethodID == "" {
			response.PaymentMethodID = payment.Source.ReferenceID
		}
	}

	// Extract Flexprice payment ID from metadata if available
	if payment.Metadata != nil {
		if fpPaymentID, ok := payment.Metadata["flexprice_payment_id"]; ok {
			response.FlexpricePaymentID = fpPaymentID
		}
	}

	s.logger.Info(ctx, "retrieved payment status from Moyasar",
		"moyasar_payment_id", moyasarPaymentID,
		"status", payment.Status,
		"amount", amount.String())

	return response, nil
}

// HandleExternalMoyasarPaymentFromWebhook handles external Moyasar payment from webhook event
// This is called when a payment_paid webhook is received without a flexprice_payment_id
func (s *PaymentService) HandleExternalMoyasarPaymentFromWebhook(
	ctx context.Context,
	payment *MoyasarPaymentObject,
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
) error {
	moyasarPaymentID := payment.ID

	s.logger.Info(ctx, "no Flexprice payment ID found, processing as external Moyasar payment",
		"moyasar_payment_id", moyasarPaymentID)

	// Check if invoice ID exists in metadata
	flexpriceInvoiceID := ""
	if payment.Metadata != nil {
		flexpriceInvoiceID = payment.Metadata["flexprice_invoice_id"]
	}

	if flexpriceInvoiceID == "" {
		s.logger.Warn(ctx, "no flexprice_invoice_id in external payment metadata, skipping",
			"moyasar_payment_id", moyasarPaymentID)
		return nil
	}

	// Process external Moyasar payment
	if err := s.ProcessExternalMoyasarPayment(ctx, payment, flexpriceInvoiceID, paymentService, invoiceService); err != nil {
		s.logger.Error(ctx, "failed to process external Moyasar payment",
			"error", err,
			"moyasar_payment_id", moyasarPaymentID,
			"flexprice_invoice_id", flexpriceInvoiceID)
		return ierr.WithError(err).
			WithHint("Failed to process external payment").
			Mark(ierr.ErrSystem)
	}

	s.logger.Info(ctx, "successfully processed external Moyasar payment",
		"moyasar_payment_id", moyasarPaymentID,
		"flexprice_invoice_id", flexpriceInvoiceID)
	return nil
}

// ProcessExternalMoyasarPayment processes a payment that was made directly in Moyasar (external to Flexprice)
func (s *PaymentService) ProcessExternalMoyasarPayment(
	ctx context.Context,
	payment *MoyasarPaymentObject,
	flexpriceInvoiceID string,
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
) error {
	moyasarPaymentID := payment.ID

	s.logger.Info(ctx, "processing external Moyasar payment",
		"moyasar_payment_id", moyasarPaymentID,
		"flexprice_invoice_id", flexpriceInvoiceID)

	// Step 1: Check if payment already exists (idempotency check)
	exists, err := s.PaymentExistsByGatewayPaymentID(ctx, moyasarPaymentID, paymentService)
	if err != nil {
		s.logger.Error(ctx, "failed to check if payment exists",
			"error", err,
			"moyasar_payment_id", moyasarPaymentID)
		return err
	} else if exists {
		s.logger.Info(ctx, "payment already exists for this Moyasar payment, skipping",
			"moyasar_payment_id", moyasarPaymentID,
			"flexprice_invoice_id", flexpriceInvoiceID)
		return nil
	}

	// Step 2: Create external payment record
	err = s.createExternalPaymentRecord(ctx, payment, flexpriceInvoiceID, paymentService)
	if err != nil {
		s.logger.Error(ctx, "failed to create external payment record",
			"error", err,
			"moyasar_payment_id", moyasarPaymentID)
		return err
	}

	// Step 3: Reconcile invoice with external payment
	// Convert amount from smallest currency unit to standard unit using currency-aware conversion
	amount := convertFromSmallestUnit(int64(payment.Amount), payment.Currency)
	err = s.reconcileInvoice(ctx, flexpriceInvoiceID, amount, invoiceService)
	if err != nil {
		s.logger.Error(ctx, "failed to reconcile invoice with external payment",
			"error", err,
			"invoice_id", flexpriceInvoiceID,
			"amount", amount.String())
		return err
	}

	s.logger.Info(ctx, "successfully processed external Moyasar payment",
		"moyasar_payment_id", moyasarPaymentID,
		"flexprice_invoice_id", flexpriceInvoiceID,
		"amount", amount.String())

	return nil
}

// createExternalPaymentRecord creates a payment record for an external Moyasar payment
func (s *PaymentService) createExternalPaymentRecord(
	ctx context.Context,
	payment *MoyasarPaymentObject,
	invoiceID string,
	paymentService interfaces.PaymentService,
) error {
	moyasarPaymentID := payment.ID

	// Convert amount from smallest currency unit to standard unit using currency-aware conversion
	amount := convertFromSmallestUnit(int64(payment.Amount), payment.Currency)

	// Determine payment method ID from source
	var paymentMethodID string
	if payment.Source != nil {
		paymentMethodID = payment.Source.GatewayID
		if paymentMethodID == "" {
			paymentMethodID = payment.Source.ReferenceID
		}
	}

	s.logger.Info(ctx, "creating external payment record",
		"moyasar_payment_id", moyasarPaymentID,
		"invoice_id", invoiceID,
		"amount", amount.String(),
		"currency", payment.Currency)

	// Create payment record with Moyasar gateway - use import alias for dto
	gatewayType := types.PaymentGatewayTypeMoyasar
	createReq := &apidto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     invoiceID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		Amount:            amount,
		Currency:          strings.ToUpper(payment.Currency),
		PaymentGateway:    &gatewayType,
		ProcessPayment:    false, // Don't process - already succeeded in Moyasar
		PaymentMethodID:   paymentMethodID,
		Metadata: types.Metadata{
			"payment_source":     "moyasar_external",
			"moyasar_payment_id": moyasarPaymentID,
			"external_payment":   "true",
		},
	}

	paymentResp, err := paymentService.CreatePayment(ctx, createReq)
	if err != nil {
		s.logger.Error(ctx, "failed to create external payment record",
			"error", err,
			"moyasar_payment_id", moyasarPaymentID,
			"invoice_id", invoiceID)
		return err
	}

	// Update payment to succeeded status
	now := time.Now().UTC()
	updateReq := apidto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusSucceeded)),
		GatewayPaymentID: lo.ToPtr(moyasarPaymentID),
		SucceededAt:      lo.ToPtr(now),
	}

	_, err = paymentService.UpdatePayment(ctx, paymentResp.ID, updateReq)
	if err != nil {
		s.logger.Error(ctx, "failed to update external payment status, attempting cleanup",
			"error", err,
			"payment_id", paymentResp.ID,
			"moyasar_payment_id", moyasarPaymentID)

		// Cleanup: Delete the orphaned payment record to prevent inconsistent state
		// If cleanup fails, log the error but return the original update error
		if deleteErr := paymentService.DeletePayment(ctx, paymentResp.ID); deleteErr != nil {
			s.logger.Error(ctx, "failed to cleanup orphaned payment record",
				"error", deleteErr,
				"payment_id", paymentResp.ID,
				"moyasar_payment_id", moyasarPaymentID)
		} else {
			s.logger.Info(ctx, "successfully cleaned up orphaned payment record",
				"payment_id", paymentResp.ID,
				"moyasar_payment_id", moyasarPaymentID)
		}

		return err
	}

	s.logger.Info(ctx, "successfully created external payment record",
		"payment_id", paymentResp.ID,
		"moyasar_payment_id", moyasarPaymentID,
		"invoice_id", invoiceID,
		"amount", amount.String())

	return nil
}

// reconcileInvoice is the shared logic for invoice reconciliation
func (s *PaymentService) reconcileInvoice(
	ctx context.Context,
	invoiceID string,
	paymentAmount decimal.Decimal,
	invoiceService interfaces.InvoiceService,
) error {
	// Update invoice payment status
	err := invoiceService.ReconcilePaymentStatus(ctx, invoiceID, types.PaymentStatusSucceeded, &paymentAmount)
	if err != nil {
		s.logger.Error(ctx, "failed to reconcile invoice payment status",
			"invoice_id", invoiceID,
			"amount", paymentAmount.String(),
			"error", err)
		return err
	}

	s.logger.Info(ctx, "successfully reconciled invoice",
		"invoice_id", invoiceID,
		"amount", paymentAmount.String())

	return nil
}

// ============================================================================
// Tokenization / Saved Payment Methods
// ============================================================================

// ChargeSavedPaymentMethod charges a customer using their saved payment method (token)
func (s *PaymentService) ChargeSavedPaymentMethod(
	ctx context.Context,
	customerID string,
	tokenID string,
	amount decimal.Decimal,
	currency string,
	description string,
	invoiceID string,
	paymentID string,
) (*CreatePaymentLinkResponse, error) {
	if tokenID == "" {
		return nil, ierr.NewError("token_id is required").Mark(ierr.ErrValidation)
	}
	if amount.IsZero() || amount.IsNegative() {
		return nil, ierr.NewError("amount must be positive").Mark(ierr.ErrValidation)
	}

	// Default currency to SAR
	if currency == "" {
		currency = DefaultCurrency
	}
	currency = strings.ToUpper(currency)

	// Convert amount to smallest currency unit using currency-aware conversion
	amountInSmallestUnit, err := convertToSmallestUnit(amount, currency)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to convert amount to smallest currency unit").
			Mark(ierr.ErrInternal)
	}

	// Build description
	if description == "" {
		description = fmt.Sprintf("%s: %s", DefaultInvoiceLabel, invoiceID)
	}

	// Build metadata
	metadata := map[string]string{
		"flexprice_customer_id": customerID,
		"flexprice_payment_id":  paymentID,
		"payment_type":          "recurring",
	}
	if invoiceID != "" {
		metadata["flexprice_invoice_id"] = invoiceID
	}

	s.logger.Info(ctx, "charging saved payment method",
		"customer_id", customerID,
		"token_id", tokenID,
		"amount", amount.String(),
		"currency", currency)

	// Charge using token
	payment, err := s.client.ChargeWithToken(ctx, tokenID, int(amountInSmallestUnit), currency, description, metadata, paymentID, "")
	if err != nil {
		s.logger.Error(ctx, "failed to charge saved payment method",
			"customer_id", customerID,
			"token_id", tokenID,
			"error", err)
		return nil, err
	}

	// Build response
	response := &CreatePaymentLinkResponse{
		ID:         payment.ID,
		PaymentURL: payment.TransactionURL,
		Amount:     amount,
		Currency:   currency,
		Status:     payment.Status,
		CreatedAt:  payment.CreatedAt,
		PaymentID:  paymentID,
	}

	s.logger.Info(ctx, "successfully charged saved payment method",
		"moyasar_payment_id", payment.ID,
		"status", payment.Status,
		"customer_id", customerID)

	return response, nil
}

// ChargeInvoiceWithSavedToken attempts to auto-pay an invoice using the customer's
// saved Moyasar token (autopay). It returns charged=true when the token charge succeeded.
//
// Flow:
//  1. Load invoice; check customer has an ACTIVE saved payment method
//  2. Create a Flexprice INITIATED payment for destination_type=INVOICE so the webhook
//     can look it up via flexprice_payment_id (same pattern as the AUTH/card-save flow)
//  3. Create a Moyasar invoice so the charge appears in the Moyasar dashboard
//  4. Charge the token — Moyasar fires payment_paid webhook with flexprice_payment_id in metadata
//  5. Update our payment to PENDING; webhook advances it to SUCCEEDED and reconciles the invoice
//
// Returns charged=false (no error) when the customer has no saved token, so the
// caller falls back to the hosted invoice flow.
func (s *PaymentService) ChargeInvoiceWithSavedToken(
	ctx context.Context,
	invoiceID string,
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
) (bool, error) {
	inv, err := invoiceService.GetInvoice(ctx, invoiceID)
	if err != nil {
		return false, err
	}
	if inv.CustomerID == "" {
		s.logger.Warn(ctx, "invoice has no customer_id, skipping autopay", "invoice_id", invoiceID)
		return false, nil
	}

	paymentMethods, err := s.GetCustomerPaymentMethods(ctx, inv.CustomerID)
	if err != nil {
		return false, err
	}
	if len(paymentMethods) == 0 {
		s.logger.Info(ctx, "no saved payment method for customer, skipping autopay",
			"invoice_id", invoiceID,
			"customer_id", inv.CustomerID)
		return false, nil
	}

	amount := inv.AmountRemaining
	if amount.IsZero() || amount.IsNegative() {
		s.logger.Warn(ctx, "invoice has no remaining amount, skipping autopay",
			"invoice_id", invoiceID,
			"amount_remaining", amount.String())
		return false, nil
	}

	currency := strings.ToUpper(inv.Currency)
	amountInSmallestUnit, err := convertToSmallestUnit(amount, currency)
	if err != nil {
		return false, ierr.WithError(err).
			WithHint("Failed to convert amount to smallest currency unit").
			Mark(ierr.ErrInternal)
	}

	s.logger.Info(ctx, "attempting autopay with saved token",
		"invoice_id", invoiceID,
		"customer_id", inv.CustomerID,
		"amount", amount.String(),
		"currency", currency,
		"token_id", paymentMethods[0].ID)

	// Step 1: Create a Flexprice INITIATED payment so the webhook can look it up.
	// The charge metadata carries flexprice_payment_id → webhook marks SUCCEEDED → reconciles invoice.
	gatewayType := types.PaymentGatewayTypeMoyasar
	flexpricePayment, err := paymentService.CreatePayment(ctx, &apidto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     invoiceID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		Amount:            amount,
		Currency:          currency,
		PaymentGateway:    &gatewayType,
		ProcessPayment:    false,
		Metadata: types.Metadata{
			"payment_type": "autopay",
		},
	})
	if err != nil {
		s.logger.Error(ctx, "failed to create Flexprice tracking payment for autopay",
			"invoice_id", invoiceID,
			"error", err)
		return false, err
	}

	s.logger.Info(ctx, "created Flexprice tracking payment for autopay",
		"invoice_id", invoiceID,
		"flexprice_payment_id", flexpricePayment.ID)

	// Step 2: Create a Moyasar invoice so the charge appears in the Moyasar dashboard.
	var moyasarInvoiceID string
	syncResp, syncErr := s.invoiceSyncSvc.SyncInvoiceToMoyasar(ctx, MoyasarInvoiceSyncRequest{InvoiceID: invoiceID}, nil)
	if syncErr != nil {
		s.logger.Error(ctx, "failed to sync invoice to Moyasar, aborting autopay",
			"invoice_id", invoiceID,
			"flexprice_payment_id", flexpricePayment.ID,
			"error", syncErr)
		return false, syncErr
	}
	if syncResp != nil {
		moyasarInvoiceID = syncResp.MoyasarInvoiceID
		s.logger.Info(ctx, "synced invoice to Moyasar",
			"invoice_id", invoiceID,
			"moyasar_invoice_id", moyasarInvoiceID)
	}

	// Step 3: Charge the saved token.
	// flexprice_payment_id in metadata lets the webhook look up and advance our payment record.
	description := fmt.Sprintf("%s: %s", DefaultInvoiceLabel, invoiceID)
	metadata := map[string]string{
		"flexprice_payment_id":  flexpricePayment.ID,
		"flexprice_customer_id": inv.CustomerID,
		"flexprice_invoice_id":  invoiceID,
		"payment_type":          "autopay",
	}

	// Step 3: Create the charge in Moyasar.
	// If this fails, the Flexprice payment stays INITIATED — no PENDING update.
	charge, err := s.client.ChargeWithToken(ctx, paymentMethods[0].ID, int(amountInSmallestUnit), currency, description, metadata, flexpricePayment.ID, moyasarInvoiceID)
	if err != nil {
		s.logger.Error(ctx, "failed to create charge in Moyasar",
			"invoice_id", invoiceID,
			"flexprice_payment_id", flexpricePayment.ID,
			"moyasar_invoice_id", moyasarInvoiceID,
			"token_id", paymentMethods[0].ID,
			"error", err)
		return false, err
	}

	s.logger.Info(ctx, "charge created in Moyasar",
		"invoice_id", invoiceID,
		"flexprice_payment_id", flexpricePayment.ID,
		"moyasar_invoice_id", moyasarInvoiceID,
		"moyasar_payment_id", charge.ID,
		"status", charge.Status)

	// Step 4: Charge exists in Moyasar — move to PENDING.
	// Webhook payment_paid → SUCCEEDED + reconcile invoice.
	// Webhook payment_failed → FAILED.
	_, updateErr := paymentService.UpdatePayment(ctx, flexpricePayment.ID, apidto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusPending)),
		GatewayPaymentID: lo.ToPtr(charge.ID),
	})
	if updateErr != nil {
		s.logger.Error(ctx, "failed to update Flexprice payment to pending",
			"flexprice_payment_id", flexpricePayment.ID,
			"moyasar_payment_id", charge.ID,
			"invoice_id", invoiceID,
			"error", updateErr)
		// Non-fatal: webhook still carries flexprice_payment_id so reconciliation will work.
	}

	s.logger.Info(ctx, "autopay charge submitted, waiting for webhook",
		"invoice_id", invoiceID,
		"flexprice_payment_id", flexpricePayment.ID,
		"moyasar_invoice_id", moyasarInvoiceID,
		"moyasar_payment_id", charge.ID)

	return true, nil
}

// SetupIntent returns the Moyasar publishable key needed by Moyasar.js to render the
// card-entry form on the frontend. It also creates an INITIATED auth payment upfront so
// we have a tracking ID (flexprice_payment_id) to link the Moyasar 1-SAR charge back to.
func (s *PaymentService) SetupIntent(
	ctx context.Context,
	customerID string,
	paymentService interfaces.PaymentService,
) (*SetupIntentResponse, error) {
	if customerID == "" {
		return nil, ierr.NewError("customer_id is required").Mark(ierr.ErrValidation)
	}

	cfg, err := s.client.GetMoyasarConfig(ctx)
	if err != nil {
		return nil, err
	}

	if cfg.PublishableKey == "" {
		return nil, ierr.NewError("Moyasar publishable key not configured").
			WithHint("Add the publishable key to your Moyasar connection in Settings → Integrations").
			Mark(ierr.ErrValidation)
	}

	// Create an INITIATED auth payment upfront so we have a tracking ID.
	gatewayType := types.PaymentGatewayTypeMoyasar
	authPayment, err := paymentService.CreatePayment(ctx, &apidto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeAuth,
		DestinationID:     customerID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		Amount:            decimal.NewFromFloat(1.0),
		Currency:          "SAR",
		PaymentGateway:    &gatewayType,
		ProcessPayment:    false,
		Metadata: types.Metadata{
			"auth_type": "card_tokenization",
		},
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create tracking payment for setup intent").
			Mark(ierr.ErrInternal)
	}

	s.logger.Info(ctx, "setup intent created",
		"customer_id", customerID,
		"flexprice_payment_id", authPayment.ID)

	return &SetupIntentResponse{
		Status:             "pending_card_entry",
		CustomerID:         customerID,
		PublishableKey:     cfg.PublishableKey,
		FlexpricePaymentID: authPayment.ID,
	}, nil
}

// GetCustomerPaymentMethods returns saved payment methods for a customer from the payment_methods table.
// Token details (brand, last4, etc.) are stored at save time in method_details; no Moyasar API call needed.
func (s *PaymentService) GetCustomerPaymentMethods(
	ctx context.Context,
	customerID string,
) ([]*PaymentMethodInfo, error) {
	if customerID == "" {
		return nil, ierr.NewError("customer_id is required").Mark(ierr.ErrValidation)
	}

	s.logger.Debug(ctx, "getting customer payment methods", "customer_id", customerID)

	methods, err := s.customerSvc.GetCustomerPaymentMethods(ctx, customerID)
	if err != nil {
		s.logger.Error(ctx, "failed to get customer payment methods",
			"customer_id", customerID, "error", err)
		return nil, err
	}

	if len(methods) == 0 {
		return []*PaymentMethodInfo{}, nil
	}

	result := make([]*PaymentMethodInfo, 0, len(methods))
	for _, m := range methods {
		info := &PaymentMethodInfo{
			ID:        m.GatewayMethodID,
			IsDefault: m.IsDefault,
			CreatedAt: m.CreatedAt.String(),
		}
		// Populate card details from stored method_details
		if d := m.MethodDetails; d != nil {
			if v, ok := d["brand"].(string); ok {
				info.Brand = v
				info.Type = v
			}
			if v, ok := d["last4"].(string); ok {
				info.Last4 = v
			}
			if v, ok := d["exp_month"].(string); ok {
				info.ExpiryMonth = v
			}
			if v, ok := d["exp_year"].(string); ok {
				info.ExpiryYear = v
			}
			if v, ok := d["name"].(string); ok {
				info.Name = v
			}
		}
		result = append(result, info)
	}

	s.logger.Info(ctx, "retrieved customer payment methods",
		"customer_id", customerID, "count", len(result))

	return result, nil
}
