package ledger

import (
	"context"
	"strings"
	"time"

	apidto "github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// PaymentsLedger maintains cross-system traceability for Flexprice-initiated payments.
// It creates an internal payment record before any gateway call, threads its ID through
// the gateway's metadata, and records the outcome when the webhook resolves it back.
type PaymentsLedger struct {
	paymentService interfaces.PaymentService
	invoiceService interfaces.InvoiceService
	logger         *logger.Logger
}

// NewPaymentsLedger returns a PaymentsLedger wired with the given payment and invoice services.
func NewPaymentsLedger(
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
	logger *logger.Logger,
) *PaymentsLedger {
	return &PaymentsLedger{
		paymentService: paymentService,
		invoiceService: invoiceService,
		logger:         logger,
	}
}

// InitiatePayment creates a payment record in INITIATED state and returns its ID.
// The caller must pass this ID as `flexprice_payment_id` in the gateway's
// metadata so that the webhook can resolve it back.
//
// Idempotent: if a payment with the given IdempotencyKey already exists,
// the existing ID is returned without creating a duplicate.
func (l *PaymentsLedger) InitiatePayment(ctx context.Context, params InitiatePaymentParams) (string, error) {
	l.logger.Info(ctx, "initiating payment",
		"destination_type", params.DestinationType,
		"destination_id", params.DestinationID,
		"gateway", params.Gateway,
		"amount", params.Amount.String(),
		"currency", params.Currency,
	)

	if err := l.validateInitiateParams(params); err != nil {
		return "", err
	}

	gatewayType := types.PaymentGatewayType(params.Gateway)
	req := &apidto.CreatePaymentRequest{
		DestinationType:   params.DestinationType,
		DestinationID:     params.DestinationID,
		PaymentMethodType: params.PaymentMethodType,
		PaymentGateway:    &gatewayType,
		Amount:            params.Amount,
		Currency:          strings.ToUpper(params.Currency),
		ProcessPayment:    false,
	}

	payment, err := l.paymentService.CreatePayment(ctx, req)
	if err != nil {
		l.logger.Error(ctx, "failed to create payment record",
			"destination_type", params.DestinationType,
			"destination_id", params.DestinationID,
			"gateway", params.Gateway,
			"error", err,
		)
		return "", ierr.WithError(err).
			WithHint("Failed to initiate payment").
			WithReportableDetails(map[string]any{
				"destination_type": params.DestinationType,
				"destination_id":   params.DestinationID,
				"gateway":          params.Gateway,
			}).
			Mark(ierr.ErrSystem)
	}

	l.logger.Info(ctx, "payment initiated",
		"flexprice_payment_id", payment.ID,
		"destination_type", params.DestinationType,
		"destination_id", params.DestinationID,
		"gateway", params.Gateway,
		"status", payment.PaymentStatus,
	)

	return payment.ID, nil
}

// ConfirmGatewayPayment transitions the payment from INITIATED to PENDING.
// Call this immediately after the gateway accepts the charge request and
// returns a gateway_payment_id. This proves the charge is in-flight.
func (l *PaymentsLedger) ConfirmGatewayPayment(ctx context.Context, flexpricePaymentID, gatewayPaymentID string) error {
	l.logger.Info(ctx, "confirming gateway payment",
		"flexprice_payment_id", flexpricePaymentID,
		"gateway_payment_id", gatewayPaymentID,
	)

	if flexpricePaymentID == "" {
		return ierr.NewError("flexprice_payment_id is required").Mark(ierr.ErrValidation)
	}
	if gatewayPaymentID == "" {
		return ierr.NewError("gateway_payment_id is required").Mark(ierr.ErrValidation)
	}

	updateReq := apidto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusPending)),
		GatewayPaymentID: lo.ToPtr(gatewayPaymentID),
	}

	_, err := l.paymentService.UpdatePayment(ctx, flexpricePaymentID, updateReq)
	if err != nil {
		l.logger.Error(ctx, "failed to confirm gateway payment",
			"flexprice_payment_id", flexpricePaymentID,
			"gateway_payment_id", gatewayPaymentID,
			"error", err,
		)
		return ierr.WithError(err).
			WithHint("Failed to confirm gateway payment").
			WithReportableDetails(map[string]any{
				"flexprice_payment_id": flexpricePaymentID,
				"gateway_payment_id":   gatewayPaymentID,
			}).
			Mark(ierr.ErrSystem)
	}

	l.logger.Info(ctx, "gateway payment confirmed, status now PENDING",
		"flexprice_payment_id", flexpricePaymentID,
		"gateway_payment_id", gatewayPaymentID,
	)

	return nil
}

