package razorpay

// Tests for PaymentService.RefundLateCapturedPayment and ensureRefunded.
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

type refundTestLock struct{ acquired bool }

func (l *refundTestLock) AcquiredSuccessfully() bool      { return l.acquired }
func (l *refundTestLock) Release(_ context.Context) error { return nil }

type refundTestLocker struct {
	acquireErr error
	acquired   bool
}

func (l *refundTestLocker) AcquireLock(_ context.Context, _ string, _ time.Duration) (cache.Lock, error) {
	if l.acquireErr != nil {
		return nil, l.acquireErr
	}
	return &refundTestLock{acquired: l.acquired}, nil
}

// ── fake RazorpayClient (RefundPayment + FetchPayment exercised) ────────────

type refundCall struct {
	paymentID   string
	amountPaise int64
}

type refundTestClient struct {
	RazorpayClient // embedded nil interface — unused methods panic if called

	fetchResp map[string]interface{}
	fetchErr  error

	refundResp  map[string]interface{}
	refundErr   error
	refundCalls []refundCall
}

func (c *refundTestClient) FetchPayment(_ context.Context, _ string) (map[string]interface{}, error) {
	if c.fetchErr != nil {
		return nil, c.fetchErr
	}
	return c.fetchResp, nil
}

func (c *refundTestClient) RefundPayment(_ context.Context, paymentID string, amountPaise int64) (map[string]interface{}, error) {
	c.refundCalls = append(c.refundCalls, refundCall{paymentID, amountPaise})
	if c.refundErr != nil {
		return nil, c.refundErr
	}
	return c.refundResp, nil
}

// ── fake interfaces.PaymentService (GetPayment/UpdatePayment exercised) ─────

type refundTestPaymentService struct {
	interfaces.PaymentService // embedded nil interface — unused methods panic if called

	payment     *dto.PaymentResponse
	getErr      error
	updateErr   error
	updateCalls []dto.UpdatePaymentRequest
}

func (s *refundTestPaymentService) GetPayment(_ context.Context, _ string) (*dto.PaymentResponse, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.payment, nil
}

