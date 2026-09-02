package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type PaymentProviderResolverSuite struct {
	testutil.BaseServiceTestSuite
	svc interfaces.PaymentProviderResolver
}

func TestPaymentProviderResolverSuite(t *testing.T) {
	suite.Run(t, new(PaymentProviderResolverSuite))
}

func (s *PaymentProviderResolverSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.ClearStores()
	s.svc = NewPaymentProviderResolver(ServiceParams{
		Logger:         s.GetLogger(),
		Config:         s.GetConfig(),
		DB:             s.GetDB(),
		ConnectionRepo: s.GetStores().ConnectionRepo,
	})
}

func (s *PaymentProviderResolverSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *PaymentProviderResolverSuite) connect(providers ...types.SecretProvider) {
	ctx := s.GetContext()
	for i, p := range providers {
		conn := &connection.Connection{
			ID:            "conn_" + string(p),
			Name:          string(p),
			ProviderType:  p,
			EnvironmentID: types.GetEnvironmentID(ctx),
			BaseModel:     types.GetDefaultBaseModel(ctx),
		}
		conn.Status = types.StatusPublished
		s.NoError(s.GetStores().ConnectionRepo.Create(ctx, conn), "seed connection %d", i)
	}
}

func (s *PaymentProviderResolverSuite) TestResolvesSoleCandidate() {
	s.connect(types.SecretProviderChargebee)

	gw, err := s.svc.ResolveProvider(s.GetContext(), "cust_1", types.IntegrationCapabilityCheckout, "")
	s.NoError(err)
	s.Equal(types.PaymentGatewayTypeChargebee, gw)
}

// The capability intersection is the point of the resolver: a Chargebee+Stripe
// tenant has exactly one checkout provider and exactly one payment-link provider,
// and they are different gateways.
func (s *PaymentProviderResolverSuite) TestIntersectionNarrowsPerCapability() {
	s.connect(types.SecretProviderChargebee, types.SecretProviderStripe)
	ctx := s.GetContext()

	gw, err := s.svc.ResolveProvider(ctx, "cust_1", types.IntegrationCapabilityCheckout, "")
	s.NoError(err)
	s.Equal(types.PaymentGatewayTypeChargebee, gw)

	// Both do payment links, so that capability is contested and must be chosen.
	_, err = s.svc.ResolveProvider(ctx, "cust_1", types.IntegrationCapabilityPaymentLink, "")
	s.True(ierr.IsValidation(err))

	gw, err = s.svc.ResolveProvider(ctx, "cust_1", types.IntegrationCapabilityPaymentMethodManagement, "")
	s.NoError(err)
	s.Equal(types.PaymentGatewayTypeChargebee, gw)
}

func (s *PaymentProviderResolverSuite) TestAmbiguousWithoutRequest() {
	s.connect(types.SecretProviderChargebee, types.SecretProviderRazorpay)

	_, err := s.svc.ResolveProvider(s.GetContext(), "cust_1", types.IntegrationCapabilityCheckout, "")
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *PaymentProviderResolverSuite) TestRequestPinsAmongCandidates() {
	s.connect(types.SecretProviderChargebee, types.SecretProviderRazorpay)

	gw, err := s.svc.ResolveProvider(s.GetContext(), "cust_1",
		types.IntegrationCapabilityCheckout, types.PaymentGatewayTypeRazorpay)
	s.NoError(err)
	s.Equal(types.PaymentGatewayTypeRazorpay, gw)
}

