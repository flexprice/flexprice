package service

import (
	"context"
	"testing"

	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type PortalWalletSuite struct {
	testutil.BaseServiceTestSuite
	svc      CustomerPortalService
	ctx      context.Context
	walletID string
}

func TestPortalWalletSuite(t *testing.T) {
	suite.Run(t, new(PortalWalletSuite))
}

func (s *PortalWalletSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.ClearStores()

	params := s.buildParams()
	s.svc = NewCustomerPortalService(params, NewCustomerService(params), nil)
	s.ctx = types.SetCustomerID(s.GetContext(), "cust_portal")
	s.walletID = "wallet_portal"

	// Invoice creation reads the customer to resolve tax rates and exemption, so the
	// row has to exist even for tests that only care about the wallet.
	s.NoError(s.GetStores().CustomerRepo.Create(s.ctx, &customer.Customer{
		ID:         "cust_portal",
		ExternalID: "ext_cust_portal",
		Name:       "Portal Customer",
		Email:      "portal@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.ctx),
	}))

	w := &wallet.Wallet{
		ID:                  s.walletID,
		CustomerID:          "cust_portal",
		Currency:            "usd",
		Balance:             decimal.NewFromInt(10),
		CreditBalance:       decimal.NewFromInt(10),
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletStatus:        types.WalletStatusActive,
		BaseModel:           types.GetDefaultBaseModel(s.ctx),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.ctx, w))
}

func (s *PortalWalletSuite) TearDownTest() {
	s.GetIntegrationFactory().SetCheckoutProvider(nil)
	s.GetIntegrationFactory().SetPaymentMethodProvider(nil)
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *PortalWalletSuite) buildParams() ServiceParams {
	stores := s.GetStores()
	return ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		SubRepo:                      stores.SubscriptionRepo,
		SubscriptionLineItemRepo:     stores.SubscriptionLineItemRepo,
		PlanRepo:                     stores.PlanRepo,
		PriceRepo:                    stores.PriceRepo,
		PriceUnitRepo:                stores.PriceUnitRepo,
		EventRepo:                    stores.EventRepo,
		MeterRepo:                    stores.MeterRepo,
		CustomerRepo:                 stores.CustomerRepo,
		InvoiceRepo:                  stores.InvoiceRepo,
		InvoiceLineItemRepo:          stores.InvoiceLineItemRepo,
		EnvironmentRepo:              stores.EnvironmentRepo,
		TenantRepo:                   stores.TenantRepo,
		WalletRepo:                   stores.WalletRepo,
		PaymentRepo:                  stores.PaymentRepo,
		CreditGrantRepo:              stores.CreditGrantRepo,
		CreditGrantApplicationRepo:   stores.CreditGrantApplicationRepo,
		CouponRepo:                   stores.CouponRepo,
		CouponAssociationRepo:        stores.CouponAssociationRepo,
		CouponApplicationRepo:        stores.CouponApplicationRepo,
		ConnectionRepo:               stores.ConnectionRepo,
		SettingsRepo:                 stores.SettingsRepo,
		TaxAssociationRepo:           stores.TaxAssociationRepo,
		TaxRateRepo:                  stores.TaxRateRepo,
		TaxAppliedRepo:               stores.TaxAppliedRepo,
		AlertLogsRepo:                stores.AlertLogsRepo,
		CheckoutSessionRepo:          stores.CheckoutSessionRepo,
		EntityIntegrationMappingRepo: stores.EntityIntegrationMappingRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		ProrationCalculator:          s.GetCalculator(),
		IntegrationFactory:           s.GetIntegrationFactory(),
		WalletBalanceAlertPubSub:     types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
	}
}

func (s *PortalWalletSuite) connect(providers ...types.SecretProvider) {
	for _, p := range providers {
		conn := &connection.Connection{
			ID:            "conn_" + string(p),
			Name:          string(p),
			ProviderType:  p,
			EnvironmentID: types.GetEnvironmentID(s.ctx),
			BaseModel:     types.GetDefaultBaseModel(s.ctx),
		}
		conn.Status = types.StatusPublished
		s.NoError(s.GetStores().ConnectionRepo.Create(s.ctx, conn))
	}
}

// The Phase 2 stub demanded payment_provider from the customer. With one capable
// provider configured the resolver supplies it and the request need not name one.
func (s *PortalWalletSuite) TestTopUpResolvesProviderWhenUnnamed() {
	s.connect(types.SecretProviderChargebee)

	provider, err := s.svc.(*customerPortalService).
		resolveCheckoutProvider(s.ctx, "cust_portal", nil)

	s.NoError(err)
	s.Equal(types.CheckoutPaymentProviderChargebee, provider)
}

