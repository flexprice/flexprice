package service

import (
	"context"
	"testing"
	"time"

	fxrate "github.com/flexprice/flexprice/internal/domain/fxrate"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type FXRateResolveSuite struct {
	testutil.BaseServiceTestSuite
	svc FXRateService
}

func TestFXRateResolve(t *testing.T) {
	suite.Run(t, new(FXRateResolveSuite))
}

func (s *FXRateResolveSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewFXRateService(ServiceParams{
		Logger:     s.GetLogger(),
		DB:         s.GetDB(),
		FXRateRepo: s.GetStores().FXRateRepo,
		SubRepo:    s.GetStores().SubscriptionRepo,
	})
	s.seed()
}

func (s *FXRateResolveSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

var (
	novFirst = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	octFirst = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
)

func (s *FXRateResolveSuite) seed() {
	ctx := s.GetContext()
	repo := s.GetStores().FXRateRepo
	mk := func(id string, scope types.FXRateScope, scopeID, rate string) *fxrate.FXRate {
		return &fxrate.FXRate{
			ID: id, Scope: scope, ScopeID: scopeID,
			FromCurrency: "usd", ToCurrency: "inr", Rate: decimal.RequireFromString(rate),
			Source:        types.FXRateSourceFixed,
			EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
		}
	}
	s.NoError(repo.Create(ctx, mk("fxr_t", types.FXRateScopeTenant, types.FXRateScopeIDTenant, "83")))
	s.NoError(repo.Create(ctx, mk("fxr_c", types.FXRateScopeCustomer, "cust_a", "84.5")))
	sub := mk("fxr_s", types.FXRateScopeSubscription, "subs_p", "82")
	sub.StartDate = &octFirst
	sub.EndDate = &novFirst
	s.NoError(repo.Create(ctx, sub))
}

func (s *FXRateResolveSuite) TestResolveRate() {
	cases := []struct {
		name      string
		req       ResolveFXRateRequest
		wantRate  string
		wantScope types.FXRateScope
		wantErr   bool
	}{
		{"subscription wins", ResolveFXRateRequest{From: "usd", To: "inr", CustomerID: "cust_a", SubscriptionID: "subs_p"}, "82", "subscription", false},
		{"customer fallback", ResolveFXRateRequest{From: "usd", To: "inr", CustomerID: "cust_a"}, "84.5", "customer", false},
		{"tenant fallback", ResolveFXRateRequest{From: "usd", To: "inr"}, "83", "tenant", false},
		{"reverse pair not found", ResolveFXRateRequest{From: "inr", To: "usd"}, "", "", true},
		{"missing pair not found", ResolveFXRateRequest{From: "eur", To: "inr"}, "", "", true},
	}
	for _, c := range cases {
		s.Run(c.name, func() {
			// resolve at mid-October so the subscription window is active
			res, err := s.svc.(*fxRateService).resolveRateAt(s.GetContext(), c.req, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
			if c.wantErr {
				s.Error(err)
				s.True(ierr.IsNotFound(err))
				return
			}
			s.NoError(err)
			s.True(decimal.RequireFromString(c.wantRate).Equal(res.Rate), "rate: got %s", res.Rate)
			s.Equal(c.wantScope, res.Scope)
		})
	}
}

func (s *FXRateResolveSuite) TestResolveRate_EndDateIsExclusive() {
	// exactly at end_date (Nov 1) the subscription override no longer applies → customer 84.5
	res, err := s.svc.(*fxRateService).resolveRateAt(s.GetContext(),
		ResolveFXRateRequest{From: "usd", To: "inr", CustomerID: "cust_a", SubscriptionID: "subs_p"}, novFirst)
	s.NoError(err)
	s.Equal(types.FXRateScopeCustomer, res.Scope, "end_date is exclusive; subscription window ends at Nov 1")
	s.True(decimal.RequireFromString("84.5").Equal(res.Rate))
}

func (s *FXRateResolveSuite) TestResolveRate_ReturnsSource() {
	res, err := s.svc.ResolveRate(s.GetContext(), ResolveFXRateRequest{From: "usd", To: "inr"})
	s.NoError(err)
	s.Equal(types.FXRateSourceFixed, res.Source)
}

func (s *FXRateResolveSuite) TestResolveRate_MarketRateNotSupportedYet() {
	ctx := s.GetContext()
	market := &fxrate.FXRate{
		ID: "fxr_mkt", Scope: types.FXRateScopeTenant, ScopeID: types.FXRateScopeIDTenant,
		FromCurrency: "eur", ToCurrency: "jpy", Rate: decimal.Zero,
		Source:        types.FXRateSourceMarket,
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().FXRateRepo.Create(ctx, market))

	_, err := s.svc.ResolveRate(ctx, ResolveFXRateRequest{From: "eur", To: "jpy"})
	s.Error(err)
	s.True(ierr.IsInvalidOperation(err), "market rate resolution should be rejected until the integration lands")
}

func TestConversionAvailable(t *testing.T) {
	ctx := context.Background()
	cfg := types.CustomCurrencyConfig{}

	ok, err := conversionAvailable(ctx, ServiceParams{FXRateRepo: failingFXRateRepo{}}, cfg, "usd", "inr")
	if err == nil || !ierr.IsDatabase(err) || ok {
		t.Fatalf("db error: want (false, database error), got (%v, %v)", ok, err)
	}

	ok, err = conversionAvailable(ctx, ServiceParams{FXRateRepo: notFoundFXRateRepo{}}, cfg, "usd", "inr")
	if err != nil || ok {
		t.Fatalf("missing rate: want (false, nil), got (%v, %v)", ok, err)
	}
}

type notFoundFXRateRepo struct{ fxrate.Repository }

func (notFoundFXRateRepo) GetTenantRate(_ context.Context, _, _ string) (*fxrate.FXRate, error) {
	return nil, ierr.NewError("fx rate not found").Mark(ierr.ErrNotFound)
}
