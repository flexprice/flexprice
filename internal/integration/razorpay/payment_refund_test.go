package razorpay

// Tests for PaymentService.RefundLateCapturedPayment.
//
// This file is in package razorpay (not razorpay_test) to access the unexported
// PaymentService.locker/entityIntegrationMappingRepo fields directly in test setup,
// and to avoid the same import cycle documented in invoice_autocharge_test.go:
//
//   razorpay [test] → testutil → integration → razorpay
//
// It uses hand-written fakes instead of testutil.

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ── fake cache.Locker ───────────────────────────────────────────────────────

type stubLock struct{ acquired bool }

func (l *stubLock) AcquiredSuccessfully() bool      { return l.acquired }
func (l *stubLock) Release(_ context.Context) error { return nil }

type stubLocker struct {
	acquireErr error
	acquired   bool
}

func (l *stubLocker) AcquireLock(_ context.Context, _ string, _ time.Duration) (cache.Lock, error) {
	if l.acquireErr != nil {
		return nil, l.acquireErr
	}
	return &stubLock{acquired: l.acquired}, nil
}

// ── fake RazorpayClient (only RefundPayment is exercised) ──────────────────

type refundCall struct {
	paymentID   string
	amountPaise int64
}

type stubRazorpayClient struct {
	RazorpayClient // embedded nil interface — unused methods panic if called
	refundResp     map[string]interface{}
	refundErr      error
	refundCalls    []refundCall
}

func (c *stubRazorpayClient) RefundPayment(_ context.Context, paymentID string, amountPaise int64) (map[string]interface{}, error) {
	c.refundCalls = append(c.refundCalls, refundCall{paymentID, amountPaise})
	if c.refundErr != nil {
		return nil, c.refundErr
	}
	return c.refundResp, nil
}

// ── fake interfaces.PaymentService (only GetPayment/UpdatePayment exercised) ─

type stubPaymentService struct {
	interfaces.PaymentService // embedded nil interface — unused methods panic if called
	payment                   *dto.PaymentResponse
	getErr                    error
	updateErr                 error
	updateCalls               []dto.UpdatePaymentRequest
}

func (s *stubPaymentService) GetPayment(_ context.Context, _ string) (*dto.PaymentResponse, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.payment, nil
}

func (s *stubPaymentService) UpdatePayment(_ context.Context, _ string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error) {
	s.updateCalls = append(s.updateCalls, req)
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	if req.PaymentStatus != nil {
		s.payment.PaymentStatus = types.PaymentStatus(*req.PaymentStatus)
	}
	if req.RefundedAt != nil {
		s.payment.RefundedAt = req.RefundedAt
	}
	if req.GatewayPaymentID != nil {
		s.payment.GatewayPaymentID = req.GatewayPaymentID
	}
	return s.payment, nil
}

// ── inline entityintegrationmapping.Repository fake ─────────────────────────

type inlineMappingStore struct {
	created []*entityintegrationmapping.EntityIntegrationMapping
}

func (s *inlineMappingStore) Create(_ context.Context, m *entityintegrationmapping.EntityIntegrationMapping) error {
	s.created = append(s.created, m)
	return nil
}
func (s *inlineMappingStore) Get(_ context.Context, _ string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
}
func (s *inlineMappingStore) List(_ context.Context, _ *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	return nil, nil
}
func (s *inlineMappingStore) Count(_ context.Context, _ *types.EntityIntegrationMappingFilter) (int, error) {
	return 0, nil
}
func (s *inlineMappingStore) Update(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}
func (s *inlineMappingStore) Delete(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}

// ── test suite ───────────────────────────────────────────────────────────────

type RefundLateCapturedPaymentSuite struct {
	suite.Suite
	ctx         context.Context
	svc         *PaymentService
	client      *stubRazorpayClient
	paymentSvc  *stubPaymentService
	mappingRepo *inlineMappingStore
}

func TestRefundLateCapturedPayment(t *testing.T) {
	suite.Run(t, new(RefundLateCapturedPaymentSuite))
}

func (s *RefundLateCapturedPaymentSuite) SetupTest() {
	s.ctx = types.SetTenantID(context.Background(), "tenant_test")
	s.ctx = types.SetEnvironmentID(s.ctx, "env_test")

	s.client = &stubRazorpayClient{refundResp: map[string]interface{}{"id": "rfnd_test001"}}
	s.paymentSvc = &stubPaymentService{
		payment: &dto.PaymentResponse{
			ID:            "pay_flex_001",
			Amount:        decimal.NewFromFloat(500.00),
			Currency:      "INR",
			PaymentStatus: types.PaymentStatusSucceeded,
		},
	}
	s.mappingRepo = &inlineMappingStore{}

	s.svc = &PaymentService{
		client:                       s.client,
		locker:                       &stubLocker{acquired: true},
		entityIntegrationMappingRepo: s.mappingRepo,
		logger:                       logger.NewNoopLogger(),
	}
}

