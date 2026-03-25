package paddle

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/PaddleHQ/paddle-go-sdk/v4/pkg/paddlenotification"
	apidto "github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// PaymentService handles Paddle payment operations for webhook processing.
// Used only for transaction.completed webhook reconciliation (no CreatePaymentLink).
type PaymentService struct {
	logger *logger.Logger
}

// NewPaymentService creates a new Paddle payment service
func NewPaymentService(logger *logger.Logger) *PaymentService {
	return &PaymentService{
		logger: logger,
	}
}

// ProcessExternalPaddleTransaction processes a transaction.completed webhook from Paddle.
// It creates a payment record and reconciles the invoice.
func (s *PaymentService) ProcessExternalPaddleTransaction(
	ctx context.Context,
	transaction *paddlenotification.TransactionNotification,
	flexpriceInvoiceID string,
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
) error {
	paddleTransactionID := transaction.ID

	s.logger.Infow("processing external Paddle transaction",
		"paddle_transaction_id", paddleTransactionID,
		"flexprice_invoice_id", flexpriceInvoiceID)

	// Step 1: Idempotency - check if payment already exists
	exists, err := paymentService.PaymentExistsByGatewayPaymentID(ctx, paddleTransactionID)
	if err != nil {
		s.logger.Errorw("failed to check if payment exists",
			"error", err,
			"paddle_transaction_id", paddleTransactionID)
		return err
	}
	if exists {
		s.logger.Debugw("payment already exists for Paddle transaction, skipping",
			"paddle_transaction_id", paddleTransactionID)
		return nil
	}

	// Step 2: Create external payment record
	err = s.createExternalPaymentRecord(ctx, transaction, flexpriceInvoiceID, paymentService)
	if err != nil {
		s.logger.Errorw("failed to create external payment record",
			"error", err,
			"paddle_transaction_id", paddleTransactionID)
		return err
	}

	// Step 3: Reconcile invoice
	amount := s.convertFromSmallestUnit(transaction.Details.Totals.Total, string(transaction.CurrencyCode))
	err = s.reconcileInvoice(ctx, flexpriceInvoiceID, amount, invoiceService)
	if err != nil {
		s.logger.Errorw("failed to reconcile invoice with external payment",
			"error", err,
			"invoice_id", flexpriceInvoiceID)
		return err
	}

	s.logger.Infow("successfully processed external Paddle transaction",
		"paddle_transaction_id", paddleTransactionID,
		"flexprice_invoice_id", flexpriceInvoiceID)

	return nil
}

