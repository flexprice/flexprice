package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type PaymentServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  PaymentService
	testData struct {
		customer *customer.Customer
		invoice  *invoice.Invoice
	}
}

func TestPaymentService(t *testing.T) {
	suite.Run(t, new(PaymentServiceSuite))
}

func (s *PaymentServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *PaymentServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *PaymentServiceSuite) setupService() {
	// Create the PaymentService
	s.service = NewPaymentService(ServiceParams{
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
	})
}

func (s *PaymentServiceSuite) setupTestData() {
	// Create test customer
	s.testData.customer = &customer.Customer{
		ID:         "cust_test_payment",
		ExternalID: "ext_cust_test_payment",
		Name:       "Test Customer",
		Email:      "test@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	// Create test invoice
	s.testData.invoice = &invoice.Invoice{
		ID:              "inv_test_payment",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(100),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(100),
		Description:     "Test Invoice for Payment Links",
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), s.testData.invoice))
}

func (s *PaymentServiceSuite) TestCreatePaymentWithPaymentLink() {
	// Add environment ID to context
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	// Test creating a payment with PAYMENT_LINK method type
	req := &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeStripe),
		Amount:            decimal.NewFromFloat(100),
		Currency:          "usd",
		ProcessPayment:    false, // Don't process immediately
	}

	resp, err := s.service.CreatePayment(ctx, req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(types.PaymentMethodTypePaymentLink, resp.PaymentMethodType)
	s.Equal("stripe", *resp.PaymentGateway)
	s.Equal(types.PaymentStatusInitiated, resp.PaymentStatus)
}

func (s *PaymentServiceSuite) TestCreatePaymentLink_InitiatedStatus() {
	// Add environment ID to context
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	// Create payment link request
	req := &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeStripe),
		Amount:            decimal.NewFromInt(1000),
		Currency:          "usd",
		ProcessPayment:    false, // Don't process immediately to avoid Stripe calls
	}

	// Test that payment is created with INITIATED status
	paymentResp, err := s.service.CreatePayment(ctx, req)
	s.NoError(err)
	s.NotNil(paymentResp)
	s.Equal(types.PaymentStatusInitiated, paymentResp.PaymentStatus)

	// Verify payment was created in database
	payment, err := s.GetStores().PaymentRepo.Get(ctx, paymentResp.ID)
	s.NoError(err)
	s.Equal(types.PaymentStatusInitiated, payment.PaymentStatus)
	s.Equal(types.PaymentMethodTypePaymentLink, payment.PaymentMethodType)
	s.Equal(string(types.PaymentGatewayTypeStripe), *payment.PaymentGateway)
}

func (s *PaymentServiceSuite) TestPaymentProcessor_PaymentLinkFlow() {
	// Add environment ID to context
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	// Create payment link request without processing
	req := &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeStripe),
		Amount:            decimal.NewFromInt(1000),
		Currency:          "usd",
		ProcessPayment:    false, // Don't process immediately
	}

	// Create payment with INITIATED status
	paymentResp, err := s.service.CreatePayment(ctx, req)
	s.NoError(err)
	s.NotNil(paymentResp)
	s.Equal(types.PaymentStatusInitiated, paymentResp.PaymentStatus)

	// Verify payment was created in database with INITIATED status
	payment, err := s.GetStores().PaymentRepo.Get(ctx, paymentResp.ID)
	s.NoError(err)
	s.Equal(types.PaymentStatusInitiated, payment.PaymentStatus)

	// Test that payment processor accepts INITIATED status for payment links
	// We'll just verify that the payment object has the correct status
	// without actually calling the processor (which would require Stripe setup)
	s.Equal(types.PaymentStatusInitiated, payment.PaymentStatus)
	s.Equal(types.PaymentMethodTypePaymentLink, payment.PaymentMethodType)
	s.Equal(string(types.PaymentGatewayTypeStripe), *payment.PaymentGateway)

	// Verify that the payment is in a state that would be accepted by the processor
	s.True(payment.PaymentStatus == types.PaymentStatusInitiated || payment.PaymentStatus == types.PaymentStatusPending)
}