// RecordPaymentSuccess transitions the payment to SUCCEEDED and, for INVOICE
// destination payments, reconciles the invoice. Called from the webhook handler.
//
// Idempotent: if the payment is already SUCCEEDED, logs and returns nil.
func (l *PaymentsLedger) RecordPaymentSuccess(ctx context.Context, params RecordPaymentSuccessParams) error {
	l.logger.Info(ctx, "recording payment success",
		"flexprice_payment_id", params.FlexpricePaymentID,
		"gateway_payment_id", params.GatewayPaymentID,
		"succeeded_at", params.SucceededAt,
	)

	if params.FlexpricePaymentID == "" {
		return ierr.NewError("flexprice_payment_id is required").Mark(ierr.ErrValidation)
	}

	existing, err := l.paymentService.GetPayment(ctx, params.FlexpricePaymentID)
	if err != nil {
		l.logger.Error(ctx, "failed to get payment for success recording",
			"flexprice_payment_id", params.FlexpricePaymentID,
			"error", err,
		)
		return ierr.WithError(err).
			WithHint("Failed to retrieve payment").
			WithReportableDetails(map[string]any{
				"flexprice_payment_id": params.FlexpricePaymentID,
			}).
			Mark(ierr.ErrSystem)
	}

	if existing.PaymentStatus == types.PaymentStatusSucceeded {
		l.logger.Info(ctx, "payment already succeeded, skipping",
			"flexprice_payment_id", params.FlexpricePaymentID,
			"gateway_payment_id", params.GatewayPaymentID,
		)
		return nil
	}
	if existing.PaymentStatus.IsTerminal() {
		return ierr.NewError("payment is in a terminal state").
			WithHint("Cannot transition to succeeded from current state").
			WithReportableDetails(map[string]any{
				"flexprice_payment_id": params.FlexpricePaymentID,
				"current_status":       existing.PaymentStatus,
				"destination_type":     existing.DestinationType,
			}).
			Mark(ierr.ErrValidation)
	}

	succeededAt := params.SucceededAt
	if succeededAt.IsZero() {
		succeededAt = time.Now().UTC()
	}

	updateReq := apidto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusSucceeded)),
		GatewayPaymentID: lo.ToPtr(params.GatewayPaymentID),
		SucceededAt:      lo.ToPtr(succeededAt),
	}

	_, err = l.paymentService.UpdatePayment(ctx, params.FlexpricePaymentID, updateReq)
	if err != nil {
		l.logger.Error(ctx, "failed to update payment to succeeded",
			"flexprice_payment_id", params.FlexpricePaymentID,
			"gateway_payment_id", params.GatewayPaymentID,
			"error", err,
		)
		return ierr.WithError(err).
			WithHint("Failed to record payment success").
			WithReportableDetails(map[string]any{
				"flexprice_payment_id": params.FlexpricePaymentID,
				"gateway_payment_id":   params.GatewayPaymentID,
			}).
			Mark(ierr.ErrSystem)
	}

	l.logger.Info(ctx, "payment marked as succeeded",
		"flexprice_payment_id", params.FlexpricePaymentID,
		"gateway_payment_id", params.GatewayPaymentID,
		"destination_type", existing.DestinationType,
		"destination_id", existing.DestinationID,
	)

	// For INVOICE destination, reconcile invoice payment status.
	if existing.DestinationType == types.PaymentDestinationTypeInvoice {
		invoiceID := existing.DestinationID
		l.logger.Info(ctx, "reconciling invoice after payment success",
			"flexprice_payment_id", params.FlexpricePaymentID,
			"invoice_id", invoiceID,
			"amount", existing.Amount.String(),
		)

		if err := l.invoiceService.ReconcilePaymentStatus(ctx, invoiceID, types.PaymentStatusSucceeded, &existing.Amount); err != nil {
			l.logger.Error(ctx, "failed to reconcile invoice after payment success",
				"flexprice_payment_id", params.FlexpricePaymentID,
				"invoice_id", invoiceID,
				"amount", existing.Amount.String(),
				"error", err,
			)
			return ierr.WithError(err).
				WithHint("Payment succeeded but invoice reconciliation failed").
				WithReportableDetails(map[string]any{
					"flexprice_payment_id": params.FlexpricePaymentID,
					"invoice_id":           invoiceID,
				}).
				Mark(ierr.ErrSystem)
		}

		l.logger.Info(ctx, "invoice reconciled after payment success",
			"flexprice_payment_id", params.FlexpricePaymentID,
			"invoice_id", invoiceID,
		)
	}

	return nil
}

