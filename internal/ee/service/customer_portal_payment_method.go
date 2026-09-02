package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *customerPortalService) ListPaymentMethods(ctx context.Context, req *dto.ListSavedPaymentMethodsRequest) (*dto.SavedPaymentMethodsResponse, error) {
	customerID, err := s.portalCustomerID(ctx)
	if err != nil {
		return nil, err
	}

	var requested []types.PaymentGatewayType
	if req != nil {
		requested = req.Providers
	}
	gateways, err := s.methodManagementProviders(ctx, customerID, requested)
	if err != nil {
		return nil, err
	}

	resp := &dto.SavedPaymentMethodsResponse{
		Providers: make([]*dto.ProviderSavedPaymentMethods, 0, len(gateways)),
	}
	for _, gw := range gateways {
		resp.Providers = append(resp.Providers, s.readSavedMethods(ctx, customerID, gw))
	}
	return resp, nil
}

func (s *customerPortalService) AddPaymentMethod(ctx context.Context, req *dto.PortalAddPaymentMethodRequest) (*dto.AddPaymentMethodResponse, error) {
	if req == nil {
		return nil, ierr.NewError("request is required").Mark(ierr.ErrValidation)
	}

	customerID, err := s.portalCustomerID(ctx)
	if err != nil {
		return nil, err
	}

	provider, gw, err := s.methodProviderFor(ctx, customerID,
		types.IntegrationCapabilityPaymentMethodManagement, req.PaymentProvider)
	if err != nil {
		return nil, err
	}

	returnURL := ""
	if req.SuccessURL != nil {
		returnURL = *req.SuccessURL
	}

	link, err := provider.CreateSetupLink(ctx, interfaces.SetupLinkRequest{
		CustomerID: customerID,
		ReturnURL:  returnURL,
	})
	if err != nil {
		return nil, err
	}

	return &dto.AddPaymentMethodResponse{
		Provider: gw,
		Action: dto.SetupAction{
			Type:      dto.SetupActionRedirect,
			URL:       link.URL,
			ExpiresAt: link.ExpiresAt,
		},
	}, nil
}

func (s *customerPortalService) DeletePaymentMethod(ctx context.Context, req *dto.PortalDeletePaymentMethodRequest) (*dto.SavedPaymentMethodsResponse, error) {
	if req == nil {
		return nil, ierr.NewError("request is required").Mark(ierr.ErrValidation)
	}

	return s.mutateSavedMethod(ctx, types.IntegrationCapabilityPaymentMethodManagement,
		req.PaymentProvider, req.PaymentMethodID,
		func(ctx context.Context, provider interfaces.PaymentMethodProvider, customerID, methodID string) error {
			return provider.DeleteSavedMethod(ctx, customerID, methodID)
		})
}

func (s *customerPortalService) SetDefaultPaymentMethod(ctx context.Context, req *dto.PortalSetDefaultPaymentMethodRequest) (*dto.SavedPaymentMethodsResponse, error) {
	if req == nil {
		return nil, ierr.NewError("request is required").Mark(ierr.ErrValidation)
	}

	return s.mutateSavedMethod(ctx, types.IntegrationCapabilitySetDefaultMethod,
		req.PaymentProvider, req.PaymentMethodID,
		func(ctx context.Context, provider interfaces.PaymentMethodProvider, customerID, methodID string) error {
			return provider.SetDefaultSavedMethod(ctx, customerID, methodID)
		})
}

// mutateSavedMethod writes to the named gateway, then re-reads that gateway only —
// the others cannot have changed. Once the write succeeds the caller gets a
// response whatever the re-read does: a failed listing surfaces as ProviderError,
// never as an error implying the write did not happen.
func (s *customerPortalService) mutateSavedMethod(
	ctx context.Context,
	capability types.IntegrationCapabilityType,
	gateway types.PaymentGatewayType,
	paymentMethodID string,
	write func(ctx context.Context, provider interfaces.PaymentMethodProvider, customerID, methodID string) error,
) (*dto.SavedPaymentMethodsResponse, error) {
	if paymentMethodID == "" {
		return nil, ierr.NewError("payment_method_id is required").
			WithHint("Specify which saved payment method to act on").
			Mark(ierr.ErrValidation)
	}

	customerID, err := s.portalCustomerID(ctx)
	if err != nil {
		return nil, err
	}

	provider, resolvedGateway, err := s.methodProviderFor(ctx, customerID, capability, gateway)
	if err != nil {
		return nil, err
	}

	if err := write(ctx, provider, customerID, paymentMethodID); err != nil {
		return nil, err
	}

	return &dto.SavedPaymentMethodsResponse{
		Providers: []*dto.ProviderSavedPaymentMethods{
			s.readSavedMethods(ctx, customerID, resolvedGateway),
		},
	}, nil
}