// Two checkout-capable providers and no choice from the caller: refuse rather than
// pick, so the customer is never silently billed through the wrong gateway.
func (s *PortalWalletSuite) TestTopUpAmbiguousProviderIsRejected() {
	s.connect(types.SecretProviderChargebee, types.SecretProviderRazorpay)

	_, err := s.svc.(*customerPortalService).
		resolveCheckoutProvider(s.ctx, "cust_portal", nil)

	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *PortalWalletSuite) TestTopUpHonoursNamedProvider() {
	s.connect(types.SecretProviderChargebee, types.SecretProviderRazorpay)

	provider, err := s.svc.(*customerPortalService).
		resolveCheckoutProvider(s.ctx, "cust_portal", lo.ToPtr(types.PaymentGatewayTypeRazorpay))

	s.NoError(err)
	s.Equal(types.CheckoutPaymentProviderRazorpay, provider)
}

// Stripe is connected but has no checkout adapter, so it must not be resolved to.
func (s *PortalWalletSuite) TestTopUpIgnoresProvidersWithoutCheckout() {
	s.connect(types.SecretProviderStripe)

	_, err := s.svc.(*customerPortalService).
		resolveCheckoutProvider(s.ctx, "cust_portal", nil)

	s.Error(err)
	s.True(ierr.IsNotFound(err))
}

// An abandoned session locks the wallet until it expires; the customer gets the
// in-flight session back instead of a conflict they cannot act on.
func (s *PortalWalletSuite) TestTopUpReturnsExistingPendingSession() {
	s.connect(types.SecretProviderChargebee)
	s.seedPendingSession("cs_inflight")

	resp, err := s.svc.TopUpWallet(s.ctx, s.walletID, &dto.PortalTopUpWalletRequest{
		CreditsToAdd:   decimal.NewFromInt(5),
		IdempotencyKey: lo.ToPtr("idem_1"),
		Checkout:       &dto.PortalCheckoutParams{},
	})

	s.NoError(err, "an in-flight session must not surface as a conflict")
	s.Require().NotNil(resp.CheckoutSession)
	s.Equal("cs_inflight", resp.CheckoutSession.ID)
	s.Nil(resp.WalletTransaction, "no new transaction is created when one is in flight")
}

func (s *PortalWalletSuite) TestTopUpUseSavedMethodValidation() {
	s.connect(types.SecretProviderRazorpay, types.SecretProviderChargebee)

	tests := []struct {
		name           string
		provider       types.PaymentGatewayType
		useSavedMethod bool
		wantErr        bool
		errCheck       func(error) bool
	}{
		{
			name:           "saved method with razorpay fails with not implemented as provider lacks capability",
			provider:       types.PaymentGatewayTypeRazorpay,
			useSavedMethod: true,
			wantErr:        true,
			errCheck:       ierr.IsNotImplemented,
		},
		{
			name:           "saved method with chargebee fails validation when customer has no saved card",
			provider:       types.PaymentGatewayTypeChargebee,
			useSavedMethod: true,
			wantErr:        true,
			errCheck:       ierr.IsValidation,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			req := &dto.PortalTopUpWalletRequest{
				CreditsToAdd: decimal.NewFromInt(5),
				Checkout: &dto.PortalCheckoutParams{
					PaymentProvider: lo.ToPtr(tt.provider),
					UseSavedMethod:  tt.useSavedMethod,
				},
			}

			resp, err := s.svc.TopUpWallet(s.ctx, s.walletID, req)
			if tt.wantErr {
				s.Error(err)
				if tt.errCheck != nil {
					s.True(tt.errCheck(err))
				}
				s.Nil(resp)
			} else {
				s.NoError(err)
				s.NotNil(resp)
			}
		})
	}
}

type stubPaymentMethodProvider struct {
	methods []interfaces.ProviderPaymentMethod
	err     error
}

func (p *stubPaymentMethodProvider) ListSavedMethods(_ context.Context, _ string) ([]interfaces.ProviderPaymentMethod, error) {
	return p.methods, p.err
}

func (p *stubPaymentMethodProvider) DeleteSavedMethod(_ context.Context, _ string, _ string) error {
	return nil
}

func (p *stubPaymentMethodProvider) SetDefaultSavedMethod(_ context.Context, _ string, _ string) error {
	return nil
}

func (p *stubPaymentMethodProvider) CreateSetupLink(_ context.Context, _ interfaces.SetupLinkRequest) (*interfaces.SetupLinkResponse, error) {
	return nil, nil
}

func (s *PortalWalletSuite) TestValidateSavedMethodForTopUp_ActiveSavedCard_Success() {
	s.connect(types.SecretProviderChargebee)

	pmProvider := &stubPaymentMethodProvider{
		methods: []interfaces.ProviderPaymentMethod{
			{
				GatewayMethodID: "pm_chargebee_valid",
				Method:          types.PaymentMethodTypeCard,
				Active:          true,
			},
		},
	}
	s.GetIntegrationFactory().SetPaymentMethodProvider(pmProvider)

	portal := s.svc.(*customerPortalService)
	err := portal.validateSavedMethodForTopUp(s.ctx, "cust_portal", types.PaymentGatewayTypeChargebee)
	s.NoError(err, "validateSavedMethodForTopUp should pass when an active saved method exists")
}

