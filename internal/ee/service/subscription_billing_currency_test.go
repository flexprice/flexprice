package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/fxrate"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type SubscriptionBillingCurrencySuite struct {
	testutil.BaseServiceTestSuite
	svc *subscriptionService
}

func TestSubscriptionBillingCurrency(t *testing.T) {
	suite.Run(t, new(SubscriptionBillingCurrencySuite))
}

func (s *SubscriptionBillingCurrencySuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewSubscriptionService(ServiceParams{
		Logger:       s.GetLogger(),
		Config:       s.GetConfig(),
		DB:           s.GetDB(),
		CustomerRepo: s.GetStores().CustomerRepo,
		FXRateRepo:   s.GetStores().FXRateRepo,
		SubRepo:      s.GetStores().SubscriptionRepo,
		SettingsRepo: s.GetStores().SettingsRepo,
	}).(*subscriptionService)
}

func (s *SubscriptionBillingCurrencySuite) ctx() context.Context {
	return types.SetEnvironmentID(s.GetContext(), "env_test")
}

func (s *SubscriptionBillingCurrencySuite) seedCustomer(id string, billing *string) *domainCustomer.Customer {
	c := &domainCustomer.Customer{
		ID:              id,
		ExternalID:      id,
		Name:            "C " + id,
		BillingCurrency: billing,
		EnvironmentID:   types.GetEnvironmentID(s.ctx()),
		BaseModel:       types.GetDefaultBaseModel(s.ctx()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.ctx(), c))
	return c
}

func (s *SubscriptionBillingCurrencySuite) seedTenantRate(from, to, rate string) {
	s.NoError(s.GetStores().FXRateRepo.Create(s.ctx(), &fxrate.FXRate{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_FX_RATE),
		Scope:         types.FXRateScopeTenant,
		ScopeID:       types.FXRateScopeIDTenant,
		FromCurrency:  from,
		ToCurrency:    to,
		Rate:          decimal.RequireFromString(rate),
		EnvironmentID: types.GetEnvironmentID(s.ctx()),
		BaseModel:     types.GetDefaultBaseModel(s.ctx()),
	}))
}

func customCfg(code, fiat, factor string) types.CustomCurrencyConfig {
	return types.CustomCurrencyConfig{
		CustomCurrencies: map[string]types.CustomCurrencyDefinition{
			code: {
				Name:                  "Custom " + code,
				Symbol:                code,
				FiatConversionFactors: map[string]decimal.Decimal{fiat: decimal.RequireFromString(factor)},
			},
		},
		DefaultFiatCurrency: fiat,
	}
}

func (s *SubscriptionBillingCurrencySuite) TestValidateSubscriptionBillingCurrency() {
	fxRate := &dto.InlineFXRate{Rate: decimal.RequireFromString("83")}

	cases := []struct {
		name       string
		billing    *string
		subCurr    string
		cfg        types.CustomCurrencyConfig
		fxRate     *dto.InlineFXRate
		setup      func()
		wantTarget string
		wantErr    bool
	}{
		{name: "no billing currency", billing: nil, subCurr: "usd", wantTarget: ""},
		{name: "billing matches subscription", billing: lo.ToPtr("usd"), subCurr: "usd", wantTarget: ""},
		{name: "no billing currency but fx_rate given is rejected", billing: nil, subCurr: "usd", fxRate: fxRate, wantErr: true},
		{name: "fiat cross-currency without tenant rate rejected", billing: lo.ToPtr("inr"), subCurr: "usd", wantErr: true},
		{
			name: "fiat cross-currency with tenant rate, no fx_rate", billing: lo.ToPtr("inr"), subCurr: "usd",
			setup: func() { s.seedTenantRate("usd", "inr", "83") }, wantTarget: "",
		},
		{
			name: "fiat cross-currency with tenant rate and fx_rate creates override target", billing: lo.ToPtr("inr"), subCurr: "usd",
			setup: func() { s.seedTenantRate("usd", "inr", "83") }, fxRate: fxRate, wantTarget: "inr",
		},
		{
			name: "custom-currency with factor allowed", billing: lo.ToPtr("usd"), subCurr: "mac",
			cfg: customCfg("mac", "usd", "0.1"), wantTarget: "",
		},
		{
			name: "custom-currency without factor rejected", billing: lo.ToPtr("inr"), subCurr: "mac",
			cfg: customCfg("mac", "usd", "0.1"), wantErr: true,
		},
		{
			name: "custom-currency with fx_rate rejected", billing: lo.ToPtr("usd"), subCurr: "mac",
			cfg: customCfg("mac", "usd", "0.1"), fxRate: fxRate, wantErr: true,
		},
	}

	for i, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			custID := "cust_sbc"
			s.seedCustomer(custID, tc.billing)
			if tc.setup != nil {
				tc.setup()
			}
			sub := &subscription.Subscription{
				ID:         "sub_sbc",
				CustomerID: custID,
				Currency:   tc.subCurr,
			}
			subscriber := &domainCustomer.Customer{ID: custID, BillingCurrency: tc.billing}

			target, err := s.svc.validateSubscriptionBillingCurrency(s.ctx(), sub, subscriber, tc.cfg, tc.fxRate)
			if tc.wantErr {
				s.Error(err, "case %d", i)
				s.True(ierr.IsValidation(err), "case %d: want validation error, got %v", i, err)
			} else {
				s.NoError(err, "case %d", i)
				s.Equal(tc.wantTarget, target, "case %d target", i)
			}
		})
	}
}