func (s *RefundLateCapturedPaymentSuite) TestNoLocker_Skips() {
	s.svc.locker = nil
	err := s.svc.RefundLateCapturedPayment(s.ctx, "pay_flex_001", "pay_rzp_001", s.paymentSvc)
	s.NoError(err)
	s.Empty(s.client.refundCalls, "refund must not be attempted without a locker")
}

func (s *RefundLateCapturedPaymentSuite) TestLockNotAcquired_Skips() {
	s.svc.locker = &stubLocker{acquired: false}
	err := s.svc.RefundLateCapturedPayment(s.ctx, "pay_flex_001", "pay_rzp_001", s.paymentSvc)
	s.NoError(err)
	s.Empty(s.client.refundCalls, "refund must not be attempted when the lock is held elsewhere")
}

func (s *RefundLateCapturedPaymentSuite) TestAcquireLockError_Skips() {
	s.svc.locker = &stubLocker{acquireErr: ierr.NewError("redis down").Mark(ierr.ErrInternal)}
	err := s.svc.RefundLateCapturedPayment(s.ctx, "pay_flex_001", "pay_rzp_001", s.paymentSvc)
	s.NoError(err)
	s.Empty(s.client.refundCalls, "refund must not be attempted when lock acquisition itself errors")
}

func (s *RefundLateCapturedPaymentSuite) TestAlreadyRefunded_Skips() {
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusRefunded
	err := s.svc.RefundLateCapturedPayment(s.ctx, "pay_flex_001", "pay_rzp_001", s.paymentSvc)
	s.NoError(err)
	s.Empty(s.client.refundCalls, "already-refunded payment must not be refunded again")
}

func (s *RefundLateCapturedPaymentSuite) TestAlreadyPartiallyRefunded_Skips() {
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPartiallyRefunded
	err := s.svc.RefundLateCapturedPayment(s.ctx, "pay_flex_001", "pay_rzp_001", s.paymentSvc)
	s.NoError(err)
	s.Empty(s.client.refundCalls, "partially-refunded payment must not be refunded again")
}

func (s *RefundLateCapturedPaymentSuite) TestGetPaymentError_Skips() {
	s.paymentSvc.getErr = ierr.NewError("db down").Mark(ierr.ErrDatabase)
	err := s.svc.RefundLateCapturedPayment(s.ctx, "pay_flex_001", "pay_rzp_001", s.paymentSvc)
	s.NoError(err)
	s.Empty(s.client.refundCalls, "refund must not be attempted when the payment record can't be fetched")
}

func (s *RefundLateCapturedPaymentSuite) TestSuccess_RefundsAndUpdatesAndCreatesMapping() {
	err := s.svc.RefundLateCapturedPayment(s.ctx, "pay_flex_001", "pay_rzp_001", s.paymentSvc)
	s.NoError(err)

	s.Require().Len(s.client.refundCalls, 1)
	s.Equal("pay_rzp_001", s.client.refundCalls[0].paymentID)
	s.Equal(int64(50000), s.client.refundCalls[0].amountPaise, "500.00 INR = 50000 paise")

	s.Equal(types.PaymentStatusRefunded, s.paymentSvc.payment.PaymentStatus)
	s.NotNil(s.paymentSvc.payment.RefundedAt)
	s.Require().NotNil(s.paymentSvc.payment.GatewayPaymentID)
	s.Equal("pay_rzp_001", *s.paymentSvc.payment.GatewayPaymentID)

	s.Require().Len(s.mappingRepo.created, 1)
	s.Equal("rfnd_test001", s.mappingRepo.created[0].ProviderEntityID)
	s.Equal("pay_flex_001", s.mappingRepo.created[0].EntityID)
}

func (s *RefundLateCapturedPaymentSuite) TestGatewayError_LogsAndSkipsUpdate() {
	s.client.refundErr = ierr.NewError("gateway down").Mark(ierr.ErrInternal)
	err := s.svc.RefundLateCapturedPayment(s.ctx, "pay_flex_001", "pay_rzp_001", s.paymentSvc)
	s.NoError(err, "gateway errors are logged, not propagated")
	s.Empty(s.paymentSvc.updateCalls, "payment status must not change when the refund call fails")
	s.Empty(s.mappingRepo.created)
}

func (s *RefundLateCapturedPaymentSuite) TestUpdatePaymentError_LogsWithoutPanic() {
	s.paymentSvc.updateErr = ierr.NewError("db down").Mark(ierr.ErrDatabase)
	err := s.svc.RefundLateCapturedPayment(s.ctx, "pay_flex_001", "pay_rzp_001", s.paymentSvc)
	s.NoError(err)
	s.Require().Len(s.client.refundCalls, 1, "refund was already submitted to Razorpay")
	s.Empty(s.mappingRepo.created, "mapping is only created after a successful status update")
}
