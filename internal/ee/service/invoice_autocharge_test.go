package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// AutoChargePaymentSuite tests the findOrCreateAutoChargePayment helper in isolation.
// It lives in package service (not service_test) so it can call the unexported method.
type AutoChargePaymentSuite struct {
	testutil.BaseServiceTestSuite
	svc         *invoiceService
	paymentRepo *testutil.InMemoryPaymentStore
	inv         *invoice.Invoice
}

func TestFindOrCreateAutoChargePayment(t *testing.T) {
	suite.Run(t, new(AutoChargePaymentSuite))
}

func (s *AutoChargePaymentSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.paymentRepo = s.GetStores().PaymentRepo.(*testutil.InMemoryPaymentStore)

	rawSvc := NewInvoiceService(ServiceParams{
		Logger:      s.GetLogger(),
		Config:      s.GetConfig(),
		DB:          s.GetDB(),
		PaymentRepo: s.GetStores().PaymentRepo,
		// Remaining fields are nil-safe for this helper which only touches PaymentRepo.
	})
	s.svc = rawSvc.(*invoiceService)

	s.inv = &invoice.Invoice{
		ID:              "inv_autocharge_test_001",
		CustomerID:      "cust_001",
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		AmountRemaining: decimal.NewFromFloat(500.00),
		Currency:        "INR",
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
}

func (s *AutoChargePaymentSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

// makePayment builds a payment pre-seeded with the autocharge idempotency key so
// tests can insert an existing record and observe how the helper reacts.
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
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		PaymentGateway:  &gatewayType,
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
}

func (s *AutoChargePaymentSuite) TestNoExistingRecord_CreatesInitiated() {
	pymnt, skip, err := s.svc.findOrCreateAutoChargePayment(s.GetContext(), s.inv)

	s.NoError(err)
	s.False(skip)
	s.Require().NotNil(pymnt)
	s.Equal(types.PaymentStatusInitiated, pymnt.PaymentStatus)
	s.Equal("autocharge:"+s.inv.ID, pymnt.IdempotencyKey)
	s.Equal(s.inv.ID, pymnt.DestinationID)
}

func (s *AutoChargePaymentSuite) TestExistingInitiated_ReturnsSameNoSkip() {
	existing := s.makePayment(types.PaymentStatusInitiated)
	s.Require().NoError(s.paymentRepo.Create(s.GetContext(), existing))

	pymnt, skip, err := s.svc.findOrCreateAutoChargePayment(s.GetContext(), s.inv)

	s.NoError(err)
	s.False(skip, "INITIATED should not be skipped")
	s.Require().NotNil(pymnt)
	s.Equal(existing.ID, pymnt.ID)
	s.Equal(types.PaymentStatusInitiated, pymnt.PaymentStatus)
}

func (s *AutoChargePaymentSuite) TestExistingPending_ReturnsSameNoSkip() {
	existing := s.makePayment(types.PaymentStatusPending)
	s.Require().NoError(s.paymentRepo.Create(s.GetContext(), existing))

	pymnt, skip, err := s.svc.findOrCreateAutoChargePayment(s.GetContext(), s.inv)

	s.NoError(err)
	s.False(skip, "PENDING should not be skipped")
	s.Require().NotNil(pymnt)
	s.Equal(existing.ID, pymnt.ID)
}

func (s *AutoChargePaymentSuite) TestExistingProcessing_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.GetContext(), s.makePayment(types.PaymentStatusProcessing)))

	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.GetContext(), s.inv)

	s.NoError(err)
	s.True(skip, "PROCESSING should be skipped")
}

func (s *AutoChargePaymentSuite) TestExistingSucceeded_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.GetContext(), s.makePayment(types.PaymentStatusSucceeded)))

	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.GetContext(), s.inv)

	s.NoError(err)
	s.True(skip, "SUCCEEDED should be skipped")
}

func (s *AutoChargePaymentSuite) TestExistingOverpaid_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.GetContext(), s.makePayment(types.PaymentStatusOverpaid)))

	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.GetContext(), s.inv)

	s.NoError(err)
	s.True(skip, "OVERPAID should be skipped")
}

func (s *AutoChargePaymentSuite) TestExistingFailed_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.GetContext(), s.makePayment(types.PaymentStatusFailed)))

	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.GetContext(), s.inv)

	s.NoError(err)
	s.True(skip, "FAILED should be skipped; fallback to send-invoice handles it")
}

func (s *AutoChargePaymentSuite) TestExistingVoided_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.GetContext(), s.makePayment(types.PaymentStatusVoided)))

	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.GetContext(), s.inv)

	s.NoError(err)
	s.True(skip, "VOIDED should be skipped via the default branch")
}

func (s *AutoChargePaymentSuite) TestExistingRefunded_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.GetContext(), s.makePayment(types.PaymentStatusRefunded)))

	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.GetContext(), s.inv)

	s.NoError(err)
	s.True(skip, "REFUNDED should be skipped via the default branch")
}

func (s *AutoChargePaymentSuite) TestExistingPartiallyRefunded_Skip() {
	s.Require().NoError(s.paymentRepo.Create(s.GetContext(), s.makePayment(types.PaymentStatusPartiallyRefunded)))

	_, skip, err := s.svc.findOrCreateAutoChargePayment(s.GetContext(), s.inv)

	s.NoError(err)
	s.True(skip, "PARTIALLY_REFUNDED should be skipped via the default branch")
}

// TestIdempotency_SecondCallReturnsSameRecord verifies that calling the helper
// twice (simulating a retry) returns the same payment record, not a new one.
func (s *AutoChargePaymentSuite) TestIdempotency_SecondCallReturnsSameRecord() {
	first, skip1, err := s.svc.findOrCreateAutoChargePayment(s.GetContext(), s.inv)
	s.Require().NoError(err)
	s.False(skip1)
	s.Require().NotNil(first)

	second, skip2, err := s.svc.findOrCreateAutoChargePayment(s.GetContext(), s.inv)
	s.Require().NoError(err)
	s.False(skip2)
	s.Require().NotNil(second)

	s.Equal(first.ID, second.ID, "second call must return the same payment record")
}
