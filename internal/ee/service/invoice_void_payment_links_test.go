package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type VoidPaymentLinksSuite struct {
	testutil.BaseServiceTestSuite
	svc      InvoiceService
	provider *fakeCheckoutProvider
}

func TestVoid_PaymentLinks(t *testing.T) {
	suite.Run(t, new(VoidPaymentLinksSuite))
}

func (s *VoidPaymentLinksSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.ClearStores()
	s.provider = &fakeCheckoutProvider{}
	s.GetIntegrationFactory().SetCheckoutProvider(s.provider)

	stores := s.GetStores()
	s.svc = NewInvoiceService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		CustomerRepo:               stores.CustomerRepo,
		InvoiceRepo:                stores.InvoiceRepo,
		InvoiceLineItemRepo:        stores.InvoiceLineItemRepo,
		BillingInstalmentRepo:      stores.BillingInstalmentRepo,
		WalletRepo:                 stores.WalletRepo,
		PaymentRepo:                stores.PaymentRepo,
		RefundRepo:                 stores.RefundRepo,
		CheckoutSessionRepo:        stores.CheckoutSessionRepo,
		CouponApplicationRepo:      stores.CouponApplicationRepo,
		SubRepo:                    stores.SubscriptionRepo,
		SubscriptionLineItemRepo:   stores.SubscriptionLineItemRepo,
		SettingsRepo:               stores.SettingsRepo,
		TaxAppliedRepo:             stores.TaxAppliedRepo,
		TaxRateRepo:                stores.TaxRateRepo,
		TaxAssociationRepo:         stores.TaxAssociationRepo,
		ConnectionRepo:             stores.ConnectionRepo,
		IntegrationFactory:         s.GetIntegrationFactory(),
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		WalletBalanceAlertPubSub:   types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
		CreditGrantRepo:            stores.CreditGrantRepo,
		CreditGrantApplicationRepo: stores.CreditGrantApplicationRepo,
		CreditNoteRepo:             stores.CreditNoteRepo,
		CreditNoteLineItemRepo:     stores.CreditNoteLineItemRepo,
		EnvironmentRepo:            stores.EnvironmentRepo,
		TenantRepo:                 stores.TenantRepo,
		UserRepo:                   stores.UserRepo,
		AuthRepo:                   stores.AuthRepo,
	})
}

func (s *VoidPaymentLinksSuite) TearDownTest() {
	s.GetIntegrationFactory().SetCheckoutProvider(nil)
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *VoidPaymentLinksSuite) seedInvoice() *invoice.Invoice {
	ctx := s.GetContext()
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, &customer.Customer{
		ID: "cust_vpl", ExternalID: "ext_vpl", Name: "C", Email: "c@test.com",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	inv := &invoice.Invoice{
		ID:              "inv_vpl",
		CustomerID:      "cust_vpl",
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		Subtotal:        decimal.NewFromInt(100),
		Total:           decimal.NewFromInt(100),
		AmountDue:       decimal.NewFromInt(100),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromInt(100),
		FinalizedAt:     lo.ToPtr(time.Now().UTC()),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))
	return inv
}

func (s *VoidPaymentLinksSuite) seedPayment(
	id string, status types.PaymentStatus, method types.PaymentMethodType, trackingID string,
) *payment.Payment {
	ctx := s.GetContext()
	p := &payment.Payment{
		ID:                id,
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     "inv_vpl",
		PaymentMethodType: method,
		PaymentGateway:    lo.ToPtr(string(types.PaymentGatewayTypeStripe)),
		GatewayTrackingID: lo.ToPtr(trackingID),
		Amount:            decimal.NewFromInt(100),
		Currency:          "usd",
		PaymentStatus:     status,
		EnvironmentID:     types.GetEnvironmentID(ctx),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PaymentRepo.Create(ctx, p))
	return p
}

func (s *VoidPaymentLinksSuite) void(req dto.InvoiceVoidRequest) {
	_, err := s.svc.VoidInvoice(s.GetContext(), "inv_vpl", req)
	s.Require().NoError(err)
}

func (s *VoidPaymentLinksSuite) payment(id string) *payment.Payment {
	p, err := s.GetStores().PaymentRepo.Get(s.GetContext(), id)
	s.Require().NoError(err)
	return p
}

