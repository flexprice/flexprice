package chargebee

import (
	"context"
	"errors"
	"testing"
	"time"

	transactionModel "github.com/chargebee/chargebee-go/v3/models/transaction"
	transactionEnum "github.com/chargebee/chargebee-go/v3/models/transaction/enum"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type lateCaptureTestLock struct{}

func (lateCaptureTestLock) AcquiredSuccessfully() bool    { return true }
func (lateCaptureTestLock) Release(context.Context) error { return nil }

type lateCaptureTestLocker struct{}

func (lateCaptureTestLocker) AcquireLock(context.Context, string, time.Duration) (cache.Lock, error) {
	return lateCaptureTestLock{}, nil
}

type lateCaptureRefundCall struct {
	transactionID  string
	amountMinor    int64
	idempotencyKey string
}

type lateCaptureTestClient struct {
	ChargebeeClient
	transaction   *transactionModel.Transaction
	refundID      string
	refundErr     error
	retrieveCalls int
	refundCalls   []lateCaptureRefundCall
}

func (c *lateCaptureTestClient) RetrieveTransaction(context.Context, string) (*transactionModel.Transaction, error) {
	c.retrieveCalls++
	return cloneChargebeeTransaction(c.transaction), nil
}

func (c *lateCaptureTestClient) RefundTransaction(_ context.Context, transactionID string, amountMinor int64, idempotencyKey string) (*transactionModel.Transaction, error) {
	c.refundCalls = append(c.refundCalls, lateCaptureRefundCall{
		transactionID:  transactionID,
		amountMinor:    amountMinor,
		idempotencyKey: idempotencyKey,
	})
	if c.refundErr != nil {
		return nil, c.refundErr
	}
	if c.refundID == "" {
		c.refundID = "txn_refund_1"
	}
	c.transaction.LinkedRefunds = append(c.transaction.LinkedRefunds, &transactionModel.LinkedRefund{
		TxnId:     c.refundID,
		TxnStatus: transactionEnum.StatusSuccess,
		TxnAmount: amountMinor,
	})
	return &transactionModel.Transaction{Id: c.refundID}, nil
}

type validatingLateCapturePaymentService struct {
	interfaces.PaymentService
	payment        *dto.PaymentResponse
	updateReqs     []dto.UpdatePaymentRequest
	statusChanges  []types.PaymentStatus
	updateAttempts int
	failUpdateAt   int
}

func (s *validatingLateCapturePaymentService) GetPayment(context.Context, dto.GetPaymentRequest) (*dto.PaymentResponse, error) {
	return cloneLateCapturePayment(s.payment), nil
}

func (s *validatingLateCapturePaymentService) UpdatePayment(_ context.Context, _ string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error) {
	s.updateAttempts++
	s.updateReqs = append(s.updateReqs, req)
	if s.updateAttempts == s.failUpdateAt {
		return nil, errors.New("injected payment update failure")
	}
	if req.PaymentStatus != nil {
		target := types.PaymentStatus(*req.PaymentStatus)
		if err := s.payment.PaymentStatus.ValidateTransitionTo(target); err != nil {
			return nil, err
		}
		s.payment.PaymentStatus = target
		s.statusChanges = append(s.statusChanges, target)
	}
	if req.GatewayPaymentID != nil {
		s.payment.GatewayPaymentID = req.GatewayPaymentID
	}
	if req.SucceededAt != nil {
		s.payment.SucceededAt = req.SucceededAt
	}
	if req.RefundedAt != nil {
		s.payment.RefundedAt = req.RefundedAt
	}
	if req.Metadata != nil {
		s.payment.Metadata = cloneMetadata(*req.Metadata)
	}
	return cloneLateCapturePayment(s.payment), nil
}

func newLateCaptureTestServices(status types.PaymentStatus) (*PaymentService, *lateCaptureTestClient, *validatingLateCapturePaymentService) {
	client := &lateCaptureTestClient{
		transaction: &transactionModel.Transaction{Id: "txn_charge_1", Amount: 2500},
	}
	payments := &validatingLateCapturePaymentService{payment: &dto.PaymentResponse{
		ID:            "pay_001",
		Amount:        decimal.NewFromInt(25),
		Currency:      "USD",
		PaymentStatus: status,
		Metadata:      types.Metadata{"source": "checkout"},
	}}
	svc := NewPaymentService(client, lateCaptureTestLocker{}, logger.NewNoopLogger())
	return svc, client, payments
}

func TestRefundLateCapturedPayment_RecordsUnsettledPaymentBeforeRefunded(t *testing.T) {
	for _, status := range []types.PaymentStatus{
		types.PaymentStatusInitiated,
		types.PaymentStatusPending,
		types.PaymentStatusProcessing,
	} {
		t.Run(string(status), func(t *testing.T) {
			svc, client, payments := newLateCaptureTestServices(status)

			err := svc.RefundLateCapturedPayment(context.Background(), "pay_001", "txn_charge_1", payments)

			require.NoError(t, err)
			require.Equal(t, []types.PaymentStatus{types.PaymentStatusSucceeded, types.PaymentStatusRefunded}, payments.statusChanges)
			require.Len(t, payments.updateReqs, 2)
			require.Equal(t, string(types.PaymentStatusSucceeded), *payments.updateReqs[0].PaymentStatus)
			require.NotNil(t, payments.updateReqs[0].SucceededAt)
			require.Equal(t, "txn_charge_1", *payments.updateReqs[0].GatewayPaymentID)
			refunded := payments.updateReqs[1]
			require.Equal(t, string(types.PaymentStatusRefunded), *refunded.PaymentStatus)
			require.NotNil(t, refunded.RefundedAt)
			require.Equal(t, "txn_charge_1", *refunded.GatewayPaymentID)
			require.Equal(t, "txn_refund_1", (*refunded.Metadata)["chargebee_refund_id"])
			require.Equal(t, "checkout", (*refunded.Metadata)["source"])
			require.Equal(t, []lateCaptureRefundCall{{
				transactionID:  "txn_charge_1",
				amountMinor:    2500,
				idempotencyKey: idempotencyScoped("pay_001", "refund"),
			}}, client.refundCalls)
		})
	}
}

func TestRefundLateCapturedPayment_RefundsSettledPaymentDirectly(t *testing.T) {
	for _, status := range []types.PaymentStatus{types.PaymentStatusSucceeded, types.PaymentStatusOverpaid} {
		t.Run(string(status), func(t *testing.T) {
			svc, client, payments := newLateCaptureTestServices(status)

			err := svc.RefundLateCapturedPayment(context.Background(), "pay_001", "txn_charge_1", payments)

			require.NoError(t, err)
			require.Equal(t, []types.PaymentStatus{types.PaymentStatusRefunded}, payments.statusChanges)
			require.Len(t, payments.updateReqs, 1)
			require.Equal(t, string(types.PaymentStatusRefunded), *payments.updateReqs[0].PaymentStatus)
			require.Equal(t, "txn_refund_1", payments.payment.Metadata["chargebee_refund_id"])
			require.Len(t, client.refundCalls, 1)
		})
	}
}

func TestRefundLateCapturedPayment_LeavesTerminalStatusAndRecordsReference(t *testing.T) {
	for _, status := range []types.PaymentStatus{types.PaymentStatusFailed, types.PaymentStatusVoided} {
		t.Run(string(status), func(t *testing.T) {
			svc, client, payments := newLateCaptureTestServices(status)

			err := svc.RefundLateCapturedPayment(context.Background(), "pay_001", "txn_charge_1", payments)

			require.NoError(t, err)
			require.Equal(t, status, payments.payment.PaymentStatus)
			require.Empty(t, payments.statusChanges)
			require.Len(t, payments.updateReqs, 1)
			req := payments.updateReqs[0]
			require.Nil(t, req.PaymentStatus)
			require.Nil(t, req.SucceededAt)
			require.Nil(t, req.RefundedAt)
			require.Equal(t, "txn_charge_1", *req.GatewayPaymentID)
			require.Equal(t, "txn_refund_1", (*req.Metadata)["chargebee_refund_id"])
			require.Equal(t, "checkout", (*req.Metadata)["source"])
			require.Len(t, client.refundCalls, 1)
		})
	}
}

func TestRefundLateCapturedPayment_SkipsAlreadyRefundedPayments(t *testing.T) {
	for _, status := range []types.PaymentStatus{types.PaymentStatusRefunded, types.PaymentStatusPartiallyRefunded} {
		t.Run(string(status), func(t *testing.T) {
			svc, client, payments := newLateCaptureTestServices(status)

			err := svc.RefundLateCapturedPayment(context.Background(), "pay_001", "txn_charge_1", payments)

			require.NoError(t, err)
			require.Zero(t, client.retrieveCalls)
			require.Empty(t, client.refundCalls)
			require.Empty(t, payments.updateReqs)
		})
	}
}

func TestRefundLateCapturedPayment_RepeatDeliveryDoesNotRefundTwice(t *testing.T) {
	svc, client, payments := newLateCaptureTestServices(types.PaymentStatusPending)

	require.NoError(t, svc.RefundLateCapturedPayment(context.Background(), "pay_001", "txn_charge_1", payments))
	require.NoError(t, svc.RefundLateCapturedPayment(context.Background(), "pay_001", "txn_charge_1", payments))

	require.Equal(t, types.PaymentStatusRefunded, payments.payment.PaymentStatus)
	require.Len(t, client.refundCalls, 1)
	require.Equal(t, 1, client.retrieveCalls)
}

func TestRefundLateCapturedPayment_ProviderFailureLeavesPaymentUnchanged(t *testing.T) {
	svc, client, payments := newLateCaptureTestServices(types.PaymentStatusPending)
	client.refundErr = errors.New("Chargebee unavailable")

	err := svc.RefundLateCapturedPayment(context.Background(), "pay_001", "txn_charge_1", payments)

	require.Error(t, err)
	require.Equal(t, types.PaymentStatusPending, payments.payment.PaymentStatus)
	require.Equal(t, types.Metadata{"source": "checkout"}, payments.payment.Metadata)
	require.Empty(t, payments.updateReqs)
	require.Len(t, client.refundCalls, 1)
}

func TestRefundLateCapturedPayment_RetryAfterLocalUpdateFailureReusesProviderRefund(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
	}{
		{name: "capture update", failAt: 1},
		{name: "refund update", failAt: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, client, payments := newLateCaptureTestServices(types.PaymentStatusPending)
			payments.failUpdateAt = tc.failAt

			err := svc.RefundLateCapturedPayment(context.Background(), "pay_001", "txn_charge_1", payments)
			require.Error(t, err)
			require.Len(t, client.refundCalls, 1)
			if tc.failAt == 1 {
				require.Equal(t, types.PaymentStatusPending, payments.payment.PaymentStatus)
			} else {
				require.Equal(t, types.PaymentStatusSucceeded, payments.payment.PaymentStatus)
			}

			payments.failUpdateAt = 0
			err = svc.RefundLateCapturedPayment(context.Background(), "pay_001", "txn_charge_1", payments)

			require.NoError(t, err)
			require.Equal(t, types.PaymentStatusRefunded, payments.payment.PaymentStatus)
			require.Equal(t, "txn_refund_1", payments.payment.Metadata["chargebee_refund_id"])
			require.Len(t, client.refundCalls, 1, "a retry must not submit another provider refund")
			require.Equal(t, 2, client.retrieveCalls)
		})
	}
}

func cloneLateCapturePayment(payment *dto.PaymentResponse) *dto.PaymentResponse {
	if payment == nil {
		return nil
	}
	clone := *payment
	clone.Metadata = cloneMetadata(payment.Metadata)
	return &clone
}

func cloneMetadata(metadata types.Metadata) types.Metadata {
	clone := make(types.Metadata, len(metadata))
	for key, value := range metadata {
		clone[key] = value
	}
	return clone
}

func cloneChargebeeTransaction(txn *transactionModel.Transaction) *transactionModel.Transaction {
	if txn == nil {
		return nil
	}
	clone := *txn
	clone.LinkedRefunds = make([]*transactionModel.LinkedRefund, len(txn.LinkedRefunds))
	for i, linkedRefund := range txn.LinkedRefunds {
		if linkedRefund != nil {
			refund := *linkedRefund
			clone.LinkedRefunds[i] = &refund
		}
	}
	return &clone
}