func (s *PaymentProviderResolverSuite) TestRequestedProviderNotConnected() {
	s.connect(types.SecretProviderChargebee)

	_, err := s.svc.ResolveProvider(s.GetContext(), "cust_1",
		types.IntegrationCapabilityCheckout, types.PaymentGatewayTypeRazorpay)
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *PaymentProviderResolverSuite) TestRequestedProviderLacksCapability() {
	s.connect(types.SecretProviderChargebee, types.SecretProviderStripe)

	_, err := s.svc.ResolveProvider(s.GetContext(), "cust_1",
		types.IntegrationCapabilityCheckout, types.PaymentGatewayTypeStripe)
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *PaymentProviderResolverSuite) TestNothingConfigured() {
	_, err := s.svc.ResolveProvider(s.GetContext(), "cust_1", types.IntegrationCapabilityCheckout, "")
	s.Error(err)
	s.True(ierr.IsNotFound(err))
}

// A connected gateway with no adapter must not be offered; nor must a non-payment
// connection leak into the listing.
func (s *PaymentProviderResolverSuite) TestListProvidersSkipsUnusableAndNonPayment() {
	s.connect(types.SecretProviderChargebee, types.SecretProviderHubSpot, types.SecretProviderPaddle)

	got, err := s.svc.ListProviders(s.GetContext(), "cust_1")
	s.NoError(err)
	s.Len(got, 1)
	s.Equal(types.PaymentGatewayTypeChargebee, got[0].Gateway)
	s.ElementsMatch([]types.IntegrationCapability{
		{Type: types.IntegrationCapabilityCheckout},
		{Type: types.IntegrationCapabilityPaymentLink},
		{Type: types.IntegrationCapabilityAutoCharge},
		{Type: types.IntegrationCapabilityPaymentMethodManagement},
		{Type: types.IntegrationCapabilitySetDefaultMethod},
	}, got[0].Capabilities)
}

func (s *PaymentProviderResolverSuite) TestListProvidersIsOrdered() {
	s.connect(types.SecretProviderStripe, types.SecretProviderChargebee, types.SecretProviderNomod)

	got, err := s.svc.ListProviders(s.GetContext(), "cust_1")
	s.NoError(err)
	s.Equal([]types.PaymentGatewayType{
		types.PaymentGatewayTypeChargebee,
		types.PaymentGatewayTypeNomod,
		types.PaymentGatewayTypeStripe,
	}, []types.PaymentGatewayType{got[0].Gateway, got[1].Gateway, got[2].Gateway})
}

func (s *PaymentProviderResolverSuite) TestCapabilityRequired() {
	s.connect(types.SecretProviderChargebee)

	_, err := s.svc.ResolveProvider(s.GetContext(), "cust_1", "", "")
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

// Guards gatewayCapabilities against drift in the factory switches it mirrors:
// adding an adapter without adding the capability leaves the gateway silently
// unoffered, and removing one leaves the resolver routing to nothing.
func (s *PaymentProviderResolverSuite) TestGatewayCapabilitiesMatchFactory() {
	ctx := s.GetContext()
	factory := s.GetIntegrationFactory()

	for gw := range gatewayCapabilities {
		wantSaved := lo.Contains(gatewayCapabilities[gw], types.IntegrationCapabilityPaymentMethodManagement)
		_, err := factory.GetPaymentMethodProvider(ctx, gw, nil)
		s.Equal(wantSaved, !ierr.IsNotImplemented(err),
			"payment_method_management for %s disagrees with Factory.GetPaymentMethodProvider", gw)

		wantCheckout := lo.Contains(gatewayCapabilities[gw], types.IntegrationCapabilityCheckout)
		provider, ok := checkoutProviderFor(gw)
		s.Equal(wantCheckout, ok, "checkout for %s has no CheckoutPaymentProvider mapping", gw)
		if !ok {
			continue
		}
		_, err = factory.GetCheckoutProvider(ctx, provider, nil, nil)
		s.False(ierr.IsValidation(err), "Factory.GetCheckoutProvider rejects %s", gw)
	}
}

func checkoutProviderFor(gw types.PaymentGatewayType) (types.CheckoutPaymentProvider, bool) {
	p := types.CheckoutPaymentProvider(gw)
	return p, p.Validate() == nil && p != ""
}