// createExternalPaymentRecord creates a payment record for an external Paddle transaction
func (s *PaymentService) createExternalPaymentRecord(
	ctx context.Context,
	transaction *paddlenotification.TransactionNotification,
	invoiceID string,
	paymentService interfaces.PaymentService,
) error {
	paddleTransactionID := transaction.ID
	amount := s.convertFromSmallestUnit(transaction.Details.Totals.Total, string(transaction.CurrencyCode))
	currency := strings.ToUpper(string(transaction.CurrencyCode))

	// Extract payment method info from first captured payment attempt
	var paddlePaymentAttemptID, paddlePaymentMethodID string
	var paddlePaymentMethodType string
	var cardLast4 string
	for _, p := range transaction.Payments {
		if p.Status == paddlenotification.PaymentAttemptStatusCaptured {
			paddlePaymentAttemptID = p.PaymentAttemptID
			if p.PaymentMethodID != nil && *p.PaymentMethodID != "" {
				paddlePaymentMethodID = *p.PaymentMethodID
			}
			if p.MethodDetails.Type != "" {
				paddlePaymentMethodType = string(p.MethodDetails.Type)
			}
			if p.MethodDetails.Card != nil && p.MethodDetails.Card.Last4 != "" {
				cardLast4 = p.MethodDetails.Card.Last4
			}
			break
		}
	}

	s.logger.Debugw("creating external payment record",
		"paddle_transaction_id", paddleTransactionID,
		"invoice_id", invoiceID)

	gatewayType := types.PaymentGatewayTypePaddle
	metadata := types.Metadata{
		"payment_source":        "paddle_external",
		"paddle_transaction_id": paddleTransactionID,
		"external_payment":      "true",
	}
	if paddlePaymentAttemptID != "" {
		metadata["paddle_payment_attempt_id"] = paddlePaymentAttemptID
	}
	if paddlePaymentMethodType != "" {
		metadata["paddle_payment_method_type"] = paddlePaymentMethodType
	}
	if cardLast4 != "" {
		metadata["paddle_card_last4"] = cardLast4
	}

	createReq := &apidto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     invoiceID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentMethodID:   paddlePaymentMethodID,
		Amount:            amount,
		Currency:          currency,
		PaymentGateway:    &gatewayType,
		ProcessPayment:    false,
		Metadata:          metadata,
	}

	paymentResp, err := paymentService.CreatePayment(ctx, createReq)
	if err != nil {
		s.logger.Errorw("failed to create external payment record",
			"error", err,
			"paddle_transaction_id", paddleTransactionID,
			"invoice_id", invoiceID)
		return err
	}

	// Update payment to succeeded status
	now := time.Now().UTC()
	updateReq := apidto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusSucceeded)),
		GatewayPaymentID: lo.ToPtr(paddleTransactionID),
		SucceededAt:      lo.ToPtr(now),
	}

	_, err = paymentService.UpdatePayment(ctx, paymentResp.ID, updateReq)
	if err != nil {
		s.logger.Errorw("failed to update external payment status, attempting cleanup",
			"error", err,
			"payment_id", paymentResp.ID,
			"paddle_transaction_id", paddleTransactionID)

		if deleteErr := paymentService.DeletePayment(ctx, paymentResp.ID); deleteErr != nil {
			s.logger.Errorw("failed to cleanup orphaned payment record",
				"error", deleteErr,
				"payment_id", paymentResp.ID,
				"paddle_transaction_id", paddleTransactionID)
		} else {
			s.logger.Debugw("cleaned up orphaned payment record",
				"payment_id", paymentResp.ID)
		}
		return err
	}

	s.logger.Infow("successfully created external payment record",
		"payment_id", paymentResp.ID,
		"paddle_transaction_id", paddleTransactionID,
		"invoice_id", invoiceID)

	return nil
}

// reconcileInvoice updates the invoice payment status
func (s *PaymentService) reconcileInvoice(
	ctx context.Context,
	invoiceID string,
	paymentAmount decimal.Decimal,
	invoiceService interfaces.InvoiceService,
) error {
	err := invoiceService.ReconcilePaymentStatus(ctx, invoiceID, types.PaymentStatusSucceeded, &paymentAmount)
	if err != nil {
		s.logger.Errorw("failed to reconcile invoice payment status",
			"invoice_id", invoiceID,
			"error", err)
		return err
	}

	s.logger.Debugw("reconciled invoice", "invoice_id", invoiceID)

	return nil
}

// convertFromSmallestUnit converts Paddle amount (smallest unit, e.g. cents) to standard unit.
// Paddle stores amounts as strings in smallest denomination.
func (s *PaymentService) convertFromSmallestUnit(totalStr string, currency string) decimal.Decimal {
	if totalStr == "" {
		s.logger.Warnw("empty Paddle total, using zero", "currency", currency)
		return decimal.Zero
	}
	amountInt, err := strconv.ParseInt(totalStr, 10, 64)
	if err != nil {
		s.logger.Warnw("failed to parse Paddle total, using zero",
			"currency", currency,
			"error", err)
		return decimal.Zero
	}
	precision := types.GetCurrencyPrecision(currency)
	var divisor int64 = 100
	if precision == 0 {
		divisor = 1
	} else if precision == 3 {
		divisor = 1000
	} else if precision != 2 {
		divisor = 1
		for i := int32(0); i < precision; i++ {
			divisor *= 10
		}
	}
	return decimal.NewFromInt(amountInt).Div(decimal.NewFromInt(divisor))
}