// RecordPaymentFailure transitions the payment to FAILED. Called from the
// webhook handler on payment_failed or equivalent gateway event.
//
// Idempotent: if the payment is already FAILED, logs and returns nil.
func (l *PaymentsLedger) RecordPaymentFailure(ctx context.Context, params RecordPaymentFailureParams) error {
	l.logger.Error(ctx, "recording payment failure",
		"flexprice_payment_id", params.FlexpricePaymentID,
		"gateway_payment_id", params.GatewayPaymentID,
		"error_message", params.ErrorMessage,
	)

	if params.FlexpricePaymentID == "" {
		return ierr.NewError("flexprice_payment_id is required").Mark(ierr.ErrValidation)
	}

	existing, err := l.paymentService.GetPayment(ctx, params.FlexpricePaymentID)
	if err != nil {
		l.logger.Error(ctx, "failed to get payment for failure recording",
			"flexprice_payment_id", params.FlexpricePaymentID,
			"error", err,
		)
		return ierr.WithError(err).
			WithHint("Failed to retrieve payment").
			WithReportableDetails(map[string]any{
				"flexprice_payment_id": params.FlexpricePaymentID,
			}).
			Mark(ierr.ErrSystem)
	}

	if existing.PaymentStatus == types.PaymentStatusFailed {
		l.logger.Info(ctx, "payment already failed, skipping",
			"flexprice_payment_id", params.FlexpricePaymentID,
			"gateway_payment_id", params.GatewayPaymentID,
		)
		return nil
	}
	if existing.PaymentStatus.IsTerminal() {
		return ierr.NewError("payment is in a terminal state").
			WithHint("Cannot transition to failed from current state").
			WithReportableDetails(map[string]any{
				"flexprice_payment_id": params.FlexpricePaymentID,
				"current_status":       existing.PaymentStatus,
				"destination_type":     existing.DestinationType,
			}).
			Mark(ierr.ErrValidation)
	}

	failedAt := params.FailedAt
	if failedAt.IsZero() {
		failedAt = time.Now().UTC()
	}
	updateReq := apidto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusFailed)),
		GatewayPaymentID: lo.ToPtr(params.GatewayPaymentID),
		FailedAt:         lo.ToPtr(failedAt),
		ErrorMessage:     lo.ToPtr(params.ErrorMessage),
	}

	_, err = l.paymentService.UpdatePayment(ctx, params.FlexpricePaymentID, updateReq)
	if err != nil {
		l.logger.Error(ctx, "failed to update payment to failed",
			"flexprice_payment_id", params.FlexpricePaymentID,
			"gateway_payment_id", params.GatewayPaymentID,
			"error", err,
		)
		return ierr.WithError(err).
			WithHint("Failed to record payment failure").
			WithReportableDetails(map[string]any{
				"flexprice_payment_id": params.FlexpricePaymentID,
				"gateway_payment_id":   params.GatewayPaymentID,
			}).
			Mark(ierr.ErrSystem)
	}

	l.logger.Error(ctx, "payment failed",
		"flexprice_payment_id", params.FlexpricePaymentID,
		"gateway_payment_id", params.GatewayPaymentID,
		"destination_type", existing.DestinationType,
		"destination_id", existing.DestinationID,
		"amount", existing.Amount.String(),
		"currency", existing.Currency,
		"error_message", params.ErrorMessage,
	)

	return nil
}

