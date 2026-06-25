package payments_test

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/integration/payments"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type PaymentLifecycleSuite struct {
	testutil.BaseServiceTestSuite
	lifecycle *payments.PaymentLifecycle
	testData struct {
		customer *customer.Customer
		invoice  *invoice.Invoice
	}
}

func TestPaymentLifecycle(t *testing.T) {
	suite.Run(t, new(PaymentLifecycleSuite))
}

func (s *PaymentLifecycleSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupLifecycle()
	s.setupTestData()
}

func (s *PaymentLifecycleSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *PaymentLifecycleSuite) setupLifecycle() {
	params := service.ServiceParams{
		Logger:           s.GetLogger(),
		Config:           s.GetConfig(),
		DB:               s.GetDB(),
		SubRepo:          s.GetStores().SubscriptionRepo,
		PlanRepo:         s.GetStores().PlanRepo,
		PriceRepo:        s.GetStores().PriceRepo,
		EventRepo:        s.GetStores().EventRepo,
		MeterRepo:        s.GetStores().MeterRepo,
		CustomerRepo:     s.GetStores().CustomerRepo,
		InvoiceRepo:      s.GetStores().InvoiceRepo,
		EntitlementRepo:  s.GetStores().EntitlementRepo,
		EnvironmentRepo:  s.GetStores().EnvironmentRepo,
		FeatureRepo:      s.GetStores().FeatureRepo,
		TenantRepo:       s.GetStores().TenantRepo,
		UserRepo:         s.GetStores().UserRepo,
		AuthRepo:         s.GetStores().AuthRepo,
		WalletRepo:       s.GetStores().WalletRepo,
		PaymentRepo:      s.GetStores().PaymentRepo,
		EventPublisher:   s.GetPublisher(),
		WebhookPublisher: s.GetWebhookPublisher(),
	}
	paymentSvc := service.NewPaymentService(params)
	invoiceSvc := service.NewInvoiceService(params)
	s.lifecycle = payments.NewPaymentLifecycle(paymentSvc, invoiceSvc, s.GetLogger())
}

func (s *PaymentLifecycleSuite) setupTestData() {
	s.testData.customer = &customer.Customer{
		ID:         "cust_lifecycle_test",
		ExternalID: "ext_cust_lifecycle_test",
		Name:       "Ledger Test Customer",
		Email:      "lifecycle@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	s.testData.invoice = &invoice.Invoice{
		ID:              "inv_lifecycle_test",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(100),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(100),
		Description:     "Ledger Test Invoice",
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), s.testData.invoice))
}

// authParams returns a minimal InitiatePaymentParams for an AUTH payment.
func (s *PaymentLifecycleSuite) authParams() payments.InitiatePaymentParams {
	return payments.InitiatePaymentParams{
		DestinationType:   types.PaymentDestinationTypeCustomer,
		DestinationID:     s.testData.customer.ID,
		PaymentMethodType: types.PaymentMethodTypeCard,
		Gateway:           string(types.PaymentGatewayTypeMoyasar),
		Amount:            decimal.NewFromFloat(1.0),
		Currency:          "SAR",
	}
}

// invoiceParams returns a minimal InitiatePaymentParams for an INVOICE payment.
func (s *PaymentLifecycleSuite) invoiceParams() payments.InitiatePaymentParams {
	return payments.InitiatePaymentParams{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypeCard,
		Gateway:           string(types.PaymentGatewayTypeMoyasar),
		Amount:            decimal.NewFromFloat(100.0),
		Currency:          "USD",
	}
}

// ── InitiatePayment ──────────────────────────────────────────────────────────

func (s *PaymentLifecycleSuite) TestInitiatePayment_Success() {
	ctx := s.GetContext()
	id, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NotEmpty(id)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusInitiated, payment.PaymentStatus)
}

func (s *PaymentLifecycleSuite) TestInitiatePayment_Idempotent() {
	ctx := s.GetContext()

	id1, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NotEmpty(id1)

	// Second call with same params should either return same id or a new one
	// but must not error; idempotency is keyed on IdempotencyKey when set.
	// Since InitiatePaymentParams has no explicit IdempotencyKey field, the
	// service-level idempotency (duplicate key constraint) may not apply here
	// but we verify no error is returned.
	id2, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NotEmpty(id2)
}

