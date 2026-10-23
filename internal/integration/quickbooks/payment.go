package quickbooks

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// QuickBooksPaymentService defines the interface for QuickBooks payment operations
type QuickBooksPaymentService interface {
	// HandleExternalPaymentFromWebhook processes a QuickBooks payment webhook (INBOUND ONLY)
	HandleExternalPaymentFromWebhook(ctx context.Context, qbPaymentID string, paymentService interfaces.PaymentService, invoiceService interfaces.InvoiceService, db postgres.IClient) error
}

// PaymentService handles QuickBooks payment operations
type PaymentService struct {
	client                       QuickBooksClient
	invoiceRepo                  invoice.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// PaymentServiceParams contains parameters for creating a PaymentService
type PaymentServiceParams struct {
	Client                       QuickBooksClient
	InvoiceRepo                  invoice.Repository
	EntityIntegrationMappingRepo entityintegrationmapping.Repository
	Logger                       *logger.Logger
}

// NewPaymentService creates a new QuickBooks payment service
func NewPaymentService(params PaymentServiceParams) QuickBooksPaymentService {
	return &PaymentService{
		client:                       params.Client,
		invoiceRepo:                  params.InvoiceRepo,
		entityIntegrationMappingRepo: params.EntityIntegrationMappingRepo,
		logger:                       params.Logger,
	}
}

// HandleExternalPaymentFromWebhook processes a QuickBooks payment webhook (INBOUND SYNC)
//
// FLOW: QuickBooks → Flexprice
// TRIGGER: When a payment is recorded in QuickBooks (via webhook notification)
//
// WHAT IT DOES:
// 1. Receives webhook notification (minimal data: just payment ID)
// 2. Calls QuickBooks API to get full payment details (amount, linked invoice, date)
// 3. Finds the Flexprice invoice via entity mapping
// 4. Creates a payment record in Flexprice with QuickBooks details in metadata
// 5. Uses ReconcilePaymentStatus() to update invoice (supports partial payments!)
//
// IMPORTANT: Now creates payment records, supports partial payments!
func (s *PaymentService) HandleExternalPaymentFromWebhook(ctx context.Context, qbPaymentID string, paymentService interfaces.PaymentService, invoiceService interfaces.InvoiceService, db postgres.IClient) error {
	s.logger.Info(ctx, "processing QuickBooks payment webhook",
		"quickbooks_payment_id", qbPaymentID)

	// Fetch payment details from QuickBooks to get the linked invoice and amount
	qbPayment, err := s.client.GetPayment(ctx, qbPaymentID)
	if err != nil {
		s.logger.Error(ctx, "failed to get payment from QuickBooks",
			"error", err,
			"quickbooks_payment_id", qbPaymentID)
		return err
	}

	// Find invoice references in payment lines
	var qbInvoiceID string
	var paymentAmount float64
	for _, line := range qbPayment.Line {
		for _, txn := range line.LinkedTxn {
			if txn.TxnType == "Invoice" {
				qbInvoiceID = txn.TxnId
				paymentAmount = line.Amount // Get actual payment amount
				break
			}
		}
		if qbInvoiceID != "" {
			break
		}
	}

	if qbInvoiceID == "" {
		s.logger.Debug(ctx, "no invoice linked to QuickBooks payment, skipping",
			"quickbooks_payment_id", qbPaymentID)
		return nil
	}

	// Find Flexprice invoice via mapping
	invoiceMapping, err := s.findInvoiceMappingByProviderID(ctx, qbInvoiceID)
	if err != nil {
		if ierr.IsNotFound(err) {
			s.logger.Debug(ctx, "invoice not found in Flexprice, skipping payment sync",
				"quickbooks_payment_id", qbPaymentID,
				"quickbooks_invoice_id", qbInvoiceID)
			return nil
		}
		s.logger.Error(ctx, "failed to find invoice mapping",
			"error", err,
			"quickbooks_invoice_id", qbInvoiceID)
		return err
	}

	flexpriceInvoiceID := invoiceMapping.EntityID

	s.logger.Info(ctx, "found Flexprice invoice for QuickBooks payment",
		"flexprice_invoice_id", flexpriceInvoiceID,
		"quickbooks_invoice_id", qbInvoiceID,
		"quickbooks_payment_id", qbPaymentID,
		"payment_amount", paymentAmount)

	amount := decimal.NewFromFloat(paymentAmount)
	applied := false
	err = db.WithTx(ctx, func(txCtx context.Context) error {
		if lockErr := db.LockWithWait(txCtx, postgres.LockRequest{Key: "quickbooks_payment:" + qbPayment.ID}); lockErr != nil {
			return lockErr
		}
		created, createErr := s.createExternalPaymentRecord(txCtx, qbPayment, qbInvoiceID, invoiceMapping.ID, flexpriceInvoiceID, paymentService, invoiceService)
		if createErr != nil || !created {
			return createErr
		}
		if reconcileErr := s.reconcileInvoiceWithExternalPayment(txCtx, flexpriceInvoiceID, amount, invoiceService); reconcileErr != nil {
			return reconcileErr
		}
		applied = true
		return nil
	})
	if err != nil {
		s.logger.Error(ctx, "failed to apply QuickBooks payment",
			"error", err,
			"quickbooks_payment_id", qbPaymentID,
			"invoice_id", flexpriceInvoiceID)
		return err
	}
	if !applied {
		s.logger.Info(ctx, "QuickBooks payment already applied, skipping",
			"quickbooks_payment_id", qbPaymentID,
			"flexprice_invoice_id", flexpriceInvoiceID)
		return nil
	}

	s.logger.Info(ctx, "successfully processed QuickBooks payment webhook",
		"quickbooks_payment_id", qbPaymentID,
		"quickbooks_invoice_id", qbInvoiceID,
		"flexprice_invoice_id", flexpriceInvoiceID,
		"payment_amount", amount)

	return nil
}

// createExternalPaymentRecord records the QuickBooks payment as succeeded.
// It must run under the payment lock and returns true only when this call
// moved an unfinished payment to succeeded.
func (s *PaymentService) createExternalPaymentRecord(ctx context.Context, qbPayment *PaymentResponse, qbInvoiceID, entityMappingID, invoiceID string, paymentService interfaces.PaymentService, invoiceService interfaces.InvoiceService) (bool, error) {
	// Any row with this gateway ID was already processed.
	filter := types.NewNoLimitPaymentFilter()
	filter.GatewayPaymentID = &qbPayment.ID
	listResp, err := paymentService.ListPayments(ctx, filter)
	if err != nil {
		return false, err
	}
	if listResp != nil && len(listResp.Items) > 0 {
		return false, nil
	}

	// Extract actual payment amount from payment lines
	var paymentAmount float64
	for _, line := range qbPayment.Line {
		paymentAmount += line.Amount
	}

	amount := decimal.NewFromFloat(paymentAmount)

	// Get invoice to get currency
	invoiceResp, err := invoiceService.GetInvoice(ctx, invoiceID)
	if err != nil {
		s.logger.Error(ctx, "failed to get invoice for payment creation",
			"error", err,
			"invoice_id", invoiceID)
		return false, err
	}

	s.logger.Info(ctx, "creating external payment record for QuickBooks payment",
		"quickbooks_payment_id", qbPayment.ID,
		"invoice_id", invoiceID,
		"amount", amount,
		"currency", invoiceResp.Currency)

	// Create payment with QuickBooks details in metadata
	// For external QuickBooks payments, use OFFLINE payment method type
	// Payment already succeeded in QuickBooks, so we just record it in Flexprice

	methodType := types.PaymentMethodTypeCard
	createReq := &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     invoiceID,
		Amount:            amount,
		Currency:          invoiceResp.Currency,
		PaymentMethodType: methodType,
		ProcessPayment:    false, // Don't process - already succeeded in QuickBooks
		IdempotencyKey:    "quickbooks_payment_" + qbPayment.ID,
		Metadata: types.Metadata{
			"payment_source":          "quickbooks_external",
			"quickbooks_payment_id":   qbPayment.ID,
			"quickbooks_invoice_id":   qbInvoiceID,
			"entity_mapping_id":       entityMappingID,
			"webhook_event_id":        qbPayment.ID, // For idempotency
			"quickbooks_payment_date": qbPayment.TxnDate,
		},
	}

	// Add customer ref if available
	if qbPayment.CustomerRef.Value != "" {
		createReq.Metadata["quickbooks_customer_id"] = qbPayment.CustomerRef.Value
	}

	// Add private note if available
	if qbPayment.PrivateNote != "" {
		createReq.Metadata["quickbooks_note"] = qbPayment.PrivateNote
	}

	paymentResp, err := paymentService.CreatePayment(ctx, createReq)
	if err != nil {
		s.logger.Error(ctx, "failed to create external payment record",
			"error", err,
			"quickbooks_payment_id", qbPayment.ID,
			"invoice_id", invoiceID)
		return false, err
	}

	// Status read under the lock decides; the create response may be stale.
	current, err := paymentService.GetPayment(ctx, paymentResp.ID)
	if err != nil {
		return false, err
	}
	switch current.PaymentStatus {
	case types.PaymentStatusInitiated, types.PaymentStatusPending, types.PaymentStatusProcessing:
	default:
		return false, nil
	}

	// Update payment to succeeded status with QuickBooks details
	// Note: GatewayPaymentID stores QuickBooks payment ID for tracking, but PaymentGateway stays nil for offline payments
	now := time.Now().UTC()
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusSucceeded)),
		GatewayPaymentID: lo.ToPtr(qbPayment.ID), // Store QuickBooks payment ID for reference
		SucceededAt:      &now,
	}

	_, err = paymentService.UpdatePayment(ctx, paymentResp.ID, updateReq)
	if err != nil {
		s.logger.Error(ctx, "failed to update external payment status",
			"error", err,
			"payment_id", paymentResp.ID,
			"quickbooks_payment_id", qbPayment.ID)
		return false, err
	}

	s.logger.Info(ctx, "successfully created external payment record",
		"payment_id", paymentResp.ID,
		"quickbooks_payment_id", qbPayment.ID,
		"invoice_id", invoiceID,
		"amount", amount)

	return true, nil
}

