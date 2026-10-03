package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type FXRateCRUDSuite struct {
	testutil.BaseServiceTestSuite
	svc FXRateService
}

func TestFXRateCRUD(t *testing.T) {
	suite.Run(t, new(FXRateCRUDSuite))
}

func (s *FXRateCRUDSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewFXRateService(ServiceParams{
		Logger:           s.GetLogger(),
		DB:               s.GetDB(),
		FXRateRepo:       s.GetStores().FXRateRepo,
		SubRepo:          s.GetStores().SubscriptionRepo,
		CustomerRepo:     s.GetStores().CustomerRepo,
		SettingsRepo:     s.GetStores().SettingsRepo,
		WebhookPublisher: s.GetWebhookPublisher(),
	})
}

func (s *FXRateCRUDSuite) TestCreate_CustomerScopeRequiresExistingCustomer() {
	s.ClearStores()
	s.createTenantRate("usd", "inr", "83")
	// no customer row for "ghost"
	_, err := s.svc.CreateFXRate(s.GetContext(), dto.CreateFXRateRequest{
		Scope: types.FXRateScopeCustomer, ScopeID: "ghost", FromCurrency: "usd", ToCurrency: "inr", Rate: "84",
	})
	s.Error(err, "a customer-scope rate for a non-existent customer must be rejected")
	s.True(ierr.IsValidation(err) || ierr.IsNotFound(err))
}

func (s *FXRateCRUDSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

// --- helpers ---

func (s *FXRateCRUDSuite) createTenantRate(from, to, rate string) {
	_, err := s.svc.CreateFXRate(s.GetContext(), dto.CreateFXRateRequest{
		Scope: types.FXRateScopeTenant, FromCurrency: from, ToCurrency: to, Rate: rate,
	})
	s.NoError(err)
}

func (s *FXRateCRUDSuite) createOverride(scope types.FXRateScope, scopeID, from, to, rate string, startDate *time.Time) *dto.FXRateResponse {
	resp, err := s.svc.CreateFXRate(s.GetContext(), dto.CreateFXRateRequest{
		Scope: scope, ScopeID: scopeID, FromCurrency: from, ToCurrency: to, Rate: rate, StartDate: startDate,
	})
	s.NoError(err)
	return resp
}

func (s *FXRateCRUDSuite) seedCustomer(id string) {
	ctx := s.GetContext()
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, &customer.Customer{
		ID:            id,
		ExternalID:    "ext_" + id,
		Name:          id,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))
}

func (s *FXRateCRUDSuite) seedSubscription(id, currency string) {
	ctx := s.GetContext()
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, &subscription.Subscription{
		ID:                 id,
		CustomerID:         "cust_a",
		Currency:           currency,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
	}))
}

func (s *FXRateCRUDSuite) seedCustomCurrency() {
	cfg := types.CustomCurrencyConfig{
		CustomCurrencies: map[string]types.CustomCurrencyDefinition{
			"mac": {
				Name:                  "Custom AI Credits",
				Symbol:                "MAC",
				FiatConversionFactors: map[string]decimal.Decimal{"usd": decimal.NewFromFloat(0.1)},
			},
		},
		DefaultFiatCurrency: "usd",
	}
	s.NoError(cfg.Validate())
	value, err := utils.ToMap(cfg)
	s.NoError(err)
	s.NoError(s.GetStores().SettingsRepo.Create(s.GetContext(), &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		Key:           types.SettingKeyCustomCurrencyConfig,
		Value:         value,
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}))
}

// --- tests ---

