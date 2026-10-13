package stripe

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type fakeRefundLock struct{ acquired bool }

func (l *fakeRefundLock) AcquiredSuccessfully() bool      { return l.acquired }
func (l *fakeRefundLock) Release(_ context.Context) error { return nil }

type fakeRefundLocker struct{ acquired bool }

func (l *fakeRefundLocker) AcquireLock(_ context.Context, _ string, _ time.Duration) (cache.Lock, error) {
	return &fakeRefundLock{acquired: l.acquired}, nil
}

type fakeLateCapturePaymentSvc struct {
	interfaces.PaymentService
	payment    *dto.PaymentResponse
	getCalls   int
	updateReqs []dto.UpdatePaymentRequest
}

func (f *fakeLateCapturePaymentSvc) GetPayment(_ context.Context, _ string) (*dto.PaymentResponse, error) {
	f.getCalls++
	return f.payment, nil
}

func (f *fakeLateCapturePaymentSvc) UpdatePayment(_ context.Context, _ string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error) {
	f.updateReqs = append(f.updateReqs, req)
	return f.payment, nil
}

func newLateCapturePayment(status types.PaymentStatus) *dto.PaymentResponse {
	return &dto.PaymentResponse{
		ID:            "pay_001",
		PaymentStatus: status,
		Metadata:      types.Metadata{"source": "checkout"},
	}
}

func TestRefundLateCapturedPayment_SkipsWhenLockHeld(t *testing.T) {
	s := &PaymentService{locker: &fakeRefundLocker{acquired: false}, logger: logger.NewNoopLogger()}
	paymentSvc := &fakeLateCapturePaymentSvc{payment: newLateCapturePayment(types.PaymentStatusPending)}

	err := s.RefundLateCapturedPayment(testContext(), "pay_001", "pi_001", paymentSvc)

	require.NoError(t, err)
	require.Zero(t, paymentSvc.getCalls)
	require.Empty(t, paymentSvc.updateReqs)
}

func TestRefundLateCapturedPayment_SkipsWhenAlreadyRefunded(t *testing.T) {
	for _, status := range []types.PaymentStatus{types.PaymentStatusRefunded, types.PaymentStatusPartiallyRefunded} {
		t.Run(string(status), func(t *testing.T) {
			// client is nil: reaching Stripe would panic, so a pass proves no refund was submitted.
			s := &PaymentService{locker: &fakeRefundLocker{acquired: true}, logger: logger.NewNoopLogger()}
			paymentSvc := &fakeLateCapturePaymentSvc{payment: newLateCapturePayment(status)}

			err := s.RefundLateCapturedPayment(testContext(), "pay_001", "pi_001", paymentSvc)

			require.NoError(t, err)
			require.Empty(t, paymentSvc.updateReqs)
		})
	}
}

func TestRecordLateCaptureRefund_PendingSettlesBeforeRefund(t *testing.T) {
	s := &PaymentService{logger: logger.NewNoopLogger()}
	existing := newLateCapturePayment(types.PaymentStatusPending)
	paymentSvc := &fakeLateCapturePaymentSvc{payment: existing}

	err := s.recordLateCaptureRefund(testContext(), existing, "pi_001", "re_001", paymentSvc)

	require.NoError(t, err)
	require.Len(t, paymentSvc.updateReqs, 2)
	require.Equal(t, string(types.PaymentStatusSucceeded), *paymentSvc.updateReqs[0].PaymentStatus)
	require.NotNil(t, paymentSvc.updateReqs[0].SucceededAt)
	refundReq := paymentSvc.updateReqs[1]
	require.Equal(t, string(types.PaymentStatusRefunded), *refundReq.PaymentStatus)
	require.NotNil(t, refundReq.RefundedAt)
	require.Equal(t, "re_001", (*refundReq.Metadata)["stripe_refund_id"])
	require.Equal(t, "checkout", (*refundReq.Metadata)["source"])
}

func TestRecordLateCaptureRefund_SucceededRefundsDirectly(t *testing.T) {
	s := &PaymentService{logger: logger.NewNoopLogger()}
	existing := newLateCapturePayment(types.PaymentStatusSucceeded)
	paymentSvc := &fakeLateCapturePaymentSvc{payment: existing}

	err := s.recordLateCaptureRefund(testContext(), existing, "pi_001", "re_001", paymentSvc)

	require.NoError(t, err)
	require.Len(t, paymentSvc.updateReqs, 1)
	require.Equal(t, string(types.PaymentStatusRefunded), *paymentSvc.updateReqs[0].PaymentStatus)
}

func TestRecordLateCaptureRefund_FinalStatusRecordsReferenceOnly(t *testing.T) {
	s := &PaymentService{logger: logger.NewNoopLogger()}
	existing := newLateCapturePayment(types.PaymentStatusFailed)
	paymentSvc := &fakeLateCapturePaymentSvc{payment: existing}

	err := s.recordLateCaptureRefund(testContext(), existing, "pi_001", "re_001", paymentSvc)

	require.NoError(t, err)
	require.Len(t, paymentSvc.updateReqs, 1)
	require.Nil(t, paymentSvc.updateReqs[0].PaymentStatus)
	require.Equal(t, "re_001", (*paymentSvc.updateReqs[0].Metadata)["stripe_refund_id"])
}

func TestRefundIdempotencyKeyIsStable(t *testing.T) {
	ctx := testContext()
	k1 := refundIdempotencyKey(ctx, "pay_001")
	k2 := refundIdempotencyKey(ctx, "pay_001")
	require.NotEmpty(t, k1)
	require.Equal(t, k1, k2)
	require.True(t, strings.HasPrefix(k1, "refund-"))

	k3 := refundIdempotencyKey(ctx, "pay_002")
	require.NotEqual(t, k1, k3)
}

func TestChargeIdempotencyKeyIsStablePerPayment(t *testing.T) {
	ctx := testContext()
	k1 := chargeIdempotencyKey(ctx, "pay_001")
	require.NotEmpty(t, k1)
	require.Equal(t, k1, chargeIdempotencyKey(ctx, "pay_001"))
	require.True(t, strings.HasPrefix(k1, "payment-"))

	require.NotEqual(t, k1, chargeIdempotencyKey(ctx, "pay_002"))
	require.NotEqual(t, k1, refundIdempotencyKey(ctx, "pay_001"), "a charge and its refund must not share a key")
}