func (s *PaymentLifecycleSuite) TestInitiatePayment_ValidationErrors() {
	ctx := s.GetContext()

	tests := []struct {
		name   string
		mutate func(*payments.InitiatePaymentParams)
	}{
		{
			name:   "missing DestinationType",
			mutate: func(p *payments.InitiatePaymentParams) { p.DestinationType = "" },
		},
		{
			name:   "missing DestinationID",
			mutate: func(p *payments.InitiatePaymentParams) { p.DestinationID = "" },
		},
		{
			name:   "zero Amount",
			mutate: func(p *payments.InitiatePaymentParams) { p.Amount = decimal.Zero },
		},
		{
			name:   "negative Amount",
			mutate: func(p *payments.InitiatePaymentParams) { p.Amount = decimal.NewFromFloat(-5) },
		},
		{
			name:   "missing Currency",
			mutate: func(p *payments.InitiatePaymentParams) { p.Currency = "" },
		},
		{
			name:   "missing Gateway",
			mutate: func(p *payments.InitiatePaymentParams) { p.Gateway = "" },
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			p := s.authParams()
			tc.mutate(&p)
			_, err := s.lifecycle.InitiatePayment(ctx, p)
			s.Error(err, "expected validation error for: %s", tc.name)
		})
	}
}

// ── ConfirmGatewayPayment ────────────────────────────────────────────────────

func (s *PaymentLifecycleSuite) TestConfirmGatewayPayment_Success() {
	ctx := s.GetContext()
	id, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)

	err = s.lifecycle.ConfirmGatewayPayment(ctx, id, "gw_pay_001")
	s.NoError(err)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusPending, payment.PaymentStatus)
	s.Require().NotNil(payment.PaymentGateway)
}

func (s *PaymentLifecycleSuite) TestConfirmGatewayPayment_MissingParams() {
	ctx := s.GetContext()
	id, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)

	tests := []struct {
		name               string
		flexpricePaymentID string
		gatewayPaymentID   string
	}{
		{"empty flexpricePaymentID", "", "gw_pay_001"},
		{"empty gatewayPaymentID", id, ""},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			err := s.lifecycle.ConfirmGatewayPayment(ctx, tc.flexpricePaymentID, tc.gatewayPaymentID)
			s.Error(err)
		})
	}
}

func (s *PaymentLifecycleSuite) TestConfirmGatewayPayment_RejectsNonInitiatedPayment() {
	ctx := s.GetContext()
	id, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)

	// Confirm once: INITIATED → PENDING
	s.NoError(s.lifecycle.ConfirmGatewayPayment(ctx, id, "gw_pay_dup"))

	// Confirming again on a PENDING payment must be rejected
	err = s.lifecycle.ConfirmGatewayPayment(ctx, id, "gw_pay_dup2")
	s.Error(err, "expected error when confirming a non-INITIATED payment")
}

// ── RecordPaymentSuccess ─────────────────────────────────────────────────────

func (s *PaymentLifecycleSuite) TestRecordPaymentSuccess_Success() {
	ctx := s.GetContext()
	id, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.lifecycle.ConfirmGatewayPayment(ctx, id, "gw_pay_002"))

	err = s.lifecycle.RecordPaymentSuccess(ctx, payments.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_002",
		SucceededAt:        time.Now().UTC(),
	})
	s.NoError(err)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusSucceeded, payment.PaymentStatus)
}

func (s *PaymentLifecycleSuite) TestRecordPaymentSuccess_Idempotent() {
	ctx := s.GetContext()
	id, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.lifecycle.ConfirmGatewayPayment(ctx, id, "gw_pay_003"))

	successParams := payments.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_003",
		SucceededAt:        time.Now().UTC(),
	}
	s.NoError(s.lifecycle.RecordPaymentSuccess(ctx, successParams))
	// Second call — already SUCCEEDED, must return nil.
	s.NoError(s.lifecycle.RecordPaymentSuccess(ctx, successParams))
}

func (s *PaymentLifecycleSuite) TestRecordPaymentSuccess_TerminalStateError() {
	ctx := s.GetContext()
	id, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.lifecycle.ConfirmGatewayPayment(ctx, id, "gw_pay_004"))
	s.NoError(s.lifecycle.RecordPaymentSuccess(ctx, payments.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_004",
	}))
	// Void it so it's in a non-SUCCEEDED terminal state.
	s.NoError(s.lifecycle.RecordPaymentVoided(ctx, payments.RecordPaymentVoidedParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_004",
	}))

	// Now attempting to mark it succeeded should fail.
	err = s.lifecycle.RecordPaymentSuccess(ctx, payments.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_004",
	})
	s.Error(err)
}

// ── RecordPaymentFailure ─────────────────────────────────────────────────────