func (s *FXRateCRUDSuite) TestCreateFXRate() {
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		setup   func()
		req     dto.CreateFXRateRequest
		wantErr bool
		check   func(resp *dto.FXRateResponse)
	}{
		{
			name: "tenant rate success stores lowercase",
			req:  dto.CreateFXRateRequest{Scope: types.FXRateScopeTenant, FromCurrency: "USD", ToCurrency: "INR", Rate: "83.00"},
			check: func(resp *dto.FXRateResponse) {
				s.Equal("usd", resp.FromCurrency)
				s.Equal("inr", resp.ToCurrency)
				s.Equal(types.FXRateScopeIDTenant, resp.ScopeID)
				s.True(decimal.RequireFromString("83").Equal(resp.Rate))
			},
		},
		{
			name:    "rejects same currency",
			req:     dto.CreateFXRateRequest{Scope: types.FXRateScopeTenant, FromCurrency: "usd", ToCurrency: "usd", Rate: "1"},
			wantErr: true,
		},
		{
			name:    "rejects non-positive rate",
			req:     dto.CreateFXRateRequest{Scope: types.FXRateScopeTenant, FromCurrency: "usd", ToCurrency: "inr", Rate: "0"},
			wantErr: true,
		},
		{
			name:    "rejects unsupported currency",
			req:     dto.CreateFXRateRequest{Scope: types.FXRateScopeTenant, FromCurrency: "usd", ToCurrency: "xyz", Rate: "1"},
			wantErr: true,
		},
		{
			name:    "override requires a tenant rate",
			setup:   func() { s.seedCustomer("cust_a") },
			req:     dto.CreateFXRateRequest{Scope: types.FXRateScopeCustomer, ScopeID: "cust_a", FromCurrency: "usd", ToCurrency: "inr", Rate: "84.5"},
			wantErr: true,
		},
		{
			name:    "duplicate tenant rate rejected",
			setup:   func() { s.createTenantRate("usd", "inr", "83") },
			req:     dto.CreateFXRateRequest{Scope: types.FXRateScopeTenant, FromCurrency: "usd", ToCurrency: "inr", Rate: "84"},
			wantErr: true,
		},
		{
			name: "overlapping override windows rejected",
			setup: func() {
				s.seedCustomer("cust_a")
				s.createTenantRate("usd", "inr", "83")
				s.createOverride(types.FXRateScopeCustomer, "cust_a", "usd", "inr", "84", nil) // open-ended
			},
			req:     dto.CreateFXRateRequest{Scope: types.FXRateScopeCustomer, ScopeID: "cust_a", FromCurrency: "usd", ToCurrency: "inr", Rate: "85", StartDate: &nov},
			wantErr: true,
		},
		{
			name:    "rejects custom currency",
			setup:   func() { s.seedCustomCurrency() },
			req:     dto.CreateFXRateRequest{Scope: types.FXRateScopeTenant, FromCurrency: "mac", ToCurrency: "inr", Rate: "10"},
			wantErr: true,
		},
		{
			name: "subscription scope currency mismatch",
			setup: func() {
				s.createTenantRate("usd", "inr", "83")
				s.seedSubscription("subs_eur", "eur")
			},
			req:     dto.CreateFXRateRequest{Scope: types.FXRateScopeSubscription, ScopeID: "subs_eur", FromCurrency: "usd", ToCurrency: "inr", Rate: "83"},
			wantErr: true,
		},
		{
			name: "subscription scope currency match succeeds",
			setup: func() {
				s.createTenantRate("usd", "inr", "83")
				s.seedSubscription("subs_usd", "usd")
			},
			req: dto.CreateFXRateRequest{Scope: types.FXRateScopeSubscription, ScopeID: "subs_usd", FromCurrency: "usd", ToCurrency: "inr", Rate: "82"},
			check: func(resp *dto.FXRateResponse) {
				s.Equal(types.FXRateScopeSubscription, resp.Scope)
			},
		},
		{
			name: "source defaults to fixed",
			req:  dto.CreateFXRateRequest{Scope: types.FXRateScopeTenant, FromCurrency: "usd", ToCurrency: "inr", Rate: "83"},
			check: func(resp *dto.FXRateResponse) {
				s.Equal(types.FXRateSourceFixed, resp.Source)
			},
		},
		{
			name: "market rate needs no rate value",
			req:  dto.CreateFXRateRequest{Scope: types.FXRateScopeTenant, Source: types.FXRateSourceMarket, FromCurrency: "usd", ToCurrency: "inr"},
			check: func(resp *dto.FXRateResponse) {
				s.Equal(types.FXRateSourceMarket, resp.Source)
			},
		},
		{
			name:    "fixed rate requires a rate value",
			req:     dto.CreateFXRateRequest{Scope: types.FXRateScopeTenant, FromCurrency: "usd", ToCurrency: "inr"},
			wantErr: true,
		},
		{
			name:    "rejects invalid source",
			req:     dto.CreateFXRateRequest{Scope: types.FXRateScopeTenant, Source: types.FXRateSource("live"), FromCurrency: "usd", ToCurrency: "inr", Rate: "83"},
			wantErr: true,
		},
	}

	for _, c := range cases {
		s.Run(c.name, func() {
			s.ClearStores()
			if c.setup != nil {
				c.setup()
			}
			resp, err := s.svc.CreateFXRate(s.GetContext(), c.req)
			if c.wantErr {
				s.Error(err)
				s.True(ierr.IsValidation(err) || ierr.IsAlreadyExists(err), "unexpected error kind: %v", err)
				return
			}
			s.NoError(err)
			if c.check != nil {
				c.check(resp)
			}
		})
	}
}

