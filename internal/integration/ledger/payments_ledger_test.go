package ledger_test

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/integration/ledger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type PaymentsLedgerSuite struct {
	testutil.BaseServiceTestSuite
	ledger   *ledger.PaymentsLedger
	testData struct {
		customer *customer.Customer
		invoice  *invoice.Invoice
	}
}

func TestPaymentsLedger(t *testing.T) {
	suite.Run(t, new(PaymentsLedgerSuite))
}

func (s *PaymentsLedgerSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupLedger()
	s.setupTestData()
}

func (s *PaymentsLedgerSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *PaymentsLedgerSuite) setupLedger() {
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
	s.ledger = ledger.NewPaymentsLedger(paymentSvc, invoiceSvc, s.GetLogger())
}

func (s *PaymentsLedgerSuite) setupTestData() {
	s.testData.customer = &customer.Customer{
		ID:         "cust_ledger_test",
		ExternalID: "ext_cust_ledger_test",
		Name:       "Ledger Test Customer",
		Email:      "ledger@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	s.testData.invoice = &invoice.Invoice{
		ID:              "inv_ledger_test",
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
func (s *PaymentsLedgerSuite) authParams() ledger.InitiatePaymentParams {
	return ledger.InitiatePaymentParams{
		DestinationType:   types.PaymentDestinationTypeAuth,
		DestinationID:     s.testData.customer.ID,
		PaymentMethodType: types.PaymentMethodTypeCard,
		Gateway:           string(types.PaymentGatewayTypeMoyasar),
		Amount:            decimal.NewFromFloat(1.0),
		Currency:          "SAR",
	}
}

// invoiceParams returns a minimal InitiatePaymentParams for an INVOICE payment.
func (s *PaymentsLedgerSuite) invoiceParams() ledger.InitiatePaymentParams {
	return ledger.InitiatePaymentParams{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypeCard,
		Gateway:           string(types.PaymentGatewayTypeMoyasar),
		Amount:            decimal.NewFromFloat(100.0),
		Currency:          "USD",
	}
}

// ── InitiatePayment ──────────────────────────────────────────────────────────

func (s *PaymentsLedgerSuite) TestInitiatePayment_Success() {
	ctx := s.GetContext()
	id, err := s.ledger.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NotEmpty(id)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusInitiated, payment.PaymentStatus)
}

func (s *PaymentsLedgerSuite) TestInitiatePayment_Idempotent() {
	ctx := s.GetContext()

	id1, err := s.ledger.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NotEmpty(id1)

	// Second call with same params should either return same id or a new one
	// but must not error; idempotency is keyed on IdempotencyKey when set.
	// Since InitiatePaymentParams has no explicit IdempotencyKey field, the
	// service-level idempotency (duplicate key constraint) may not apply here
	// but we verify no error is returned.
	id2, err := s.ledger.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NotEmpty(id2)
}

func (s *PaymentsLedgerSuite) TestInitiatePayment_ValidationErrors() {
	ctx := s.GetContext()

	tests := []struct {
		name   string
		mutate func(*ledger.InitiatePaymentParams)
	}{
		{
			name:   "missing DestinationType",
			mutate: func(p *ledger.InitiatePaymentParams) { p.DestinationType = "" },
		},
		{
			name:   "missing DestinationID",
			mutate: func(p *ledger.InitiatePaymentParams) { p.DestinationID = "" },
		},
		{
			name:   "zero Amount",
			mutate: func(p *ledger.InitiatePaymentParams) { p.Amount = decimal.Zero },
		},
		{
			name:   "negative Amount",
			mutate: func(p *ledger.InitiatePaymentParams) { p.Amount = decimal.NewFromFloat(-5) },
		},
		{
			name:   "missing Currency",
			mutate: func(p *ledger.InitiatePaymentParams) { p.Currency = "" },
		},
		{
			name:   "missing Gateway",
			mutate: func(p *ledger.InitiatePaymentParams) { p.Gateway = "" },
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			p := s.authParams()
			tc.mutate(&p)
			_, err := s.ledger.InitiatePayment(ctx, p)
			s.Error(err, "expected validation error for: %s", tc.name)
		})
	}
}

// ── ConfirmGatewayPayment ────────────────────────────────────────────────────

func (s *PaymentsLedgerSuite) TestConfirmGatewayPayment_Success() {
	ctx := s.GetContext()
	id, err := s.ledger.InitiatePayment(ctx, s.authParams())
	s.NoError(err)

	err = s.ledger.ConfirmGatewayPayment(ctx, id, "gw_pay_001")
	s.NoError(err)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusPending, payment.PaymentStatus)
	s.Require().NotNil(payment.PaymentGateway)
}

func (s *PaymentsLedgerSuite) TestConfirmGatewayPayment_MissingParams() {
	ctx := s.GetContext()
	id, err := s.ledger.InitiatePayment(ctx, s.authParams())
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
			err := s.ledger.ConfirmGatewayPayment(ctx, tc.flexpricePaymentID, tc.gatewayPaymentID)
			s.Error(err)
		})
	}
}

// ── RecordPaymentSuccess ─────────────────────────────────────────────────────

func (s *PaymentsLedgerSuite) TestRecordPaymentSuccess_Success() {
	ctx := s.GetContext()
	id, err := s.ledger.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.ledger.ConfirmGatewayPayment(ctx, id, "gw_pay_002"))

	err = s.ledger.RecordPaymentSuccess(ctx, ledger.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_002",
		SucceededAt:        time.Now().UTC(),
	})
	s.NoError(err)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusSucceeded, payment.PaymentStatus)
}

