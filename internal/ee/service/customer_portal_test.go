package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type CustomerPortalServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  CustomerPortalService
	testData struct {
		customer      *customer.Customer
		otherCustomer *customer.Customer
		wallet        *wallet.Wallet
		otherWallet   *wallet.Wallet
	}
}

func TestCustomerPortalService(t *testing.T) {
	suite.Run(t, new(CustomerPortalServiceSuite))
}

func (s *CustomerPortalServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *CustomerPortalServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.BaseServiceTestSuite.ClearStores()
}

// GetContext returns a context with the environment set, matching the other service suites.
func (s *CustomerPortalServiceSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_test")
}

// portalContext mimics what SessionTokenAuthMiddleware puts on the request context.
func (s *CustomerPortalServiceSuite) portalContext(customerID string) context.Context {
	return context.WithValue(s.GetContext(), types.CtxCustomerID, customerID)
}

func (s *CustomerPortalServiceSuite) buildServiceParams() ServiceParams {
	stores := s.GetStores()
	pubsub := testutil.NewInMemoryPubSub()
	return ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		RedisCache:                   s.GetRedisCache(),
		WalletRepo:                   stores.WalletRepo,
		SubRepo:                      stores.SubscriptionRepo,
		SubscriptionLineItemRepo:     stores.SubscriptionLineItemRepo,
		PlanRepo:                     stores.PlanRepo,
		PriceRepo:                    stores.PriceRepo,
		EventRepo:                    stores.EventRepo,
		MeterUsageRepo:               stores.MeterUsageRepo,
		MeterRepo:                    stores.MeterRepo,
		CustomerRepo:                 stores.CustomerRepo,
		InvoiceRepo:                  stores.InvoiceRepo,
		InvoiceLineItemRepo:          stores.InvoiceLineItemRepo,
		EntitlementRepo:              stores.EntitlementRepo,
		EntitlementGrantRepo:         stores.EntitlementGrantRepo,
		FeatureRepo:                  stores.FeatureRepo,
		AddonAssociationRepo:         stores.AddonAssociationRepo,
		SettingsRepo:                 stores.SettingsRepo,
		AlertLogsRepo:                stores.AlertLogsRepo,
		PaymentRepo:                  stores.PaymentRepo,
		CheckoutSessionRepo:          stores.CheckoutSessionRepo,
		CouponRepo:                   stores.CouponRepo,
		CouponAssociationRepo:        stores.CouponAssociationRepo,
		CouponApplicationRepo:        stores.CouponApplicationRepo,
		TaxAssociationRepo:           stores.TaxAssociationRepo,
		TaxRateRepo:                  stores.TaxRateRepo,
		TaxAppliedRepo:               stores.TaxAppliedRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		WalletBalanceAlertPubSub:     types.WalletBalanceAlertPubSub{PubSub: pubsub},
		IntegrationFactory:           s.GetIntegrationFactory(),
		ConnectionRepo:               stores.ConnectionRepo,
		EntityIntegrationMappingRepo: stores.EntityIntegrationMappingRepo,
	}
}

func (s *CustomerPortalServiceSuite) setupService() {
	params := s.buildServiceParams()
	s.service = NewCustomerPortalService(
		params,
		NewCustomerService(params),
		// Analytics is not exercised by these tests; the portal service only
		// forwards to it from the analytics endpoints.
		NewRevenueAnalyticsService(params, nil),
	)
}