// methodProviderFor validates a caller-named gateway and builds its adapter.
func (s *customerPortalService) methodProviderFor(
	ctx context.Context,
	customerID string,
	capability types.IntegrationCapabilityType,
	gateway types.PaymentGatewayType,
) (interfaces.PaymentMethodProvider, types.PaymentGatewayType, error) {
	if gateway == "" {
		return nil, "", ierr.NewError("payment_provider is required").
			WithHint("Specify which payment provider to use").
			Mark(ierr.ErrValidation)
	}

	resolvedGateway, err := NewPaymentProviderResolver(s.ServiceParams).
		ResolveProvider(ctx, customerID, capability, gateway)
	if err != nil {
		return nil, "", err
	}

	provider, err := s.IntegrationFactory.GetPaymentMethodProvider(ctx, resolvedGateway, s.customerService)
	if err != nil {
		return nil, "", err
	}

	return provider, resolvedGateway, nil
}

// methodManagementProviders returns the gateways to fan out over. A named provider
// is validated through the resolver so "not connected" and "cannot manage methods"
// stay distinguishable; naming none means every capable provider.
func (s *customerPortalService) methodManagementProviders(
	ctx context.Context,
	customerID string,
	requested []types.PaymentGatewayType,
) ([]types.PaymentGatewayType, error) {
	resolver := NewPaymentProviderResolver(s.ServiceParams)

	if len(requested) > 0 {
		out := make([]types.PaymentGatewayType, 0, len(requested))
		for _, gw := range lo.Uniq(requested) {
			resolved, err := resolver.ResolveProvider(ctx, customerID, types.IntegrationCapabilityPaymentMethodManagement, gw)
			if err != nil {
				return nil, err
			}
			out = append(out, resolved)
		}
		return out, nil
	}

	providers, err := resolver.ListProviders(ctx, customerID)
	if err != nil {
		return nil, err
	}
	capable := lo.Filter(providers, func(p interfaces.ProviderCapabilities, _ int) bool {
		return lo.ContainsBy(p.Capabilities, func(c types.IntegrationCapability) bool {
			return c.Type == types.IntegrationCapabilityPaymentMethodManagement
		})
	})
	return lo.Map(capable, func(p interfaces.ProviderCapabilities, _ int) types.PaymentGatewayType {
		return p.Gateway
	}), nil
}

// readSavedMethods never returns an error: one unreachable gateway must not blank
// out the others, and an empty Items with no Error means "nothing saved" — a
// distinction the UI needs and a bare empty list cannot make.
func (s *customerPortalService) readSavedMethods(
	ctx context.Context,
	customerID string,
	gw types.PaymentGatewayType,
) *dto.ProviderSavedPaymentMethods {
	group := &dto.ProviderSavedPaymentMethods{
		Provider: gw,
		Items:    []*dto.SavedPaymentMethod{},
	}

	provider, err := s.IntegrationFactory.GetPaymentMethodProvider(ctx, gw, s.customerService)
	if err != nil {
		s.Logger.Error(ctx, "payment method provider unavailable",
			"error", err, "customer_id", customerID, "provider", gw)
		group.Error = &dto.ProviderError{Message: "This payment provider is currently unavailable"}
		return group
	}

	methods, err := provider.ListSavedMethods(ctx, customerID)
	if err != nil {
		s.Logger.Error(ctx, "failed to list saved payment methods",
			"error", err, "customer_id", customerID, "provider", gw)
		group.Error = &dto.ProviderError{Message: "Could not read saved payment methods from this provider"}
		return group
	}

	autoCharge := lo.Contains(gatewayCapabilities[gw], types.IntegrationCapabilityAutoCharge)
	for _, m := range methods {
		group.Items = append(group.Items, toSavedPaymentMethod(m, gw, autoCharge))
	}
	return group
}

func toSavedPaymentMethod(
	m interfaces.ProviderPaymentMethod,
	gw types.PaymentGatewayType,
	providerAutoCharges bool,
) *dto.SavedPaymentMethod {
	status := types.PaymentMethodStatusInactive
	if m.Active {
		status = types.PaymentMethodStatusActive
	}

	// GatewayAccountID is deliberately not mapped — it is a routing detail.
	out := &dto.SavedPaymentMethod{
		ID:            m.GatewayMethodID,
		Provider:      gw,
		Type:          m.Method,
		Status:        status,
		IsDefault:     m.IsDefault,
		CanAutoCharge: m.Active && providerAutoCharges,
	}
	if m.Card != nil {
		out.Card = &dto.SavedCardDetails{
			Brand:    m.Card.Brand,
			Last4:    m.Card.Last4,
			ExpMonth: m.Card.ExpMonth,
			ExpYear:  m.Card.ExpYear,
		}
	}
	return out
}
