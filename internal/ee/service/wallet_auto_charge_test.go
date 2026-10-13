package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func autoTopupWallet(cooldown *types.Duration) *wallet.Wallet {
	return &wallet.Wallet{
		ID:         "wallet_1",
		CustomerID: "cust_1",
		AutoTopup: &types.AutoTopup{
			Enabled:   lo.ToPtr(true),
			Threshold: lo.ToPtr(decimal.NewFromInt(10)),
			Amount:    lo.ToPtr(decimal.NewFromInt(20)),
			Invoicing: lo.ToPtr(true),
			Cooldown:  cooldown,
		},
	}
}

func TestWithinDefaultAutoChargeCooldown(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	recent := &wallet.Transaction{BaseModel: types.BaseModel{CreatedAt: now.Add(-time.Minute)}}
	old := &wallet.Transaction{BaseModel: types.BaseModel{CreatedAt: now.Add(-2 * time.Hour)}}

	assert.True(t, withinDefaultAutoChargeCooldown(autoTopupWallet(nil), recent, now))
	assert.False(t, withinDefaultAutoChargeCooldown(autoTopupWallet(nil), old, now))
	assert.False(t, withinDefaultAutoChargeCooldown(autoTopupWallet(nil), nil, now))

	configured := &types.Duration{Value: 5, Unit: types.DurationUnitMinute}
	assert.False(t, withinDefaultAutoChargeCooldown(autoTopupWallet(configured), recent, now),
		"a wallet with its own cooloff is governed by that, not the default")
}

func TestAutoTopupCheckoutFallsBackToInvoice(t *testing.T) {
	s := &walletService{ServiceParams: ServiceParams{Logger: logger.NewNoopLogger()}}
	ctx := context.Background()

	require.Nil(t, s.autoTopupCheckout(ctx, autoTopupWallet(nil), false),
		"direct credits are not paid for, so there is nothing to charge")
}

type WalletAutoChargeSuite struct {
	testutil.BaseServiceTestSuite
	ctx context.Context
}

func TestWalletAutoChargeSuite(t *testing.T) {
	suite.Run(t, new(WalletAutoChargeSuite))
}

func (s *WalletAutoChargeSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.ClearStores()
	s.ctx = types.SetCustomerID(s.GetContext(), "cust_1")
}

func (s *WalletAutoChargeSuite) TearDownTest() {
	s.GetIntegrationFactory().SetCheckoutProvider(nil)
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *WalletAutoChargeSuite) connect(providers ...types.SecretProvider) {
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

func (s *WalletAutoChargeSuite) buildParams() ServiceParams {
	stores := s.GetStores()
	return ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		ConnectionRepo:               stores.ConnectionRepo,
		CustomerRepo:                 stores.CustomerRepo,
		EntityIntegrationMappingRepo: stores.EntityIntegrationMappingRepo,
		IntegrationFactory:           s.GetIntegrationFactory(),
	}
}

func (s *WalletAutoChargeSuite) TestFetchGatewayWithAutoChargeSupport_NilFactoryOrConnectionRepo() {
	gw, err := fetchGatewayWithAutoChargeSupport(s.ctx, ServiceParams{}, nil, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
	s.NoError(err)
	s.Equal(types.PaymentGatewayType(""), gw)
}

func (s *WalletAutoChargeSuite) TestFetchGatewayWithAutoChargeSupport_NoConnections() {
	params := s.buildParams()
	gw, err := fetchGatewayWithAutoChargeSupport(s.ctx, params, nil, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
	s.NoError(err)
	s.Equal(types.PaymentGatewayType(""), gw)
}

func (s *WalletAutoChargeSuite) TestFetchGatewayWithAutoChargeSupport_RazorpayNotSynced() {
	s.connect(types.SecretProviderRazorpay)
	params := s.buildParams()
	gw, err := fetchGatewayWithAutoChargeSupport(s.ctx, params, nil, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
	s.NoError(err)
	s.Equal(types.PaymentGatewayType(""), gw)
}

func (s *WalletAutoChargeSuite) TestFetchGatewayWithAutoChargeSupport_ChargebeeNotSynced() {
	s.connect(types.SecretProviderChargebee)
	params := s.buildParams()
	gw, err := fetchGatewayWithAutoChargeSupport(s.ctx, params, nil, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
	s.NoError(err)
	s.Equal(types.PaymentGatewayType(""), gw)
}

func (s *WalletAutoChargeSuite) TestFetchGatewayWithAutoChargeSupport_StripeAndNomodIgnored() {
	s.connect(types.SecretProviderStripe, types.SecretProviderNomod)
	params := s.buildParams()
	gw, err := fetchGatewayWithAutoChargeSupport(s.ctx, params, nil, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
	s.NoError(err)
	s.Equal(types.PaymentGatewayType(""), gw)
}

type stubAutoChargeCheckoutProvider struct {
	hasMethod bool
	err       error
	lastReq   interfaces.HasAutoChargeableMethodRequest
}

func (p *stubAutoChargeCheckoutProvider) CreatePaymentLink(context.Context, interfaces.CheckoutProviderRequest) (*interfaces.CheckoutProviderResponse, error) {
	return nil, nil
}
func (p *stubAutoChargeCheckoutProvider) CreateAuthorizationLink(context.Context, interfaces.AuthorizationLinkRequest) (*interfaces.CheckoutProviderResponse, error) {
	return nil, nil
}
func (p *stubAutoChargeCheckoutProvider) TryAutoChargingSavedMethod(context.Context, interfaces.AuthorizationLinkRequest) (*interfaces.CheckoutProviderResponse, bool, error) {
	return nil, false, nil
}
func (p *stubAutoChargeCheckoutProvider) HasAutoChargeableMethod(_ context.Context, req interfaces.HasAutoChargeableMethodRequest) (bool, error) {
	p.lastReq = req
	return p.hasMethod, p.err
}
func (p *stubAutoChargeCheckoutProvider) FetchPaymentState(context.Context, interfaces.PaymentStateRequest) (*interfaces.PaymentState, error) {
	return nil, nil
}

func (s *WalletAutoChargeSuite) TestFetchGatewayWithAutoChargeSupport_Success() {
	s.connect(types.SecretProviderChargebee)
	provider := &stubAutoChargeCheckoutProvider{hasMethod: true}
	s.GetIntegrationFactory().SetCheckoutProvider(provider)

	params := s.buildParams()
	amount := decimal.NewFromInt(200)
	gw, err := fetchGatewayWithAutoChargeSupport(s.ctx, params, nil, interfaces.HasAutoChargeableMethodRequest{
		CustomerID: "cust_1",
		Amount:     &amount,
	})
	s.NoError(err)
	s.Equal(types.PaymentGatewayTypeChargebee, gw)
	s.Equal("cust_1", provider.lastReq.CustomerID)
	s.NotNil(provider.lastReq.Amount)
	s.Equal(amount, *provider.lastReq.Amount)
}