func (s *refundTestPaymentService) UpdatePayment(_ context.Context, _ string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error) {
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

type refundTestMappingStore struct {
	created []*entityintegrationmapping.EntityIntegrationMapping
}

func (s *refundTestMappingStore) Create(_ context.Context, m *entityintegrationmapping.EntityIntegrationMapping) error {
	s.created = append(s.created, m)
	return nil
}
func (s *refundTestMappingStore) Get(_ context.Context, _ string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
}
func (s *refundTestMappingStore) List(_ context.Context, _ *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	return nil, nil
}
func (s *refundTestMappingStore) Count(_ context.Context, _ *types.EntityIntegrationMappingFilter) (int, error) {
	return 0, nil
}
func (s *refundTestMappingStore) Update(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}
func (s *refundTestMappingStore) Delete(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}

// ── test suite ───────────────────────────────────────────────────────────────

type RefundLateCapturedPaymentSuite struct {
	suite.Suite
	ctx         context.Context
	svc         *PaymentService
	client      *refundTestClient
	paymentSvc  *refundTestPaymentService
	mappingRepo *refundTestMappingStore
}

func TestRefundLateCapturedPayment(t *testing.T) {
	suite.Run(t, new(RefundLateCapturedPaymentSuite))
}

func (s *RefundLateCapturedPaymentSuite) SetupTest() {
	s.ctx = types.SetTenantID(context.Background(), "tenant_test")
	s.ctx = types.SetEnvironmentID(s.ctx, "env_test")

	s.client = &refundTestClient{
		fetchResp:  map[string]interface{}{"refunded": false},
		refundResp: map[string]interface{}{"id": "rfnd_test001"},
	}
	s.paymentSvc = &refundTestPaymentService{
		payment: &dto.PaymentResponse{
			ID:            "pay_flex_001",
			Amount:        decimal.NewFromFloat(500.00),
			Currency:      "INR",
			PaymentStatus: types.PaymentStatusInitiated,
		},
	}
	s.mappingRepo = &refundTestMappingStore{}

	s.svc = &PaymentService{
		client:                       s.client,
		locker:                       &refundTestLocker{acquired: true},
		entityIntegrationMappingRepo: s.mappingRepo,
		logger:                       logger.NewNoopLogger(),
	}
}

func (s *RefundLateCapturedPaymentSuite) refund() {
	s.svc.RefundLateCapturedPayment(s.ctx, "pay_flex_001", "pay_rzp_001", s.paymentSvc)
}

func (s *RefundLateCapturedPaymentSuite) TestLockNotAcquired_Skips() {
	s.svc.locker = &refundTestLocker{acquired: false}
	s.refund()
	s.Empty(s.client.refundCalls, "refund must not be attempted when the lock is held elsewhere")
}

func (s *RefundLateCapturedPaymentSuite) TestAcquireLockError_Skips() {
	s.svc.locker = &refundTestLocker{acquireErr: ierr.NewError("redis down").Mark(ierr.ErrInternal)}
	s.refund()
	s.Empty(s.client.refundCalls, "refund must not be attempted when lock acquisition itself errors")
}

func (s *RefundLateCapturedPaymentSuite) TestGetPaymentError_Skips() {
	s.paymentSvc.getErr = ierr.NewError("db down").Mark(ierr.ErrDatabase)
	s.refund()
	s.Empty(s.client.refundCalls, "refund must not be attempted when the payment record can't be fetched")
}

func (s *RefundLateCapturedPaymentSuite) TestAlreadyRefunded_Skips() {
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusRefunded
	s.refund()
	s.Empty(s.client.refundCalls, "already-refunded payment must not be refunded again")
}

func (s *RefundLateCapturedPaymentSuite) TestAlreadyPartiallyRefunded_Skips() {
	s.paymentSvc.payment.PaymentStatus = types.PaymentStatusPartiallyRefunded
	s.refund()
	s.Empty(s.client.refundCalls, "partially-refunded payment must not be refunded again")
}

func (s *RefundLateCapturedPaymentSuite) TestSuccess_RefundsAndUpdatesAndCreatesMapping() {
	s.refund()

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

func (s *RefundLateCapturedPaymentSuite) TestFetchPaymentError_ProceedsWithRefundAnyway() {
	s.client.fetchErr = ierr.NewError("razorpay unreachable").Mark(ierr.ErrInternal)
	s.refund()

	s.Require().Len(s.client.refundCalls, 1, "a failed pre-check must not block the refund attempt")
	s.Equal(types.PaymentStatusRefunded, s.paymentSvc.payment.PaymentStatus)
}

func (s *RefundLateCapturedPaymentSuite) TestAlreadyRefundedAtGateway_SkipsDuplicateSubmitButUpdatesLocalStatus() {
	// Simulates: a prior attempt refunded successfully at Razorpay but crashed
	// before persisting that locally (payment record still shows Initiated).
	s.client.fetchResp = map[string]interface{}{"refunded": true}
	s.refund()

	s.Empty(s.client.refundCalls, "must not submit a second refund when Razorpay already shows one")
	s.Equal(types.PaymentStatusRefunded, s.paymentSvc.payment.PaymentStatus, "local status must still be reconciled")
	s.Empty(s.mappingRepo.created, "no refund ID is available when skipping duplicate submission, so no mapping is created")
}

func (s *RefundLateCapturedPaymentSuite) TestGatewayError_LogsAndSkipsUpdate() {
	s.client.refundErr = ierr.NewError("gateway down").Mark(ierr.ErrInternal)
	s.refund()

	s.Empty(s.paymentSvc.updateCalls, "payment status must not change when the refund call fails")
	s.Empty(s.mappingRepo.created)
}

func (s *RefundLateCapturedPaymentSuite) TestUpdatePaymentError_LogsWithoutPanic() {
	s.paymentSvc.updateErr = ierr.NewError("db down").Mark(ierr.ErrDatabase)
	s.refund()

	s.Require().Len(s.client.refundCalls, 1, "refund was already submitted to Razorpay")
	s.Empty(s.mappingRepo.created, "mapping is only created after a successful status update")
}
