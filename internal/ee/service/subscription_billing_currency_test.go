package service

import (
	"context"
	"testing"
	"time"

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
	fxRates := []dto.InlineFXRate{{Rate: decimal.RequireFromString("83")}}

	cases := []struct {
		name       string
		billing    *string
		subCurr    string
		cfg        types.CustomCurrencyConfig
		fxRates    []dto.InlineFXRate
		setup      func()
		wantTarget string
		wantErr    bool
	}{
		{name: "no billing currency", billing: nil, subCurr: "usd", wantTarget: ""},
		{name: "billing matches subscription", billing: lo.ToPtr("usd"), subCurr: "usd", wantTarget: ""},
		{name: "no billing currency but fx_rates given is rejected", billing: nil, subCurr: "usd", fxRates: fxRates, wantErr: true},
		{name: "fiat cross-currency without tenant rate rejected", billing: lo.ToPtr("inr"), subCurr: "usd", wantErr: true},
		{
			name: "fiat cross-currency with tenant rate, no fx_rates", billing: lo.ToPtr("inr"), subCurr: "usd",
			setup: func() { s.seedTenantRate("usd", "inr", "83") }, wantTarget: "",
		},
		{
			name: "fiat cross-currency with tenant rate and fx_rates creates override target", billing: lo.ToPtr("inr"), subCurr: "usd",
			setup: func() { s.seedTenantRate("usd", "inr", "83") }, fxRates: fxRates, wantTarget: "inr",
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
			name: "custom-currency with fx_rates rejected", billing: lo.ToPtr("usd"), subCurr: "mac",
			cfg: customCfg("mac", "usd", "0.1"), fxRates: fxRates, wantErr: true,
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

			target, err := s.svc.validateSubscriptionBillingCurrency(s.ctx(), sub, subscriber, tc.cfg, tc.fxRates)
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

func (s *SubscriptionBillingCurrencySuite) TestHandleFxOverrideCreatesEveryWindow() {
	s.seedCustomer("cust_win", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	sub := &subscription.Subscription{ID: "sub_win", CustomerID: "cust_win", Currency: "usd"}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.ctx(), sub))

	cut := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	rates := []dto.InlineFXRate{
		{Rate: decimal.RequireFromString("83.5"), EndDate: &cut},
		{Rate: decimal.RequireFromString("84"), StartDate: &cut},
	}
	s.NoError(s.svc.handleFxOverride(s.ctx(), sub, "inr", rates))

	scope := types.FXRateScopeSubscription
	scopeID := sub.ID
	got, err := s.GetStores().FXRateRepo.List(s.ctx(), &types.FXRateFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		Scope:       &scope,
		ScopeID:     &scopeID,
	})
	s.NoError(err)
	s.Len(got, 2)
	byRate := lo.KeyBy(got, func(r *fxrate.FXRate) string { return r.Rate.String() })
	s.Require().Contains(byRate, "83.5")
	s.Require().Contains(byRate, "84")
	s.Nil(byRate["83.5"].StartDate)
	s.True(byRate["83.5"].EndDate.Equal(cut))
	s.True(byRate["84"].StartDate.Equal(cut))
	s.Nil(byRate["84"].EndDate)
	for _, r := range got {
		s.Equal("usd", r.FromCurrency)
		s.Equal("inr", r.ToCurrency)
	}
}

func (s *SubscriptionBillingCurrencySuite) TestHandleFxOverrideNoTargetIsNoop() {
	sub := &subscription.Subscription{ID: "sub_noop", CustomerID: "cust_noop", Currency: "usd"}
	s.NoError(s.svc.handleFxOverride(s.ctx(), sub, "", []dto.InlineFXRate{{Rate: decimal.RequireFromString("83")}}))
	got, err := s.GetStores().FXRateRepo.List(s.ctx(), &types.FXRateFilter{QueryFilter: types.NewNoLimitQueryFilter()})
	s.NoError(err)
	s.Empty(got)
}