// reconcileInvoiceWithExternalPayment reconciles an invoice with an external QuickBooks payment
// It supports partial payments!
func (s *PaymentService) reconcileInvoiceWithExternalPayment(ctx context.Context, invoiceID string, paymentAmount decimal.Decimal, invoiceService interfaces.InvoiceService) error {
	// Get invoice to calculate new payment status
	invoiceResp, err := invoiceService.GetInvoice(ctx, invoiceID)
	if err != nil {
		s.logger.Error(ctx, "failed to get invoice for external payment reconciliation",
			"error", err,
			"invoice_id", invoiceID)
		return err
	}

	// Calculate new amounts
	newAmountPaid := invoiceResp.AmountPaid.Add(paymentAmount)
	newAmountRemaining := invoiceResp.AmountDue.Sub(newAmountPaid)

	// Determine payment status (SUPPORTS PARTIAL PAYMENTS!)
	var newPaymentStatus types.PaymentStatus
	if newAmountRemaining.IsZero() {
		newPaymentStatus = types.PaymentStatusSucceeded // Fully paid
	} else if newAmountRemaining.IsNegative() {
		newPaymentStatus = types.PaymentStatusOverpaid // Overpaid
	} else {
		newPaymentStatus = types.PaymentStatusPending // Partial payment
	}

	s.logger.Info(ctx, "calculated payment status for external QuickBooks payment",
		"invoice_id", invoiceID,
		"payment_amount", paymentAmount,
		"current_amount_paid", invoiceResp.AmountPaid,
		"new_amount_paid", newAmountPaid,
		"amount_due", invoiceResp.AmountDue,
		"new_amount_remaining", newAmountRemaining,
		"new_payment_status", newPaymentStatus)

	// Use ReconcilePaymentStatus
	err = invoiceService.ReconcilePaymentStatus(ctx, invoiceID, newPaymentStatus, &paymentAmount)
	if err != nil {
		s.logger.Error(ctx, "failed to update invoice payment status",
			"error", err,
			"invoice_id", invoiceID,
			"payment_amount", paymentAmount,
			"payment_status", newPaymentStatus)
		return err
	}

	s.logger.Info(ctx, "successfully reconciled invoice with external QuickBooks payment",
		"invoice_id", invoiceID,
		"payment_amount", paymentAmount,
		"payment_status", newPaymentStatus)

	return nil
}

// findInvoiceMappingByProviderID finds an invoice mapping by QuickBooks invoice ID
func (s *PaymentService) findInvoiceMappingByProviderID(ctx context.Context, qbInvoiceID string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderQuickBooks)},
		ProviderEntityIDs: []string{qbInvoiceID},
		EntityType:        types.IntegrationEntityTypeInvoice,
		QueryFilter:       types.NewDefaultQueryFilter(),
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(mappings) == 0 {
		return nil, ierr.NewError("invoice mapping not found").Mark(ierr.ErrNotFound)
	}

	return mappings[0], nil
}