func (s *CustomerPortalServiceSuite) setupTestData() {
	ctx := s.GetContext()

	s.testData.customer = &customer.Customer{
		ID:         "cust_portal",
		ExternalID: "ext_cust_portal",
		Name:       "Portal Customer",
		Email:      "portal@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))

	s.testData.otherCustomer = &customer.Customer{
		ID:         "cust_other",
		ExternalID: "ext_cust_other",
		Name:       "Other Customer",
		Email:      "other@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.otherCustomer))

	s.testData.wallet = &wallet.Wallet{
		ID:             "wallet_portal",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromInt(100),
		CreditBalance:  decimal.NewFromInt(100),
		WalletStatus:   types.WalletStatusActive,
		ConversionRate: decimal.NewFromInt(1),
		WalletType:     types.WalletTypePrePaid,
		Config:         lo.FromPtr(types.GetDefaultWalletConfig()),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(ctx, s.testData.wallet))

	s.testData.otherWallet = &wallet.Wallet{
		ID:             "wallet_other",
		CustomerID:     s.testData.otherCustomer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromInt(500),
		CreditBalance:  decimal.NewFromInt(500),
		WalletStatus:   types.WalletStatusActive,
		ConversionRate: decimal.NewFromInt(1),
		WalletType:     types.WalletTypePrePaid,
		Config:         lo.FromPtr(types.GetDefaultWalletConfig()),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(ctx, s.testData.otherWallet))
}

func (s *CustomerPortalServiceSuite) TestTopUpWalletRejectsAnotherCustomersWallet() {
	ctx := s.portalContext(s.testData.customer.ID)

	_, err := s.service.TopUpWallet(ctx, s.testData.otherWallet.ID, dto.PortalTopUpWalletRequest{
		CreditsToAdd:   decimal.NewFromInt(10),
		IdempotencyKey: lo.ToPtr("idem_other_wallet"),
	})

	s.Error(err)
	s.True(ierr.IsNotFound(err), "expected a not-found error, got %v", err)

	// The other customer's balance must be untouched.
	w, getErr := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.otherWallet.ID)
	s.NoError(getErr)
	s.True(w.CreditBalance.Equal(decimal.NewFromInt(500)))
}

func (s *CustomerPortalServiceSuite) TestTopUpWalletRequiresSessionCustomer() {
	// No customer on the context — i.e. no valid portal session.
	_, err := s.service.TopUpWallet(s.GetContext(), s.testData.wallet.ID, dto.PortalTopUpWalletRequest{
		CreditsToAdd:   decimal.NewFromInt(10),
		IdempotencyKey: lo.ToPtr("idem_no_session"),
	})

	s.Error(err)
	s.True(ierr.IsPermissionDenied(err), "expected permission denied, got %v", err)
}

// The portal payload has no transaction_reason field: a customer must not be able
// to mint FREE_CREDIT for themselves, so the mapping pins a purchased-credit reason.
func (s *CustomerPortalServiceSuite) TestTopUpRequestPinsPurchasedCreditReason() {
	req := dto.PortalTopUpWalletRequest{CreditsToAdd: decimal.NewFromInt(25)}

	mapped := req.ToTopUpWalletRequest()

	s.Equal(types.TransactionReasonPurchasedCreditInvoiced, mapped.TransactionReason)
	s.True(mapped.CreditsToAdd.Equal(decimal.NewFromInt(25)))
}

func (s *CustomerPortalServiceSuite) TestUpdateWalletAutoTopupRejectsAnotherCustomersWallet() {
	ctx := s.portalContext(s.testData.customer.ID)

	_, err := s.service.UpdateWalletAutoTopup(ctx, s.testData.otherWallet.ID, dto.PortalUpdateAutoTopupRequest{
		AutoTopup: &types.AutoTopup{
			Enabled:   lo.ToPtr(true),
			Threshold: lo.ToPtr(decimal.NewFromInt(10)),
			Amount:    lo.ToPtr(decimal.NewFromInt(50)),
			Invoicing: lo.ToPtr(true),
		},
	})

	s.Error(err)
	s.True(ierr.IsNotFound(err), "expected a not-found error, got %v", err)

	w, getErr := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.otherWallet.ID)
	s.NoError(getErr)
	s.Nil(w.AutoTopup)
}

func (s *CustomerPortalServiceSuite) TestUpdateWalletAutoTopupOnOwnWallet() {
	ctx := s.portalContext(s.testData.customer.ID)

	updated, err := s.service.UpdateWalletAutoTopup(ctx, s.testData.wallet.ID, dto.PortalUpdateAutoTopupRequest{
		AutoTopup: &types.AutoTopup{
			Enabled:   lo.ToPtr(true),
			Threshold: lo.ToPtr(decimal.NewFromInt(10)),
			Amount:    lo.ToPtr(decimal.NewFromInt(50)),
			Invoicing: lo.ToPtr(true),
		},
	})

	s.NoError(err)
	s.NotNil(updated.AutoTopup)
	s.True(lo.FromPtr(updated.AutoTopup.Enabled))
	s.True(updated.AutoTopup.Amount.Equal(decimal.NewFromInt(50)))
}

// The portal update path must not carry a name/config/metadata rewrite through
// to the wallet: only auto_topup is forwarded.
func (s *CustomerPortalServiceSuite) TestAutoTopupRequestOnlyForwardsAutoTopup() {
	req := dto.PortalUpdateAutoTopupRequest{
		AutoTopup: &types.AutoTopup{
			Enabled:   lo.ToPtr(true),
			Threshold: lo.ToPtr(decimal.NewFromInt(1)),
			Amount:    lo.ToPtr(decimal.NewFromInt(2)),
			Invoicing: lo.ToPtr(false),
		},
	}

	mapped := req.ToUpdateWalletRequest()

	s.NotNil(mapped.AutoTopup)
	s.Nil(mapped.Name)
	s.Nil(mapped.Description)
	s.Nil(mapped.Metadata)
	s.Nil(mapped.Config)
	s.Nil(mapped.AlertSettings)
}

