package razorpay

// Tests for InvoiceSyncService.findOrCreateAutoChargePayment.
//
// This file is in package razorpay (not razorpay_test) to access the unexported
// method. It does NOT import testutil to avoid an import cycle:
//
//   razorpay [test] → testutil → integration → razorpay
//
// Instead it uses a minimal inline payment store and logger.NewNoopLogger().

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ── minimal inline payment store ────────────────────────────────────────────

type inlinePaymentStore struct {
	mu      sync.RWMutex
	byID    map[string]*payment.Payment
	byIdemp map[string]*payment.Payment
}

func newInlinePaymentStore() *inlinePaymentStore {
	return &inlinePaymentStore{
		byID:    make(map[string]*payment.Payment),
		byIdemp: make(map[string]*payment.Payment),
	}
}

func (s *inlinePaymentStore) Create(_ context.Context, p *payment.Payment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.byIdemp[p.IdempotencyKey]; dup {
		return fmt.Errorf("duplicate idempotency key %s", p.IdempotencyKey)
	}
	s.byID[p.ID] = p
	s.byIdemp[p.IdempotencyKey] = p
	return nil
}

func (s *inlinePaymentStore) GetByIdempotencyKey(_ context.Context, key string) (*payment.Payment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.byIdemp[key]
	if !ok {
		return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
	}
	return p, nil
}

func (s *inlinePaymentStore) Get(_ context.Context, id string) (*payment.Payment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.byID[id]
	if !ok {
		return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
	}
	return p, nil
}

// Stub out the remaining payment.Repository methods (unused by tests here).
func (s *inlinePaymentStore) Update(_ context.Context, p *payment.Payment) error { s.mu.Lock(); s.byID[p.ID] = p; s.mu.Unlock(); return nil }
func (s *inlinePaymentStore) Delete(_ context.Context, _ string) error            { return nil }
func (s *inlinePaymentStore) List(_ context.Context, _ *types.PaymentFilter) ([]*payment.Payment, error) {
	return nil, nil
}
func (s *inlinePaymentStore) Count(_ context.Context, _ *types.PaymentFilter) (int, error) {
	return 0, nil
}
func (s *inlinePaymentStore) CreateAttempt(_ context.Context, _ *payment.PaymentAttempt) error {
	return nil
}
func (s *inlinePaymentStore) GetAttempt(_ context.Context, _ string) (*payment.PaymentAttempt, error) {
	return nil, nil
}
func (s *inlinePaymentStore) UpdateAttempt(_ context.Context, _ *payment.PaymentAttempt) error {
	return nil
}
func (s *inlinePaymentStore) ListAttempts(_ context.Context, _ string) ([]*payment.PaymentAttempt, error) {
	return nil, nil
}
func (s *inlinePaymentStore) GetLatestAttempt(_ context.Context, _ string) (*payment.PaymentAttempt, error) {
	return nil, nil
}
func (s *inlinePaymentStore) ListScopedByDestinationStatusGateway(_ context.Context, _ types.PaymentDestinationType, _ types.PaymentStatus, _ types.PaymentGatewayType) ([]payment.ScopedPayment, error) {
	return nil, nil
}

// ── test suite ───────────────────────────────────────────────────────────────

type AutoChargePaymentSuite struct {
	suite.Suite
	svc         *InvoiceSyncService
	paymentRepo *inlinePaymentStore
	inv         *invoice.Invoice
	ctx         context.Context
}

func TestFindOrCreateAutoChargePayment(t *testing.T) {
	suite.Run(t, new(AutoChargePaymentSuite))
}

func (s *AutoChargePaymentSuite) SetupTest() {
	s.ctx = types.SetTenantID(context.Background(), "tenant_test")
	s.ctx = types.SetEnvironmentID(s.ctx, "env_test")
	s.paymentRepo = newInlinePaymentStore()
	s.svc = &InvoiceSyncService{
		paymentRepo: s.paymentRepo,
		logger:      logger.NewNoopLogger(),
	}
	s.inv = &invoice.Invoice{
		ID:              "inv_autocharge_test_001",
		CustomerID:      "cust_001",
		EnvironmentID:   "env_test",
		AmountRemaining: decimal.NewFromFloat(500.00),
		Currency:        "INR",
		BaseModel:       types.GetDefaultBaseModel(s.ctx),
	}
}

func (s *AutoChargePaymentSuite) makePayment(status types.PaymentStatus) *payment.Payment {
	gatewayType := string(types.PaymentGatewayTypeRazorpay)
	return &payment.Payment{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PAYMENT),
		IdempotencyKey:  "autocharge:" + s.inv.ID,
		DestinationType: types.PaymentDestinationTypeInvoice,
		DestinationID:   s.inv.ID,
		PaymentStatus:   status,
		Amount:          decimal.NewFromFloat(500.00),
		Currency:        "INR",
		EnvironmentID:   "env_test",
		PaymentGateway:  &gatewayType,
		BaseModel:       types.GetDefaultBaseModel(s.ctx),
	}
}

