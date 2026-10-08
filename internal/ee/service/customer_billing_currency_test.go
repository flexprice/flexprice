package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/fxrate"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"time"
)

func (s *CustomerServiceSuite) seedCustomerRow(id string) {
	s.NoError(s.GetStores().CustomerRepo.Create(s.ctx, &domainCustomer.Customer{
		ID:            id,
		ExternalID:    id,
		Name:          "BC " + id,
		EnvironmentID: types.GetEnvironmentID(s.ctx),
		BaseModel:     types.GetDefaultBaseModel(s.ctx),
	}))
}

func (s *CustomerServiceSuite) seedSubscriptionRow(id, customerID, currency string, status types.SubscriptionStatus) {
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.ctx, &subscription.Subscription{
		ID:                 id,
		CustomerID:         customerID,
		SubscriptionStatus: status,
		Currency:           currency,
		BillingAnchor:      time.Now().UTC(),
		StartDate:          time.Now().UTC(),
		CurrentPeriodStart: time.Now().UTC(),
		CurrentPeriodEnd:   time.Now().UTC().AddDate(0, 1, 0),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		EnvironmentID:      types.GetEnvironmentID(s.ctx),
		BaseModel:          types.GetDefaultBaseModel(s.ctx),
	}))
}

func (s *CustomerServiceSuite) seedWalletRow(id, customerID, currency string) {
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.ctx, &wallet.Wallet{
		ID:            id,
		CustomerID:    customerID,
		Currency:      currency,
		WalletStatus:  types.WalletStatusActive,
		EnvironmentID: types.GetEnvironmentID(s.ctx),
		BaseModel:     types.GetDefaultBaseModel(s.ctx),
	}))
}

func (s *CustomerServiceSuite) seedOpenCheckout(customerID string) {
	s.NoError(s.GetStores().CheckoutSessionRepo.Create(s.ctx, &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		CustomerID:      customerID,
		Action:          types.CheckoutActionCreateSubscription,
		CheckoutStatus:  types.CheckoutStatusPending,
		PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		ExpiresAt:       time.Now().UTC().Add(15 * time.Minute),
		EnvironmentID:   types.GetEnvironmentID(s.ctx),
		BaseModel:       types.GetDefaultBaseModel(s.ctx),
	}))
}

func (s *CustomerServiceSuite) seedTenantRate(from, to, rate string) {
	s.NoError(s.GetStores().FXRateRepo.Create(s.ctx, &fxrate.FXRate{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_FX_RATE),
		Scope:         types.FXRateScopeTenant,
		ScopeID:       types.FXRateScopeIDTenant,
		FromCurrency:  from,
		ToCurrency:    to,
		Rate:          decimal.RequireFromString(rate),
		EnvironmentID: types.GetEnvironmentID(s.ctx),
		BaseModel:     types.GetDefaultBaseModel(s.ctx),
	}))
}

func (s *CustomerServiceSuite) seedCustomCurrency(code, fiat string, factor string) {
	cfg := types.CustomCurrencyConfig{
		CustomCurrencies: map[string]types.CustomCurrencyDefinition{
			code: {
				Name:                  "Custom " + code,
				Symbol:                code,
				FiatConversionFactors: map[string]decimal.Decimal{fiat: decimal.RequireFromString(factor)},
			},
		},
		DefaultFiatCurrency: fiat,
	}
	s.NoError(cfg.Validate())
	value, err := utils.ToMap(cfg)
	s.NoError(err)
	s.NoError(s.GetStores().SettingsRepo.Create(s.ctx, &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		Key:           types.SettingKeyCustomCurrencyConfig,
		Value:         value,
		EnvironmentID: types.GetEnvironmentID(s.ctx),
		BaseModel:     types.GetDefaultBaseModel(s.ctx),
	}))
}