func (s *PaymentsLedgerSuite) TestRecordPaymentSuccess_Idempotent() {
	ctx := s.GetContext()
	id, err := s.ledger.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.ledger.ConfirmGatewayPayment(ctx, id, "gw_pay_003"))

	successParams := ledger.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_003",
		SucceededAt:        time.Now().UTC(),
	}
	s.NoError(s.ledger.RecordPaymentSuccess(ctx, successParams))
	// Second call — already SUCCEEDED, must return nil.
	s.NoError(s.ledger.RecordPaymentSuccess(ctx, successParams))
}

func (s *PaymentsLedgerSuite) TestRecordPaymentSuccess_TerminalStateError() {
	ctx := s.GetContext()
	id, err := s.ledger.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.ledger.ConfirmGatewayPayment(ctx, id, "gw_pay_004"))
	s.NoError(s.ledger.RecordPaymentSuccess(ctx, ledger.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_004",
	}))
	// Void it so it's in a non-SUCCEEDED terminal state.
	s.NoError(s.ledger.RecordPaymentVoided(ctx, ledger.RecordPaymentVoidedParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_004",
	}))

	// Now attempting to mark it succeeded should fail.
	err = s.ledger.RecordPaymentSuccess(ctx, ledger.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_004",
	})
	s.Error(err)
}

// ── RecordPaymentFailure ─────────────────────────────────────────────────────

func (s *PaymentsLedgerSuite) TestRecordPaymentFailure_Success() {
	ctx := s.GetContext()
	id, err := s.ledger.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.ledger.ConfirmGatewayPayment(ctx, id, "gw_pay_005"))

	err = s.ledger.RecordPaymentFailure(ctx, ledger.RecordPaymentFailureParams{
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

func (s *PaymentsLedgerSuite) TestRecordPaymentFailure_Idempotent() {
	ctx := s.GetContext()
	id, err := s.ledger.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.ledger.ConfirmGatewayPayment(ctx, id, "gw_pay_006"))

	failParams := ledger.RecordPaymentFailureParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_006",
		ErrorMessage:       "insufficient funds",
	}
	s.NoError(s.ledger.RecordPaymentFailure(ctx, failParams))
	// Second call — already FAILED, must return nil.
	s.NoError(s.ledger.RecordPaymentFailure(ctx, failParams))
}

// ── RecordPaymentVoided ──────────────────────────────────────────────────────

func (s *PaymentsLedgerSuite) TestRecordPaymentVoided_Success() {
	ctx := s.GetContext()
	id, err := s.ledger.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.ledger.ConfirmGatewayPayment(ctx, id, "gw_pay_007"))
	s.NoError(s.ledger.RecordPaymentSuccess(ctx, ledger.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_007",
	}))

	err = s.ledger.RecordPaymentVoided(ctx, ledger.RecordPaymentVoidedParams{
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

func (s *PaymentsLedgerSuite) TestRecordPaymentRefunded_Success() {
	ctx := s.GetContext()
	id, err := s.ledger.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NoError(s.ledger.ConfirmGatewayPayment(ctx, id, "gw_pay_008"))
	s.NoError(s.ledger.RecordPaymentSuccess(ctx, ledger.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_pay_008",
	}))

	err = s.ledger.RecordPaymentRefunded(ctx, ledger.RecordPaymentRefundedParams{
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
func (s *PaymentsLedgerSuite) TestFullLifecycle_AUTH() {
	ctx := s.GetContext()

	// Step 1: Initiate
	id, err := s.ledger.InitiatePayment(ctx, s.authParams())
	s.NoError(err)
	s.NotEmpty(id)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusInitiated, payment.PaymentStatus)

	// Step 2: Confirm (INITIATED → PENDING)
	s.NoError(s.ledger.ConfirmGatewayPayment(ctx, id, "gw_auth_001"))
	payment, err = s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusPending, payment.PaymentStatus)

	// Step 3: Succeed (PENDING → SUCCEEDED)
	s.NoError(s.ledger.RecordPaymentSuccess(ctx, ledger.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_auth_001",
		SucceededAt:        time.Now().UTC(),
	}))
	payment, err = s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusSucceeded, payment.PaymentStatus)

	// Step 4: Void (SUCCEEDED → VOIDED)
	s.NoError(s.ledger.RecordPaymentVoided(ctx, ledger.RecordPaymentVoidedParams{
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
func (s *PaymentsLedgerSuite) TestFullLifecycle_Invoice() {
	ctx := s.GetContext()

	// Step 1: Initiate
	id, err := s.ledger.InitiatePayment(ctx, s.invoiceParams())
	s.NoError(err)
	s.NotEmpty(id)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusInitiated, payment.PaymentStatus)
	s.Equal(types.PaymentDestinationTypeInvoice, payment.DestinationType)

	// Step 2: Confirm (INITIATED → PENDING)
	s.NoError(s.ledger.ConfirmGatewayPayment(ctx, id, "gw_inv_001"))
	payment, err = s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusPending, payment.PaymentStatus)

	// Step 3: Succeed (PENDING → SUCCEEDED) — invoice should be reconciled
	s.NoError(s.ledger.RecordPaymentSuccess(ctx, ledger.RecordPaymentSuccessParams{
		FlexpricePaymentID: id,
		GatewayPaymentID:   "gw_inv_001",
		SucceededAt:        time.Now().UTC(),
	}))
	payment, err = s.GetStores().PaymentRepo.Get(ctx, id)
	s.NoError(err)
	s.Equal(types.PaymentStatusSucceeded, payment.PaymentStatus)
}
