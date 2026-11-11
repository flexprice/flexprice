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
	params   ServiceParams
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
	s.params = ServiceParams{
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
	}
	s.svc = NewInvoiceService(s.params)
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

func (s *VoidPaymentLinksSuite) TestVoid_InTransactionClosesLinksAfterCommit() {
	s.seedInvoice()
	s.seedPayment("pay_1", types.PaymentStatusPending, types.PaymentMethodTypePaymentLink, "cs_1")
	txCtx := types.WithPostCommitHooks(s.GetContext())

	_, err := s.svc.VoidInvoice(txCtx, "inv_vpl", dto.InvoiceVoidRequest{})
	s.Require().NoError(err)

	s.Equal(types.PaymentStatusPending, s.payment("pay_1").PaymentStatus)
	s.Empty(s.provider.expired)

	types.RunPostCommitHooks(txCtx)

	s.Equal(types.PaymentStatusVoided, s.payment("pay_1").PaymentStatus)
	s.Equal([]string{"cs_1"}, s.provider.expired)
}

func (s *VoidPaymentLinksSuite) TestVoid_SkipsCheckoutSessionLink() {
	s.seedInvoice()
	s.seedPayment("pay_1", types.PaymentStatusPending, types.PaymentMethodTypePaymentLink, "cs_1")
	session := s.seedSession("pay_1")

	s.void(dto.InvoiceVoidRequest{})

	s.Equal(types.PaymentStatusPending, s.payment("pay_1").PaymentStatus)
	s.Empty(s.provider.expired)
	got, err := s.GetStores().CheckoutSessionRepo.Get(s.GetContext(), session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusPending, got.CheckoutStatus)
}

func (s *VoidPaymentLinksSuite) voidInvoiceDirectly() {
	ctx := s.GetContext()
	inv, err := s.GetStores().InvoiceRepo.Get(ctx, "inv_vpl")
	s.Require().NoError(err)
	inv.InvoiceStatus = types.InvoiceStatusVoided
	s.Require().NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))
}

func (s *VoidPaymentLinksSuite) TestProcessPayment_RejectsVoidedInvoice() {
	s.seedInvoice()
	s.seedPayment("pay_1", types.PaymentStatusInitiated, types.PaymentMethodTypePaymentLink, "cs_1")
	s.voidInvoiceDirectly()

	_, err := NewPaymentProcessorService(s.params).ProcessPayment(s.GetContext(), "pay_1")

	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
	s.Empty(s.provider.linkRequests)
	s.Equal(types.PaymentStatusInitiated, s.payment("pay_1").PaymentStatus)
}

func (s *VoidPaymentLinksSuite) TestGetOrCreatePaymentLink_RejectsVoidedInvoice() {
	s.seedInvoice()
	p := s.seedPayment("pay_1", types.PaymentStatusPending, types.PaymentMethodTypePaymentLink, "cs_1")
	p.GatewayMetadata = types.Metadata{"payment_url": "https://pay.test/cs_1"}
	s.Require().NoError(s.GetStores().PaymentRepo.Update(s.GetContext(), p))
	s.voidInvoiceDirectly()

	_, err := NewPaymentService(s.params).(*paymentService).GetOrCreateInvoicePaymentLink(s.GetContext(), &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     "inv_vpl",
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeStripe),
	})

	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
	s.Empty(s.provider.linkRequests)
}