func (s *VoidPaymentLinksSuite) seedSession(paymentID string) *checkout.CheckoutSession {
	ctx := s.GetContext()
	session := &checkout.CheckoutSession{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		CustomerID:        "cust_vpl",
		Action:            types.CheckoutActionCreateSubscription,
		CheckoutStatus:    types.CheckoutStatusPending,
		PaymentProvider:   types.CheckoutPaymentProviderStripe,
		CheckoutPaymentID: lo.ToPtr(paymentID),
		ExpiresAt:         time.Now().UTC().Add(20 * time.Minute),
		EnvironmentID:     types.GetEnvironmentID(ctx),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, session))
	return session
}

func (s *VoidPaymentLinksSuite) TestVoid_VoidsPendingStripeLinkAndExpiresIt() {
	s.seedInvoice()
	s.seedPayment("pay_1", types.PaymentStatusPending, types.PaymentMethodTypePaymentLink, "cs_1")

	s.void(dto.InvoiceVoidRequest{})

	p := s.payment("pay_1")
	s.Equal(types.PaymentStatusVoided, p.PaymentStatus)
	s.NotNil(p.VoidedAt)
	s.Equal([]string{"cs_1"}, s.provider.expired)
}

func (s *VoidPaymentLinksSuite) TestVoid_VoidsInitiatedLink() {
	s.seedInvoice()
	s.seedPayment("pay_1", types.PaymentStatusInitiated, types.PaymentMethodTypePaymentLink, "cs_1")

	s.void(dto.InvoiceVoidRequest{})

	s.Equal(types.PaymentStatusVoided, s.payment("pay_1").PaymentStatus)
}

func (s *VoidPaymentLinksSuite) TestVoid_LeavesNonLinkAndSettledPaymentsAlone() {
	s.seedInvoice()
	s.seedPayment("pay_done", types.PaymentStatusSucceeded, types.PaymentMethodTypePaymentLink, "cs_done")
	s.seedPayment("pay_card", types.PaymentStatusPending, types.PaymentMethodTypeCard, "pi_card")

	s.void(dto.InvoiceVoidRequest{})

	s.Equal(types.PaymentStatusSucceeded, s.payment("pay_done").PaymentStatus)
	s.Equal(types.PaymentStatusPending, s.payment("pay_card").PaymentStatus)
	s.Empty(s.provider.expired)
}

func (s *VoidPaymentLinksSuite) TestVoid_ProviderErrorDoesNotFailVoid() {
	s.seedInvoice()
	s.seedPayment("pay_1", types.PaymentStatusPending, types.PaymentMethodTypePaymentLink, "cs_1")
	s.provider.expireErr = ierr.NewError("stripe down").Mark(ierr.ErrHTTPClient)

	s.void(dto.InvoiceVoidRequest{})

	s.Equal(types.PaymentStatusVoided, s.payment("pay_1").PaymentStatus)
}

func (s *VoidPaymentLinksSuite) TestVoid_NotImplementedProviderStillVoidsPayment() {
	s.seedInvoice()
	s.seedPayment("pay_1", types.PaymentStatusPending, types.PaymentMethodTypePaymentLink, "hp_1")
	s.provider.expireErr = ierr.NewError("not implemented").Mark(ierr.ErrNotImplemented)

	s.void(dto.InvoiceVoidRequest{})

	s.Equal(types.PaymentStatusVoided, s.payment("pay_1").PaymentStatus)
}

func (s *VoidPaymentLinksSuite) TestVoid_TerminatesForeignCheckoutSession() {
	s.seedInvoice()
	s.seedPayment("pay_1", types.PaymentStatusPending, types.PaymentMethodTypePaymentLink, "cs_1")
	session := s.seedSession("pay_1")

	s.void(dto.InvoiceVoidRequest{})

	got, err := s.GetStores().CheckoutSessionRepo.Get(s.GetContext(), session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusFailed, got.CheckoutStatus)
}

func (s *VoidPaymentLinksSuite) TestVoid_SkipsOwningCheckoutSession() {
	s.seedInvoice()
	s.seedPayment("pay_1", types.PaymentStatusPending, types.PaymentMethodTypePaymentLink, "cs_1")
	session := s.seedSession("pay_1")

	s.void(dto.InvoiceVoidRequest{InvoiceStateChangeSource: dto.NewCheckoutSessionSource(session.ID)})

	got, err := s.GetStores().CheckoutSessionRepo.Get(s.GetContext(), session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusPending, got.CheckoutStatus)
}