func (s *CustomerPortalServiceSuite) TestPayInvoiceRequiresSessionCustomer() {
	err := s.service.PayInvoice(s.GetContext(), "inv_does_not_matter")

	s.Error(err)
	s.True(ierr.IsPermissionDenied(err), "expected permission denied, got %v", err)
}

func (s *CustomerPortalServiceSuite) TestListPaymentMethodsDefaultsToStripe() {
	req := dto.PortalListPaymentMethodsRequest{}

	mapped := req.ToListPaymentMethodsRequest()

	s.Equal(string(types.SecretProviderStripe), mapped.Provider)
}

// Add-payment-method pins off_session usage so the saved card can back auto top-up.
func (s *CustomerPortalServiceSuite) TestAddPaymentMethodRequestDefaults() {
	req := dto.PortalAddPaymentMethodRequest{}

	mapped := req.ToCreateSetupIntentRequest()

	s.Equal(types.SecretProviderStripe, mapped.Provider)
	s.Equal("off_session", mapped.Usage)
}

func (s *CustomerPortalServiceSuite) TestAddPaymentMethodRejectsNonStripeProvider() {
	ctx := s.portalContext(s.testData.customer.ID)

	_, err := s.service.AddPaymentMethod(ctx, dto.PortalAddPaymentMethodRequest{
		Provider: types.SecretProviderMoyasar,
	})

	s.Error(err)
	s.True(ierr.IsValidation(err), "expected a validation error, got %v", err)
}

func (s *CustomerPortalServiceSuite) TestSetDefaultPaymentMethodRequiresSessionCustomer() {
	err := s.service.SetDefaultPaymentMethod(s.GetContext(), dto.PortalSetDefaultPaymentMethodRequest{
		PaymentMethodID: "pm_123",
	})

	s.Error(err)
	s.True(ierr.IsPermissionDenied(err), "expected permission denied, got %v", err)
}

func (s *CustomerPortalServiceSuite) TestSetDefaultPaymentMethodRequiresPaymentMethodID() {
	ctx := s.portalContext(s.testData.customer.ID)

	err := s.service.SetDefaultPaymentMethod(ctx, dto.PortalSetDefaultPaymentMethodRequest{})

	s.Error(err)
	s.True(ierr.IsValidation(err), "expected a validation error, got %v", err)
}

// TopUpWallet falls back to a key derived from an RFC3339 timestamp when none is
// supplied, so two identical portal submissions a second apart would each grant
// credits and raise an invoice. The portal therefore requires the caller to send
// one and reuse it on retry.
func (s *CustomerPortalServiceSuite) TestTopUpWalletRequiresIdempotencyKey() {
	ctx := s.portalContext(s.testData.customer.ID)

	_, err := s.service.TopUpWallet(ctx, s.testData.wallet.ID, dto.PortalTopUpWalletRequest{
		CreditsToAdd: decimal.NewFromInt(10),
	})

	s.Error(err)
	s.True(ierr.IsValidation(err), "expected a validation error, got %v", err)
}

func (s *CustomerPortalServiceSuite) TestTopUpWalletRejectsEmptyIdempotencyKey() {
	ctx := s.portalContext(s.testData.customer.ID)

	_, err := s.service.TopUpWallet(ctx, s.testData.wallet.ID, dto.PortalTopUpWalletRequest{
		CreditsToAdd:   decimal.NewFromInt(10),
		IdempotencyKey: lo.ToPtr(""),
	})

	s.Error(err)
	s.True(ierr.IsValidation(err), "expected a validation error, got %v", err)
}

// The supplied key must reach the shared wallet request unchanged, or the dedup
// it exists to drive would key off the timestamp fallback instead.
func (s *CustomerPortalServiceSuite) TestTopUpRequestForwardsIdempotencyKey() {
	req := dto.PortalTopUpWalletRequest{
		CreditsToAdd:   decimal.NewFromInt(25),
		IdempotencyKey: lo.ToPtr("idem_abc123"),
	}

	mapped := req.ToTopUpWalletRequest()

	s.Equal("idem_abc123", lo.FromPtr(mapped.IdempotencyKey))
}