// RecordPaymentVoided transitions a SUCCEEDED payment to VOIDED — the charge
// was reversed at the gateway after the token was saved.
//
// Idempotent: if the payment is already VOIDED, logs and returns nil.
func (l *PaymentsLedger) RecordPaymentVoided(ctx context.Context, params RecordPaymentVoidedParams) error {
	l.logger.Info(ctx, "recording payment voided",
		"flexprice_payment_id", params.FlexpricePaymentID,
		"gateway_payment_id", params.GatewayPaymentID,
	)

	if params.FlexpricePaymentID == "" {
		return ierr.NewError("flexprice_payment_id is required").Mark(ierr.ErrValidation)
	}

	existing, err := l.paymentService.GetPayment(ctx, params.FlexpricePaymentID)
	if err != nil {
		l.logger.Error(ctx, "failed to get payment for void recording",
			"flexprice_payment_id", params.FlexpricePaymentID,
			"error", err,
		)
		return ierr.WithError(err).
			WithHint("Failed to retrieve payment").
			WithReportableDetails(map[string]any{
				"flexprice_payment_id": params.FlexpricePaymentID,
			}).
			Mark(ierr.ErrSystem)
	}

	if existing.PaymentStatus == types.PaymentStatusVoided {
		l.logger.Info(ctx, "payment already voided, skipping",
			"flexprice_payment_id", params.FlexpricePaymentID,
		)
		return nil
	}
	if existing.PaymentStatus != types.PaymentStatusSucceeded {
		return ierr.NewError("payment must be in SUCCEEDED state to be voided").
			WithHint("Only a succeeded AUTH payment can be voided").
			WithReportableDetails(map[string]any{
				"flexprice_payment_id": params.FlexpricePaymentID,
				"current_status":       existing.PaymentStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	voidedAt := params.VoidedAt
	if voidedAt.IsZero() {
		voidedAt = time.Now().UTC()
	}
	updateReq := apidto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusVoided)),
		GatewayPaymentID: lo.ToPtr(params.GatewayPaymentID),
		VoidedAt:         lo.ToPtr(voidedAt),
	}

	_, err = l.paymentService.UpdatePayment(ctx, params.FlexpricePaymentID, updateReq)
	if err != nil {
		l.logger.Error(ctx, "failed to update payment to voided",
			"flexprice_payment_id", params.FlexpricePaymentID,
			"gateway_payment_id", params.GatewayPaymentID,
			"error", err,
		)
		return ierr.WithError(err).
			WithHint("Failed to record payment void").
			WithReportableDetails(map[string]any{
				"flexprice_payment_id": params.FlexpricePaymentID,
				"gateway_payment_id":   params.GatewayPaymentID,
			}).
			Mark(ierr.ErrSystem)
	}

	l.logger.Info(ctx, "payment voided",
		"flexprice_payment_id", params.FlexpricePaymentID,
		"gateway_payment_id", params.GatewayPaymentID,
		"destination_type", existing.DestinationType,
		"destination_id", existing.DestinationID,
	)

	return nil
}