func (s *AutoChargePaymentSuite) TestNoExistingRecord_CreatesInitiated() {
	pymnt, skip, err := s.svc.findOrCreateAutoChargePayment(s.ctx, s.inv, types.PaymentMethodTypeUPI)

	s.NoError(err)
	s.False(skip)
	s.Require().NotNil(pymnt)
	s.Equal(types.PaymentStatusInitiated, pymnt.PaymentStatus)
	s.Equal("autocharge:"+s.inv.ID, pymnt.IdempotencyKey)
	s.Equal(s.inv.ID, pymnt.DestinationID)
}

func (s *AutoChargePaymentSuite) TestExistingInitiated_ReturnsSameNoSkip() {
	existing := s.makePayment(types.PaymentStatusInitiated)
	s.Require().NoError(s.paymentRepo.Create(s.ctx, existing))

	pymnt, skip, err := s.svc.findOrCreateAutoChargePayment(s.ctx, s.inv, types.PaymentMethodTypeUPI)

	s.NoError(err)
	s.False(skip, "INITIATED should not be skipped")
	s.Require().NotNil(pymnt)
	s.Equal(existing.ID, pymnt.ID)
}

func (s *AutoChargePaymentSuite) TestExistingPending_ReturnsSameNoSkip() {
	existing := s.makePayment(types.PaymentStatusPending)
	s.Require().NoError(s.paymentRepo.Create(s.ctx, existing))

	pymnt, skip, err := s.svc.findOrCreateAutoChargePayment(s.ctx, s.inv, types.PaymentMethodTypeUPI)

	s.NoError(err)
	s.False(skip, "PENDING should not be skipped")
	s.Require().NotNil(pymnt)
	s.Equal(existing.ID, pymnt.ID)
}

func (s *AutoChargePaymentSuite) TestExistingProcessing_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.ctx, s.makePayment(types.PaymentStatusProcessing)))
	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.ctx, s.inv, types.PaymentMethodTypeUPI)
	s.NoError(err)
	s.True(skip, "PROCESSING should be skipped")
}

func (s *AutoChargePaymentSuite) TestExistingSucceeded_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.ctx, s.makePayment(types.PaymentStatusSucceeded)))
	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.ctx, s.inv, types.PaymentMethodTypeUPI)
	s.NoError(err)
	s.True(skip, "SUCCEEDED should be skipped")
}

func (s *AutoChargePaymentSuite) TestExistingOverpaid_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.ctx, s.makePayment(types.PaymentStatusOverpaid)))
	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.ctx, s.inv, types.PaymentMethodTypeUPI)
	s.NoError(err)
	s.True(skip, "OVERPAID should be skipped")
}

func (s *AutoChargePaymentSuite) TestExistingFailed_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.ctx, s.makePayment(types.PaymentStatusFailed)))
	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.ctx, s.inv, types.PaymentMethodTypeUPI)
	s.NoError(err)
	s.True(skip, "FAILED should be skipped")
}

func (s *AutoChargePaymentSuite) TestExistingVoided_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.ctx, s.makePayment(types.PaymentStatusVoided)))
	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.ctx, s.inv, types.PaymentMethodTypeUPI)
	s.NoError(err)
	s.True(skip, "VOIDED should be skipped via default branch")
}

func (s *AutoChargePaymentSuite) TestExistingRefunded_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.ctx, s.makePayment(types.PaymentStatusRefunded)))
	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.ctx, s.inv, types.PaymentMethodTypeUPI)
	s.NoError(err)
	s.True(skip, "REFUNDED should be skipped via default branch")
}

func (s *AutoChargePaymentSuite) TestExistingPartiallyRefunded_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.ctx, s.makePayment(types.PaymentStatusPartiallyRefunded)))
	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.ctx, s.inv, types.PaymentMethodTypeUPI)
	s.NoError(err)
	s.True(skip, "PARTIALLY_REFUNDED should be skipped via default branch")
}

func (s *AutoChargePaymentSuite) TestIdempotency_SecondCallReturnsSameRecord() {
	first, skip1, err := s.svc.findOrCreateAutoChargePayment(s.ctx, s.inv, types.PaymentMethodTypeUPI)
	s.Require().NoError(err)
	s.False(skip1)
	s.Require().NotNil(first)

	second, skip2, err := s.svc.findOrCreateAutoChargePayment(s.ctx, s.inv, types.PaymentMethodTypeUPI)
	s.Require().NoError(err)
	s.False(skip2)
	s.Require().NotNil(second)

	s.Equal(first.ID, second.ID, "retry must return the same payment record")
}

func (s *AutoChargePaymentSuite) TestNoExistingRecord_UsesGivenPaymentMethodType() {
	pymnt, skip, err := s.svc.findOrCreateAutoChargePayment(s.ctx, s.inv, types.PaymentMethodTypeCard)

	s.NoError(err)
	s.False(skip)
	s.Require().NotNil(pymnt)
	s.Equal(types.PaymentMethodTypeCard, pymnt.PaymentMethodType)
}
