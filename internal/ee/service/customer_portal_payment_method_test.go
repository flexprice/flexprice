package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type PortalPaymentMethodSuite struct {
	testutil.BaseServiceTestSuite
	svc CustomerPortalService
	ctx context.Context
}

func TestPortalPaymentMethodSuite(t *testing.T) {
	suite.Run(t, new(PortalPaymentMethodSuite))
}

func (s *PortalPaymentMethodSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.ClearStores()

	params := ServiceParams{
		Logger:             s.GetLogger(),
		Config:             s.GetConfig(),
		DB:                 s.GetDB(),
		ConnectionRepo:     s.GetStores().ConnectionRepo,
		CustomerRepo:       s.GetStores().CustomerRepo,
		IntegrationFactory: s.GetIntegrationFactory(),
	}
	s.svc = NewCustomerPortalService(params, NewCustomerService(params), nil)
	s.ctx = types.SetCustomerID(s.GetContext(), "cust_portal")
}

func (s *PortalPaymentMethodSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *PortalPaymentMethodSuite) connect(providers ...types.SecretProvider) {
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

func (s *PortalPaymentMethodSuite) TestGetIntegrationsReportsCapabilities() {
	s.connect(types.SecretProviderChargebee, types.SecretProviderStripe)

	resp, err := s.svc.GetIntegrations(s.ctx)
	s.NoError(err)
	s.Require().Len(resp.PaymentIntegrations, 2)

	byProvider := map[types.PaymentGatewayType][]dto.IntegrationCapability{}
	for _, p := range resp.PaymentIntegrations {
		byProvider[p.Provider] = p.Capabilities
	}
	s.Contains(byProvider, types.PaymentGatewayTypeChargebee)
	s.Contains(byProvider, types.PaymentGatewayTypeStripe)

	s.ElementsMatch([]dto.IntegrationCapability{
		{Type: types.IntegrationCapabilityPaymentLink},
	}, byProvider[types.PaymentGatewayTypeStripe])
	s.ElementsMatch([]dto.IntegrationCapability{
		{Type: types.IntegrationCapabilityCheckout},
		{Type: types.IntegrationCapabilityPaymentLink},
		{Type: types.IntegrationCapabilityAutoCharge},
		{Type: types.IntegrationCapabilityPaymentMethodManagement},
		{Type: types.IntegrationCapabilitySetDefaultMethod},
	}, byProvider[types.PaymentGatewayTypeChargebee])
}

// A tenant with no payment connection gets an empty list, not an error — the
// portal still has to render.
func (s *PortalPaymentMethodSuite) TestGetIntegrationsWithNoConnections() {
	resp, err := s.svc.GetIntegrations(s.ctx)
	s.NoError(err)
	s.Empty(resp.PaymentIntegrations)
}

func (s *PortalPaymentMethodSuite) TestGetIntegrationsRequiresPortalCustomer() {
	_, err := s.svc.GetIntegrations(s.GetContext())
	s.Error(err)
	s.True(ierr.IsPermissionDenied(err))
}

// A customer never synced to the gateway has nothing saved — that is a normal
// outcome, not a provider failure, so Error stays nil and Items is empty.
func (s *PortalPaymentMethodSuite) TestListPaymentMethodsUnsyncedCustomerIsNotAnError() {
	s.connect(types.SecretProviderChargebee)

	resp, err := s.svc.ListPaymentMethods(s.ctx, &dto.ListSavedPaymentMethodsRequest{})
	s.NoError(err)
	s.Require().Len(resp.Providers, 1)

	group := resp.Providers[0]
	s.Equal(types.PaymentGatewayTypeChargebee, group.Provider)
	s.Nil(group.Error)
	s.Empty(group.Items)
	s.NotNil(group.Items, "Items must serialise as [] rather than null")
}

// The failure branch: a gateway the factory cannot build a provider for reports
// ProviderError instead of blanking the group or failing the whole response.
func (s *PortalPaymentMethodSuite) TestReadSavedMethodsReportsProviderFailure() {
	portal := s.svc.(*customerPortalService)

	group := portal.readSavedMethods(s.ctx, "cust_portal", types.PaymentGatewayTypeRazorpay)

	s.Equal(types.PaymentGatewayTypeRazorpay, group.Provider)
	s.Require().NotNil(group.Error, "an unavailable provider must report an error, not an empty list")
	s.NotEmpty(group.Error.Message)
	s.Empty(group.Items)
}

// Stripe has no payment_method_management capability yet, so it must not appear
// in the fan-out even though it is connected.
func (s *PortalPaymentMethodSuite) TestListPaymentMethodsSkipsIncapableProviders() {
	s.connect(types.SecretProviderStripe)

	resp, err := s.svc.ListPaymentMethods(s.ctx, &dto.ListSavedPaymentMethodsRequest{})
	s.NoError(err)
	s.Empty(resp.Providers)
}

func (s *PortalPaymentMethodSuite) TestListPaymentMethodsRejectsUnconnectedProvider() {
	s.connect(types.SecretProviderChargebee)

	_, err := s.svc.ListPaymentMethods(s.ctx, &dto.ListSavedPaymentMethodsRequest{
		Providers: []types.PaymentGatewayType{types.PaymentGatewayTypeRazorpay},
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *PortalPaymentMethodSuite) TestListPaymentMethodsRejectsIncapableProvider() {
	s.connect(types.SecretProviderChargebee, types.SecretProviderStripe)

	_, err := s.svc.ListPaymentMethods(s.ctx, &dto.ListSavedPaymentMethodsRequest{
		Providers: []types.PaymentGatewayType{types.PaymentGatewayTypeStripe},
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *PortalPaymentMethodSuite) TestListPaymentMethodsRequiresPortalCustomer() {
	s.connect(types.SecretProviderChargebee)

	_, err := s.svc.ListPaymentMethods(s.GetContext(), &dto.ListSavedPaymentMethodsRequest{})
	s.Error(err)
	s.True(ierr.IsPermissionDenied(err))
}

func TestToSavedPaymentMethod(t *testing.T) {
	expiry := 2030
	tests := []struct {
		name                string
		in                  interfaces.ProviderPaymentMethod
		providerAutoCharges bool
		wantStatus          types.PaymentMethodStatus
		wantAutoCharge      bool
		wantCard            bool
	}{
		{
			name: "active card at an auto-charging provider",
			in: interfaces.ProviderPaymentMethod{
				GatewayMethodID:  "pm_live",
				Method:           types.PaymentMethodTypeCard,
				Active:           true,
				IsDefault:        true,
				GatewayAccountID: "gw_acct_1",
				Card:             &interfaces.ProviderCardDetails{Brand: "visa", Last4: "4242", ExpMonth: 4, ExpYear: expiry},
			},
			providerAutoCharges: true,
			wantStatus:          types.PaymentMethodStatusActive,
			wantAutoCharge:      true,
			wantCard:            true,
		},
		{
			name: "inactive card cannot auto-charge",
			in: interfaces.ProviderPaymentMethod{
				GatewayMethodID: "pm_dead",
				Method:          types.PaymentMethodTypeCard,
				Active:          false,
				Card:            &interfaces.ProviderCardDetails{Brand: "visa", Last4: "0000"},
			},
			providerAutoCharges: true,
			wantStatus:          types.PaymentMethodStatusInactive,
			wantAutoCharge:      false,
			wantCard:            true,
		},
		{
			name: "active method at a provider that cannot auto-charge",
			in: interfaces.ProviderPaymentMethod{
				GatewayMethodID: "pm_upi",
				Method:          types.PaymentMethodTypeUPI,
				Active:          true,
			},
			providerAutoCharges: false,
			wantStatus:          types.PaymentMethodStatusActive,
			wantAutoCharge:      false,
			wantCard:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toSavedPaymentMethod(tt.in, types.PaymentGatewayTypeChargebee, tt.providerAutoCharges)

			if got.ID != tt.in.GatewayMethodID {
				t.Fatalf("ID = %q, want %q", got.ID, tt.in.GatewayMethodID)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.CanAutoCharge != tt.wantAutoCharge {
				t.Errorf("CanAutoCharge = %v, want %v", got.CanAutoCharge, tt.wantAutoCharge)
			}
			if got.IsDefault != tt.in.IsDefault {
				t.Errorf("IsDefault = %v, want %v", got.IsDefault, tt.in.IsDefault)
			}
			if (got.Card != nil) != tt.wantCard {
				t.Errorf("Card present = %v, want %v", got.Card != nil, tt.wantCard)
			}
			if tt.wantCard && got.Card.Last4 != tt.in.Card.Last4 {
				t.Errorf("Last4 = %q, want %q", got.Card.Last4, tt.in.Card.Last4)
			}
		})
	}
}

func (s *PortalPaymentMethodSuite) TestAddPaymentMethodRequiresPortalCustomer() {
	s.connect(types.SecretProviderChargebee)

	_, err := s.svc.AddPaymentMethod(s.GetContext(), &dto.PortalAddPaymentMethodRequest{
		PaymentProvider: types.PaymentGatewayTypeChargebee,
	})
	s.Error(err)
	s.True(ierr.IsPermissionDenied(err))
}

func (s *PortalPaymentMethodSuite) TestAddPaymentMethodRequiresProvider() {
	s.connect(types.SecretProviderChargebee)

	_, err := s.svc.AddPaymentMethod(s.ctx, &dto.PortalAddPaymentMethodRequest{})
	s.Error(err)
	s.True(ierr.IsValidation(err), "an unnamed provider must be refused, not guessed")
}

// Named but not connected: refused before any gateway call, so the customer is
// never sent to a link that cannot exist.
func (s *PortalPaymentMethodSuite) TestAddPaymentMethodRejectsUnconnectedProvider() {
	s.connect(types.SecretProviderStripe)

	_, err := s.svc.AddPaymentMethod(s.ctx, &dto.PortalAddPaymentMethodRequest{
		PaymentProvider: types.PaymentGatewayTypeChargebee,
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *PortalPaymentMethodSuite) TestAddPaymentMethodRejectsIncapableProvider() {
	s.connect(types.SecretProviderChargebee, types.SecretProviderStripe)

	_, err := s.svc.AddPaymentMethod(s.ctx, &dto.PortalAddPaymentMethodRequest{
		PaymentProvider: types.PaymentGatewayTypeStripe,
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *PortalPaymentMethodSuite) TestMutateRequiresPaymentMethodID() {
	s.connect(types.SecretProviderChargebee)
	delRef := &dto.PortalDeletePaymentMethodRequest{PaymentProvider: types.PaymentGatewayTypeChargebee}
	defRef := &dto.PortalSetDefaultPaymentMethodRequest{PaymentProvider: types.PaymentGatewayTypeChargebee}

	_, err := s.svc.DeletePaymentMethod(s.ctx, delRef)
	s.Error(err)
	s.True(ierr.IsValidation(err))

	_, err = s.svc.SetDefaultPaymentMethod(s.ctx, defRef)
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

// A bare method id is only unique within a provider, so the pair is the address —
// an unnamed provider must never be inferred.
func (s *PortalPaymentMethodSuite) TestMutateRequiresProvider() {
	s.connect(types.SecretProviderChargebee)
	delRef := &dto.PortalDeletePaymentMethodRequest{PaymentMethodID: "pm_x"}
	defRef := &dto.PortalSetDefaultPaymentMethodRequest{PaymentMethodID: "pm_x"}

	_, err := s.svc.DeletePaymentMethod(s.ctx, delRef)
	s.Error(err)
	s.True(ierr.IsValidation(err))

	_, err = s.svc.SetDefaultPaymentMethod(s.ctx, defRef)
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *PortalPaymentMethodSuite) TestMutateRequiresPortalCustomer() {
	s.connect(types.SecretProviderChargebee)

	_, err := s.svc.DeletePaymentMethod(s.GetContext(), &dto.PortalDeletePaymentMethodRequest{
		PaymentProvider: types.PaymentGatewayTypeChargebee,
		PaymentMethodID: "pm_x",
	})
	s.Error(err)
	s.True(ierr.IsPermissionDenied(err))
}

func (s *PortalPaymentMethodSuite) TestMutateRejectsIncapableProvider() {
	s.connect(types.SecretProviderChargebee, types.SecretProviderStripe)

	_, err := s.svc.SetDefaultPaymentMethod(s.ctx, &dto.PortalSetDefaultPaymentMethodRequest{
		PaymentProvider: types.PaymentGatewayTypeStripe,
		PaymentMethodID: "pm_x",
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}