func (s *PortalWalletSuite) seedPendingSession(id string) {
	session := &domainCheckout.CheckoutSession{
		ID:              id,
		EnvironmentID:   types.GetEnvironmentID(s.ctx),
		CustomerID:      "cust_portal",
		Action:          types.CheckoutActionWalletTopup,
		CheckoutStatus:  types.CheckoutStatusPending,
		PaymentProvider: types.CheckoutPaymentProviderChargebee,
		Configuration: domainCheckout.JSONBCheckoutConfiguration{
			WalletTopupParams: &types.WalletTopupParams{WalletID: s.walletID},
		},
		ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
		BaseModel: types.GetDefaultBaseModel(s.ctx),
	}
	s.NoError(s.GetStores().CheckoutSessionRepo.Create(s.ctx, session))
}

func (s *PortalWalletSuite) TestAutoTopupRequiresAmountAndThreshold() {
	s.connect(types.SecretProviderChargebee)

	_, err := s.svc.UpdateAutoTopup(s.ctx, s.walletID, &dto.PortalUpdateAutoTopupRequest{
		Enabled: true,
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

// Enabling is gated on a chargeable method: storing a preference that can never be
// honoured would leave the wallet silently never topping up.
func (s *PortalWalletSuite) TestAutoTopupEnableRequiresChargeableMethod() {
	s.connect(types.SecretProviderChargebee)

	_, err := s.svc.UpdateAutoTopup(s.ctx, s.walletID, &dto.PortalUpdateAutoTopupRequest{
		Enabled:   true,
		Threshold: lo.ToPtr(decimal.NewFromInt(5)),
		Amount:    lo.ToPtr(decimal.NewFromInt(20)),
	})
	s.Error(err)
	s.False(ierr.IsValidation(err), "a missing card is a state conflict, not a bad request")
}

func (s *PortalWalletSuite) TestAutoTopupEnableRequiresChargeableMethodRazorpay() {
	s.connect(types.SecretProviderRazorpay)

	_, err := s.svc.UpdateAutoTopup(s.ctx, s.walletID, &dto.PortalUpdateAutoTopupRequest{
		Enabled:   true,
		Threshold: lo.ToPtr(decimal.NewFromInt(5)),
		Amount:    lo.ToPtr(decimal.NewFromInt(20)),
	})
	s.Error(err)
	s.False(ierr.IsValidation(err), "a missing mandate is a state conflict, not a bad request")
}

// Disabling must never be gated on a card: a customer with no usable method still
// has to be able to turn auto top-up off.
func (s *PortalWalletSuite) TestAutoTopupDisableIsNotGated() {
	s.connect(types.SecretProviderChargebee)

	resp, err := s.svc.UpdateAutoTopup(s.ctx, s.walletID, &dto.PortalUpdateAutoTopupRequest{
		Enabled: false,
	})

	s.NoError(err)
	s.Require().NotNil(resp.AutoTopup)
	s.False(lo.FromPtr(resp.AutoTopup.Enabled))
	s.True(lo.FromPtr(resp.AutoTopup.Invoicing), "Invoicing is pinned true, never taken from the request")
}

func (s *PortalWalletSuite) TestAutoTopupRequiresWalletOwnership() {
	s.connect(types.SecretProviderChargebee)
	other := types.SetCustomerID(s.GetContext(), "cust_other")

	_, err := s.svc.UpdateAutoTopup(other, s.walletID, &dto.PortalUpdateAutoTopupRequest{})
	s.Error(err)
	s.True(ierr.IsPermissionDenied(err))
}

// A customer-initiated top-up must never grant credits before the invoice is paid,
// even on a tenant that has auto-complete switched on for its own admin top-ups.
func (s *PortalWalletSuite) TestTopUpNeverAutoCompletesForPortalCustomers() {
	s.enableAutoCompletePurchasedCredit()

	resp, err := s.svc.TopUpWallet(s.ctx, s.walletID, &dto.PortalTopUpWalletRequest{
		CreditsToAdd:   decimal.NewFromInt(5),
		IdempotencyKey: lo.ToPtr("idem_no_autocomplete"),
	})

	s.NoError(err)
	s.Require().NotNil(resp.WalletTransaction)
	s.Equal(types.TransactionStatusPending, resp.WalletTransaction.TxStatus,
		"portal top-up must stay pending until the invoice is paid")

	w, err := s.GetStores().WalletRepo.GetWalletByID(s.ctx, s.walletID)
	s.NoError(err)
	s.True(w.CreditBalance.Equal(decimal.NewFromInt(10)),
		"credits must not land before payment, got %s", w.CreditBalance)
}

func (s *PortalWalletSuite) enableAutoCompletePurchasedCredit() {
	cfg := types.InvoiceConfig{AutoCompletePurchasedCreditTransaction: true}
	raw, err := utils.ToMap(cfg)
	s.Require().NoError(err)

	setting := &settings.Setting{
		ID:            "setting_invoice_config",
		Key:           types.SettingKeyInvoiceConfig,
		Value:         raw,
		EnvironmentID: types.GetEnvironmentID(s.ctx),
		BaseModel:     types.GetDefaultBaseModel(s.ctx),
	}
	setting.Status = types.StatusPublished
	s.Require().NoError(s.GetStores().SettingsRepo.Create(s.ctx, setting))
}
