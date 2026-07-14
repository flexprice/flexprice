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
	// Add FlexPrice payment ID for webhook reconciliation
	if req.PaymentID != "" {
		metadata["flexprice_payment_id"] = req.PaymentID
	}
	if req.InvoiceID != "" {
		metadata["flexprice_invoice_id"] = req.InvoiceID
	}
	if req.CustomerID != "" {
		metadata["flexprice_customer_id"] = req.CustomerID
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
		GivenID:     req.PaymentID, // Use FlexPrice payment ID for idempotency
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
		s.logger.Info(context.Background(), "payment record not found for reconciliation",
			"payment_id", paymentID)
		return ierr.NewError("payment not found").Mark(ierr.ErrNotFound)
	}

	// Get the invoice ID from the payment destination
	if paymentRecord.DestinationType != types.PaymentDestinationTypeInvoice {
		s.logger.Info(ctx, "payment destination is not an invoice, skipping reconciliation",
			"payment_id", paymentID,
			"destination_type", paymentRecord.DestinationType)
		return nil
	}

	invoiceID := paymentRecord.DestinationID
	if invoiceID == "" {
		s.logger.Info(context.Background(), "payment has no invoice destination",
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
	}

	// Extract FlexPrice payment ID from metadata if available
	if payment.Metadata != nil {
		if flexpricePaymentID, ok := payment.Metadata["flexprice_payment_id"]; ok {
			response.FlexPricePaymentID = flexpricePaymentID
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
	payment *MoyasarPayment,
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
) error {
	moyasarPaymentID := payment.ID

	s.logger.Info(ctx, "no FlexPrice payment ID found, processing as external Moyasar payment",
		"moyasar_payment_id", moyasarPaymentID)

	// Check if invoice ID exists in metadata
	flexpriceInvoiceID := ""
	if payment.Metadata != nil {
		flexpriceInvoiceID = payment.Metadata["flexprice_invoice_id"]
	}

	if flexpriceInvoiceID == "" {
		s.logger.Info(ctx, "no FlexPrice invoice ID found in external payment metadata, skipping",
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

// ProcessExternalMoyasarPayment processes a payment that was made directly in Moyasar (external to FlexPrice)
func (s *PaymentService) ProcessExternalMoyasarPayment(
	ctx context.Context,
	payment *MoyasarPayment,
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

	// Step 4: Sync Moyasar invoice status into FlexPrice invoice metadata
	if syncErr := s.SyncMoyasarInvoiceStatus(ctx, flexpriceInvoiceID, payment.Status, invoiceService); syncErr != nil {
		s.logger.Error(ctx, "failed to sync Moyasar invoice status to invoice metadata",
			"error", syncErr,
			"flexprice_invoice_id", flexpriceInvoiceID,
			"moyasar_status", payment.Status)
	}

	s.logger.Info(ctx, "successfully processed external Moyasar payment",
		"moyasar_payment_id", moyasarPaymentID,
		"flexprice_invoice_id", flexpriceInvoiceID,
		"amount", amount.String())

	return nil
}

// syncMoyasarInvoiceStatus updates the moyasar_invoice_status key in the FlexPrice invoice metadata
// to reflect the current Moyasar payment status. Failures are non-fatal — logged by the caller.
func (s *PaymentService) SyncMoyasarInvoiceStatus(
	ctx context.Context,
	flexpriceInvoiceID string,
	moyasarStatus string,
	invoiceService interfaces.InvoiceService,
) error {
	inv, err := invoiceService.GetInvoice(ctx, flexpriceInvoiceID)
	if err != nil {
		return err
	}

	metadata := types.Metadata{}
	for k, v := range inv.Metadata {
		metadata[k] = v
	}
	metadata["moyasar_invoice_status"] = moyasarStatus

	_, err = invoiceService.UpdateInvoice(ctx, flexpriceInvoiceID, apidto.UpdateInvoiceRequest{
		Metadata: &metadata,
	})
	return err
}

// createExternalPaymentRecord creates a payment record for an external Moyasar payment
func (s *PaymentService) createExternalPaymentRecord(
	ctx context.Context,
	payment *MoyasarPayment,
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
	moyasarInvoiceID string,
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
		description = fmt.Sprintf("%s: %s", DefaultInvoiceLabel, paymentID)
	}

	// Build metadata
	metadata := map[string]string{
		"flexprice_customer_id": customerID,
		"flexprice_payment_id":  paymentID,
		"payment_type":          "recurring",
	}

	s.logger.Info(ctx, "charging saved payment method",
		"customer_id", customerID,
		"amount", amount.String(),
		"currency", currency)

	// Charge using token — no given_id, flexprice_payment_id in metadata is the anchor
	// moyasarInvoiceID links this payment to the Moyasar invoice so Moyasar marks it paid internally
	payment, err := s.client.ChargeWithToken(ctx, tokenID, int(amountInSmallestUnit), currency, description, metadata, "", moyasarInvoiceID)
	if err != nil {
		s.logger.Error(ctx, "failed to charge saved payment method",
			"customer_id", customerID,
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

// SetupIntent creates a setup intent to save a payment method for future use
// In Moyasar, this creates a token that can be used for recurring payments
// Note: Token creation in Moyasar typically happens on the frontend.
// This method returns a response with the callback URL for frontend token creation.
func (s *PaymentService) SetupIntent(
	ctx context.Context,
	customerID string,
	req *SetupIntentRequest,
) (*SetupIntentResponse, error) {
	if customerID == "" {
		return nil, ierr.NewError("customer_id is required").Mark(ierr.ErrValidation)
	}
	if req == nil {
		return nil, ierr.NewError("setup intent request is required").Mark(ierr.ErrValidation)
	}

	s.logger.Info(ctx, "creating setup intent for customer",
		"customer_id", customerID,
		"has_callback_url", req.CallbackURL != "")

	// Build metadata for the token
	metadata := make(map[string]string)
	if req.Metadata != nil {
		for k, v := range req.Metadata {
			metadata[k] = v
		}
	}
	metadata["flexprice_customer_id"] = customerID

	// Note: Token creation in Moyasar typically happens on the frontend
	// This setup intent response provides the configuration for frontend token creation
	response := &SetupIntentResponse{
		TokenID:  "", // Token ID will be created on frontend
		Status:   "pending_card_entry",
		SetupURL: req.CallbackURL,
	}

	s.logger.Info(ctx, "setup intent created",
		"customer_id", customerID,
		"status", response.Status)

	return response, nil
}

// GetCustomerPaymentMethods retrieves saved payment methods (tokens) for a customer
// Note: Moyasar doesn't have a native customer-to-tokens mapping API,
// so tokens are stored in FlexPrice's entity integration mapping
func (s *PaymentService) GetCustomerPaymentMethods(
	ctx context.Context,
	customerID string,
	customerService interfaces.CustomerService,
) ([]*PaymentMethodInfo, error) {
	if customerID == "" {
		return nil, ierr.NewError("customer_id is required").Mark(ierr.ErrValidation)
	}

	s.logger.Debug(ctx, "getting customer payment methods",
		"customer_id", customerID)

	// Get customer's saved token IDs from entity integration mapping
	tokenIDs, err := s.customerSvc.GetCustomerTokens(ctx, customerID)
	if err != nil {
		s.logger.Error(ctx, "failed to get customer tokens",
			"customer_id", customerID,
			"error", err)
		return nil, err
	}

	if len(tokenIDs) == 0 {
		s.logger.Info(ctx, "no saved payment methods found for customer",
			"customer_id", customerID)
		return []*PaymentMethodInfo{}, nil
	}

	// Fetch token details from Moyasar
	var paymentMethods []*PaymentMethodInfo
	for i, tokenID := range tokenIDs {
		token, err := s.client.GetToken(ctx, tokenID)
		if err != nil {
			s.logger.Info(context.Background(), "failed to get token from Moyasar, skipping",
				"error", err)
			continue
		}

		paymentMethod := &PaymentMethodInfo{
			ID:          token.ID,
			Type:        string(token.Brand),
			Brand:       token.Brand,
			Last4:       token.Last4,
			ExpiryMonth: token.Month,
			ExpiryYear:  token.Year,
			Name:        token.Name,
			IsDefault:   i == 0, // First token is considered default
			CreatedAt:   token.CreatedAt,
		}
		paymentMethods = append(paymentMethods, paymentMethod)
	}

	s.logger.Info(ctx, "retrieved customer payment methods",
		"customer_id", customerID,
		"count", len(paymentMethods))

	return paymentMethods, nil
}