// TestSetBillingCurrency_Guardrails covers the billing-currency checks on UpdateCustomer.
func (s *CustomerServiceSuite) TestSetBillingCurrency_Guardrails() {
	const custID = "cust_bc_main"

	cases := []struct {
		name      string
		billing   string
		setup     func()
		expectErr bool
	}{
		{
			name:    "valid fiat, no subscriptions or wallets",
			billing: "inr",
			setup:   func() {},
		},
		{
			name:      "invalid non-fiat currency rejected",
			billing:   "zzz",
			setup:     func() {},
			expectErr: true,
		},
		{
			name:    "custom currency as billing currency rejected",
			billing: "mac",
			setup: func() {
				s.seedCustomCurrency("mac", "usd", "0.1")
			},
			expectErr: true,
		},
		{
			name:    "subscription in different currency without tenant rate rejected",
			billing: "inr",
			setup: func() {
				s.seedSubscriptionRow("sub_usd", custID, "usd", types.SubscriptionStatusActive)
			},
			expectErr: true,
		},
		{
			name:    "subscription in different currency with tenant rate allowed",
			billing: "inr",
			setup: func() {
				s.seedSubscriptionRow("sub_usd", custID, "usd", types.SubscriptionStatusActive)
				s.seedTenantRate("usd", "inr", "83")
			},
		},
		{
			name:    "paused subscription is also checked",
			billing: "inr",
			setup: func() {
				s.seedSubscriptionRow("sub_paused", custID, "eur", types.SubscriptionStatusPaused)
			},
			expectErr: true,
		},
		{
			name:    "cancelled subscription is ignored",
			billing: "inr",
			setup: func() {
				s.seedSubscriptionRow("sub_cancelled", custID, "eur", types.SubscriptionStatusCancelled)
			},
		},
		{
			name:    "custom-currency subscription with factor for billing currency allowed",
			billing: "usd",
			setup: func() {
				s.seedCustomCurrency("mac", "usd", "0.1")
				s.seedSubscriptionRow("sub_mac", custID, "mac", types.SubscriptionStatusActive)
			},
		},
		{
			name:    "custom-currency subscription without factor rejected",
			billing: "inr",
			setup: func() {
				s.seedCustomCurrency("mac", "usd", "0.1") // factor only to usd, not inr
				s.seedSubscriptionRow("sub_mac", custID, "mac", types.SubscriptionStatusActive)
			},
			expectErr: true,
		},
		{
			name:    "wallet in different currency without rate rejected",
			billing: "inr",
			setup: func() {
				s.seedWalletRow("wal_usd", custID, "usd")
			},
			expectErr: true,
		},
		{
			name:    "wallet in different currency with rate allowed",
			billing: "inr",
			setup: func() {
				s.seedWalletRow("wal_usd", custID, "usd")
				s.seedTenantRate("usd", "inr", "83")
			},
		},
		{
			name:    "open checkout session blocks the change",
			billing: "inr",
			setup: func() {
				s.seedOpenCheckout(custID)
			},
			expectErr: true,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.seedCustomerRow(custID)
			tc.setup()

			_, err := s.service.UpdateCustomer(s.ctx, custID, dto.UpdateCustomerRequest{
				BillingCurrency: lo.ToPtr(tc.billing),
			})
			if tc.expectErr {
				s.Error(err, "expected billing currency change to be rejected")
				s.True(ierr.IsValidation(err), "expected a validation error, got %v", err)
			} else {
				s.NoError(err)
				got, gerr := s.service.GetCustomer(s.ctx, custID)
				s.NoError(gerr)
				s.NotNil(got.BillingCurrency)
				s.Equal(tc.billing, *got.BillingCurrency)
			}
		})
	}
}

// TestSetBillingCurrency_ClearToNull: clearing is allowed with no open checkout, blocked with one.
func (s *CustomerServiceSuite) TestSetBillingCurrency_ClearToNull() {
	const custID = "cust_bc_clear"
	s.seedCustomerRow(custID)
	// first set it
	_, err := s.service.UpdateCustomer(s.ctx, custID, dto.UpdateCustomerRequest{BillingCurrency: lo.ToPtr("inr")})
	s.NoError(err)

	// clear it back to null
	_, err = s.service.UpdateCustomer(s.ctx, custID, dto.UpdateCustomerRequest{BillingCurrency: lo.ToPtr("")})
	s.NoError(err)
	got, gerr := s.service.GetCustomer(s.ctx, custID)
	s.NoError(gerr)
	s.Nil(got.BillingCurrency, "billing currency should be cleared")
}

// TestSetBillingCurrency_UnrelatedUpdateNotBlockedByCheckout: an update that does not touch the
// billing currency proceeds even while a checkout session is open.
func (s *CustomerServiceSuite) TestSetBillingCurrency_UnrelatedUpdateNotBlockedByCheckout() {
	const custID = "cust_bc_unrelated"
	s.seedCustomerRow(custID)
	s.seedOpenCheckout(custID)

	_, err := s.service.UpdateCustomer(s.ctx, custID, dto.UpdateCustomerRequest{
		Name: lo.ToPtr("Renamed During Checkout"),
	})
	s.NoError(err, "an update that does not change billing currency must not be blocked by an open checkout")
}

// TestCreateCustomer_WithBillingCurrency stores a normalized (lowercase) billing currency.
func (s *CustomerServiceSuite) TestCreateCustomer_WithBillingCurrency() {
	resp, err := s.service.CreateCustomer(s.ctx, dto.CreateCustomerRequest{
		ExternalID:      "ext-bc-create",
		Name:            "Created With BC",
		BillingCurrency: lo.ToPtr("INR"),
	})
	s.NoError(err)
	s.NotNil(resp.BillingCurrency)
	s.Equal("inr", *resp.BillingCurrency)
}