// RecordPaymentRefunded transitions a SUCCEEDED payment to REFUNDED — the charge
// was refunded at the gateway after the token was saved.
//
// Idempotent: if the payment is already REFUNDED, logs and returns nil.
func (l *PaymentsLedger) RecordPaymentRefunded(ctx context.Context, params RecordPaymentRefundedParams) error {
	l.logger.Info(ctx, "recording payment refunded",
		"flexprice_payment_id", params.FlexpricePaymentID,
		"gateway_payment_id", params.GatewayPaymentID,
		"refunded_at", params.RefundedAt,
	)

	if params.FlexpricePaymentID == "" {
		return ierr.NewError("flexprice_payment_id is required").Mark(ierr.ErrValidation)
	}

	existing, err := l.paymentService.GetPayment(ctx, params.FlexpricePaymentID)
	if err != nil {
		l.logger.Error(ctx, "failed to get payment for refund recording",
			"flexprice_payment_id", params.FlexpricePaymentID,
			"error", err,
		)
		return ierr.WithError(err).
			WithHint("Failed to retrieve payment").
			WithReportableDetails(map[string]any{
				"flexprice_payment_id": params.FlexpricePaymentID,
			}).
			Mark(ierr.ErrSystem)
	}

	if existing.PaymentStatus == types.PaymentStatusRefunded {
		l.logger.Info(ctx, "payment already refunded, skipping",
			"flexprice_payment_id", params.FlexpricePaymentID,
		)
		return nil
	}
	if existing.PaymentStatus != types.PaymentStatusSucceeded {
		return ierr.NewError("payment must be in SUCCEEDED state to be refunded").
			WithHint("Only a succeeded AUTH payment can be refunded").
			WithReportableDetails(map[string]any{
				"flexprice_payment_id": params.FlexpricePaymentID,
				"current_status":       existing.PaymentStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	refundedAt := params.RefundedAt
	if refundedAt.IsZero() {
		refundedAt = time.Now().UTC()
	}

	updateReq := apidto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusRefunded)),
		GatewayPaymentID: lo.ToPtr(params.GatewayPaymentID),
		RefundedAt:       lo.ToPtr(refundedAt),
	}

	_, err = l.paymentService.UpdatePayment(ctx, params.FlexpricePaymentID, updateReq)
	if err != nil {
		l.logger.Error(ctx, "failed to update payment to refunded",
			"flexprice_payment_id", params.FlexpricePaymentID,
			"gateway_payment_id", params.GatewayPaymentID,
			"error", err,
		)
		return ierr.WithError(err).
			WithHint("Failed to record payment refund").
			WithReportableDetails(map[string]any{
				"flexprice_payment_id": params.FlexpricePaymentID,
				"gateway_payment_id":   params.GatewayPaymentID,
			}).
			Mark(ierr.ErrSystem)
	}

	l.logger.Info(ctx, "payment refunded",
		"flexprice_payment_id", params.FlexpricePaymentID,
		"gateway_payment_id", params.GatewayPaymentID,
		"destination_type", existing.DestinationType,
		"destination_id", existing.DestinationID,
		"amount", existing.Amount.String(),
		"refunded_at", refundedAt,
	)

	return nil
}

// validateInitiateParams checks all required fields before creating a payment record.
func (l *PaymentsLedger) validateInitiateParams(params InitiatePaymentParams) error {
	if err := params.DestinationType.Validate(); err != nil {
		return ierr.NewError("invalid destination_type").Mark(ierr.ErrValidation)
	}
	if params.DestinationID == "" {
		return ierr.NewError("destination_id is required").Mark(ierr.ErrValidation)
	}
	if params.Gateway == "" {
		return ierr.NewError("gateway is required").Mark(ierr.ErrValidation)
	}
	if params.Amount.IsZero() || params.Amount.IsNegative() {
		return ierr.NewError("amount must be positive").Mark(ierr.ErrValidation)
	}
	if params.Currency == "" {
		return ierr.NewError("currency is required").Mark(ierr.ErrValidation)
	}
	return nil
}
