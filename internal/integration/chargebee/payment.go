package chargebee

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const refundLockTTL = 15 * time.Minute

// PaymentService owns the Chargebee-side money movements that are not part of
// creating a checkout — today, refunding a payment that arrived too late.
type PaymentService struct {
	client ChargebeeClient
	locker cache.Locker
	logger *logger.Logger
}

func NewPaymentService(client ChargebeeClient, locker cache.Locker, logger *logger.Logger) *PaymentService {
	return &PaymentService{client: client, locker: locker, logger: logger}
}

// RefundLateCapturedPayment refunds a payment that settled after its checkout session
// reached a terminal state. The hosted page (3h) outlives the session (30-minute TTL
// plus the expiry sweep), so a customer really can pay after we have given up: by then
// the invoice and payment have been archived and nothing can be delivered for the money.
func (s *PaymentService) RefundLateCapturedPayment(
	ctx context.Context,
	flexpricePaymentID string,
	chargebeeTransactionID string,
	paymentService interfaces.PaymentService,
) error {
	lockKey := cache.GenerateKey(ctx, cache.PrefixChargebeeWebhookRefundLock, flexpricePaymentID)
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
			s.logger.Error(ctx, "failed to release refund lock", "error", releaseErr, "payment_id", flexpricePaymentID)
		}
	}()

	existingPayment, err := paymentService.GetPayment(ctx, flexpricePaymentID)
	if err != nil {
		return ierr.WithError(err).
			WithMessage("failed to get payment record for refund").
			WithReportableDetails(map[string]interface{}{"payment_id": flexpricePaymentID}).
			Mark(ierr.ErrInternal)
	}
	if existingPayment.PaymentStatus == types.PaymentStatusRefunded ||
		existingPayment.PaymentStatus == types.PaymentStatusPartiallyRefunded {
		s.logger.Info(ctx, "payment already refunded, skipping",
			"payment_id", flexpricePaymentID, "status", existingPayment.PaymentStatus)
		return nil
	}

	refundID, err := s.ensureRefunded(ctx, chargebeeTransactionID,
		amountToMinorUnits(existingPayment.Amount, existingPayment.Currency), flexpricePaymentID)
	if err != nil {
		return ierr.WithError(err).
			WithMessage("failed to refund late-captured payment at Chargebee").
			WithReportableDetails(map[string]interface{}{
				"payment_id":               flexpricePaymentID,
				"chargebee_transaction_id": chargebeeTransactionID,
			}).
			Mark(ierr.ErrInternal)
	}

	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusRefunded)),
		RefundedAt:       lo.ToPtr(time.Now().UTC()),
		GatewayPaymentID: lo.ToPtr(chargebeeTransactionID),
	}
	if refundID != "" {
		metadata := lo.Assign(existingPayment.Metadata, types.Metadata{"chargebee_refund_id": refundID})
		updateReq.Metadata = &metadata
	}
	if _, err := paymentService.UpdatePayment(ctx, flexpricePaymentID, updateReq); err != nil {
		return ierr.WithError(err).
			WithMessage("refund confirmed at Chargebee but failed to update FlexPrice payment status").
			WithReportableDetails(map[string]interface{}{
				"payment_id":          flexpricePaymentID,
				"chargebee_refund_id": refundID,
			}).
			Mark(ierr.ErrInternal)
	}

	s.logger.Info(ctx, "refunded late-captured chargebee payment",
		"payment_id", flexpricePaymentID,
		"chargebee_transaction_id", chargebeeTransactionID,
		"chargebee_refund_id", refundID)
	return nil
}

// ensureRefunded submits the refund unless Chargebee already shows nothing left to
// refund. Returns the refund transaction id, empty when the refund was already done.
func (s *PaymentService) ensureRefunded(ctx context.Context, transactionID string, amountMinor int64, flexpricePaymentID string) (string, error) {
	if txn, err := s.client.RetrieveTransaction(ctx, transactionID); err != nil {
		s.logger.Info(ctx, "failed to read chargebee transaction before refunding, proceeding anyway",
			"chargebee_transaction_id", transactionID, "error", err)
	} else if AmountRefunded(txn) >= txn.Amount {
		s.logger.Info(ctx, "chargebee transaction is already fully refunded, skipping duplicate submission",
			"chargebee_transaction_id", transactionID)
		return "", nil
	}

	refund, err := s.client.RefundTransaction(ctx, transactionID, amountMinor,
		idempotencyScoped(flexpricePaymentID, "refund"))
	if err != nil {
		return "", err
	}
	return refund.Id, nil
}