// TestCreateCustomer_InvalidBillingCurrencyRejected rejects a non-fiat currency at create.
func (s *CustomerServiceSuite) TestCreateCustomer_InvalidBillingCurrencyRejected() {
	_, err := s.service.CreateCustomer(s.ctx, dto.CreateCustomerRequest{
		ExternalID:      "ext-bc-bad",
		Name:            "Bad BC",
		BillingCurrency: lo.ToPtr("zzz"),
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

// failingFXRateRepo fails every tenant-rate read the way an outage would.
type failingFXRateRepo struct{ fxrate.Repository }

func (failingFXRateRepo) GetTenantRate(_ context.Context, _, _ string) (*fxrate.FXRate, error) {
	return nil, ierr.NewError("connection refused").Mark(ierr.ErrDatabase)
}

// TestSetBillingCurrency_RateLookupErrorSurfaces: an infrastructure failure reading rates is returned
// as such, not reported to the caller as a missing exchange rate.
func (s *CustomerServiceSuite) TestSetBillingCurrency_RateLookupErrorSurfaces() {
	const custID = "cust_bc_dberr"
	s.seedCustomerRow(custID)
	s.seedSubscriptionRow("sub_bc_dberr", custID, "usd", types.SubscriptionStatusActive)

	params := s.service.(*customerService).ServiceParams
	params.FXRateRepo = failingFXRateRepo{}
	svc := NewCustomerService(params)

	_, err := svc.UpdateCustomer(s.ctx, custID, dto.UpdateCustomerRequest{BillingCurrency: lo.ToPtr("inr")})
	s.Require().Error(err)
	s.True(ierr.IsDatabase(err), "want the database error, got %v", err)
	s.False(ierr.IsValidation(err), "must not be reported as a missing rate: %v", err)
}

// TestSetBillingCurrency_UnchangedValueNotBlockedByCheckout: a client that resends the full customer
// object (billing currency unchanged, even differently cased) is not blocked by an open checkout.
func (s *CustomerServiceSuite) TestSetBillingCurrency_UnchangedValueNotBlockedByCheckout() {
	const custID = "cust_bc_same"
	s.seedCustomerRow(custID)
	_, err := s.service.UpdateCustomer(s.ctx, custID, dto.UpdateCustomerRequest{BillingCurrency: lo.ToPtr("inr")})
	s.Require().NoError(err)
	s.seedOpenCheckout(custID)

	_, err = s.service.UpdateCustomer(s.ctx, custID, dto.UpdateCustomerRequest{
		Name:            lo.ToPtr("Renamed During Checkout"),
		BillingCurrency: lo.ToPtr(" INR "),
	})
	s.NoError(err, "resending the same billing currency must not be blocked by an open checkout")

	// A real change is still blocked.
	_, err = s.service.UpdateCustomer(s.ctx, custID, dto.UpdateCustomerRequest{BillingCurrency: lo.ToPtr("usd")})
	s.Error(err)
	s.True(ierr.IsValidation(err), "changing the value during checkout stays blocked, got %v", err)
}

// A subscription the customer holds but another customer pays for is invoiced to the payer, so it
// does not need a rate to the customer's billing currency; one it pays for on another's behalf does.
func (s *CustomerServiceSuite) TestSetBillingCurrency_OnlyChecksSubscriptionsInvoicedToCustomer() {
	const childID, parentID = "cust_bc_child", "cust_bc_parent"
	s.seedCustomerRow(childID)
	s.seedCustomerRow(parentID)
	s.seedSubscriptionRow("sub_bc_child", childID, "usd", types.SubscriptionStatusActive)
	sub, err := s.GetStores().SubscriptionRepo.Get(s.ctx, "sub_bc_child")
	s.Require().NoError(err)
	sub.InvoicingCustomerID = lo.ToPtr(parentID)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.ctx, sub))

	_, err = s.service.UpdateCustomer(s.ctx, childID, dto.UpdateCustomerRequest{BillingCurrency: lo.ToPtr("inr")})
	s.NoError(err, "the child's sub is invoiced to the parent; no usd->inr rate is needed for the child")

	_, err = s.service.UpdateCustomer(s.ctx, parentID, dto.UpdateCustomerRequest{BillingCurrency: lo.ToPtr("inr")})
	s.Require().Error(err, "the parent pays for the usd sub, so it needs a usd->inr rate")
	s.True(ierr.IsValidation(err), "got %v", err)
}