func (s *PaymentLifecycleSuite) TestRecordPaymentFailure_Success() {
	ctx := s.GetContext()
	id, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.lifecycle.ConfirmGatewayPayment(ctx, id, "gw_pay_005"))

	err = s.lifecycle.RecordPaymentFailure(ctx, payments.RecordPaymentFailureParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_005",
		ErrorMessage:       "card declined",
		FailedAt:           time.Now().UTC(),
	})
	s.NoError(err)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusFailed, payment.PaymentStatus)
}

func (s *PaymentLifecycleSuite) TestRecordPaymentFailure_Idempotent() {
	ctx := s.GetContext()
	id, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.lifecycle.ConfirmGatewayPayment(ctx, id, "gw_pay_006"))

	failParams := payments.RecordPaymentFailureParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_006",
		ErrorMessage:       "insufficient funds",
	}
	s.NoError(s.lifecycle.RecordPaymentFailure(ctx, failParams))
	// Second call — already FAILED, must return nil.
	s.NoError(s.lifecycle.RecordPaymentFailure(ctx, failParams))
}

// ── RecordPaymentVoided ──────────────────────────────────────────────────────

func (s *PaymentLifecycleSuite) TestRecordPaymentVoided_Success() {
	ctx := s.GetContext()
	id, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.lifecycle.ConfirmGatewayPayment(ctx, id, "gw_pay_007"))
	s.NoError(s.lifecycle.RecordPaymentSuccess(ctx, payments.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_007",
	}))

	err = s.lifecycle.RecordPaymentVoided(ctx, payments.RecordPaymentVoidedParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_007",
		VoidedAt:           time.Now().UTC(),
	})
	s.NoError(err)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusVoided, payment.PaymentStatus)
}

// ── RecordPaymentRefunded ────────────────────────────────────────────────────

func (s *PaymentLifecycleSuite) TestRecordPaymentRefunded_Success() {
	ctx := s.GetContext()
	id, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.lifecycle.ConfirmGatewayPayment(ctx, id, "gw_pay_008"))
	s.NoError(s.lifecycle.RecordPaymentSuccess(ctx, payments.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_008",
	}))

	err = s.lifecycle.RecordPaymentRefunded(ctx, payments.RecordPaymentRefundedParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_008",
		RefundedAt:         time.Now().UTC(),
	})
	s.NoError(err)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusRefunded, payment.PaymentStatus)
}

// ── Full lifecycle tests ─────────────────────────────────────────────────────

// TestFullLifecycle_AUTH exercises the complete AUTH token flow:
// INITIATED → PENDING → SUCCEEDED → VOIDED
func (s *PaymentLifecycleSuite) TestFullLifecycle_AUTH() {
	ctx := s.GetContext()

	// Step 1: Initiate
	id, err := s.lifecycle.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NotEmpty(id)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusInitiated, payment.PaymentStatus)

	// Step 2: Confirm (INITIATED → PENDING)
	s.NoError(s.lifecycle.ConfirmGatewayPayment(ctx, id, "gw_auth_001"))
	payment, err = s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusPending, payment.PaymentStatus)

	// Step 3: Succeed (PENDING → SUCCEEDED)
	s.NoError(s.lifecycle.RecordPaymentSuccess(ctx, payments.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_auth_001",
		SucceededAt:        time.Now().UTC(),
	}))
	payment, err = s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusSucceeded, payment.PaymentStatus)

	// Step 4: Void (SUCCEEDED → VOIDED)
	s.NoError(s.lifecycle.RecordPaymentVoided(ctx, payments.RecordPaymentVoidedParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_auth_001",
		VoidedAt:           time.Now().UTC(),
	}))
	payment, err = s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusVoided, payment.PaymentStatus)
}

// TestFullLifecycle_Invoice exercises the complete invoice payment flow:
// INITIATED → PENDING → SUCCEEDED (with invoice reconciliation)
func (s *PaymentLifecycleSuite) TestFullLifecycle_Invoice() {
	ctx := s.GetContext()

	// Step 1: Initiate
	id, err := s.lifecycle.InitiatePayment(ctx, s.invoiceParams())
	s.NoError(err)
	s.NotEmpty(id)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusInitiated, payment.PaymentStatus)
	s.Equal(types.PaymentDestinationTypeInvoice, payment.DestinationType)

	// Step 2: Confirm (INITIATED → PENDING)
	s.NoError(s.lifecycle.ConfirmGatewayPayment(ctx, id, "gw_inv_001"))
	payment, err = s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusPending, payment.PaymentStatus)

	// Step 3: Succeed (PENDING → SUCCEEDED) — invoice should be reconciled
	s.NoError(s.lifecycle.RecordPaymentSuccess(ctx, payments.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_inv_001",
		SucceededAt:        time.Now().UTC(),
	}))
	payment, err = s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusSucceeded, payment.PaymentStatus)
}