func (s *FXRateCRUDSuite) TestUpdateFXRate() {
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		setup   func() string // returns the id to update
		req     dto.UpdateFXRateRequest
		wantErr bool
		check   func(id string)
	}{
		{
			name: "tenant rate rejects validity window",
			setup: func() string {
				s.createTenantRate("usd", "inr", "83")
				return s.onlyRateID()
			},
			req:     dto.UpdateFXRateRequest{EndDate: &nov},
			wantErr: true,
		},
		{
			name: "tenant rate updates value",
			setup: func() string {
				s.createTenantRate("usd", "inr", "83")
				return s.onlyRateID()
			},
			req: dto.UpdateFXRateRequest{Rate: lo.ToPtr("90")},
			check: func(id string) {
				got, err := s.svc.GetFXRate(s.GetContext(), id)
				s.NoError(err)
				s.True(decimal.RequireFromString("90").Equal(got.Rate))
			},
		},
		{
			name: "rejects invalid rate",
			setup: func() string {
				s.createTenantRate("usd", "inr", "83")
				return s.onlyRateID()
			},
			req:     dto.UpdateFXRateRequest{Rate: lo.ToPtr("-5")},
			wantErr: true,
		},
		{
			name: "rejects updating an archived override",
			setup: func() string {
				s.seedCustomer("cust_a")
				s.createTenantRate("usd", "inr", "83")
				resp, err := s.svc.CreateFXRate(s.GetContext(), dto.CreateFXRateRequest{
					Scope: types.FXRateScopeCustomer, ScopeID: "cust_a",
					FromCurrency: "usd", ToCurrency: "inr", Rate: "84",
				})
				s.Require().NoError(err)
				s.Require().NoError(s.svc.DeleteFXRate(s.GetContext(), resp.ID))
				return resp.ID
			},
			req:     dto.UpdateFXRateRequest{Rate: lo.ToPtr("85")},
			wantErr: true,
		},
	}

	for _, c := range cases {
		s.Run(c.name, func() {
			s.ClearStores()
			id := c.setup()
			_, err := s.svc.UpdateFXRate(s.GetContext(), id, c.req)
			if c.wantErr {
				s.Error(err)
				s.True(ierr.IsValidation(err), "unexpected error kind: %v", err)
				return
			}
			s.NoError(err)
			if c.check != nil {
				c.check(id)
			}
		})
	}
}

func (s *FXRateCRUDSuite) TestDeleteFXRate() {
	cases := []struct {
		name    string
		setup   func() string // returns id to delete
		wantErr bool
		check   func()
	}{
		{
			name: "tenant rate cannot be deleted",
			setup: func() string {
				s.createTenantRate("usd", "inr", "83")
				return s.onlyRateID()
			},
			wantErr: true,
		},
		{
			name: "override is soft-archived",
			setup: func() string {
				s.seedCustomer("cust_a")
				s.createTenantRate("usd", "inr", "83")
				return s.createOverride(types.FXRateScopeCustomer, "cust_a", "usd", "inr", "84", nil).ID
			},
			check: func() {
				scope := types.FXRateScopeCustomer
				list, err := s.svc.ListFXRates(s.GetContext(), &types.FXRateFilter{QueryFilter: types.NewNoLimitQueryFilter(), Scope: &scope})
				s.NoError(err)
				s.Empty(list.Items, "archived override is hidden by default")
			},
		},
	}

	for _, c := range cases {
		s.Run(c.name, func() {
			s.ClearStores()
			id := c.setup()
			err := s.svc.DeleteFXRate(s.GetContext(), id)
			if c.wantErr {
				s.Error(err)
				s.True(ierr.IsValidation(err), "unexpected error kind: %v", err)
				return
			}
			s.NoError(err)
			if c.check != nil {
				c.check()
			}
		})
	}
}

// onlyRateID returns the id of the single tenant rate currently stored.
func (s *FXRateCRUDSuite) onlyRateID() string {
	scope := types.FXRateScopeTenant
	list, err := s.svc.ListFXRates(s.GetContext(), &types.FXRateFilter{QueryFilter: types.NewNoLimitQueryFilter(), Scope: &scope})
	s.NoError(err)
	s.Len(list.Items, 1)
	return list.Items[0].ID
}
