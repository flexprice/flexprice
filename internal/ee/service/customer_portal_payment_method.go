package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *customerPortalService) ListPaymentMethods(ctx context.Context, req *dto.ListSavedPaymentMethodsRequest) (*dto.SavedPaymentMethodsResponse, error) {
	customerID, err := s.portalCustomerID(ctx)
	if err != nil {
		return nil, err
	}

	return s.paymentService.ListPaymentMethods(ctx, customerID, req)
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
		types.IntegrationCapabilityAddPaymentMethod, req.PaymentProvider)
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

	return s.mutateSavedMethod(ctx, types.IntegrationCapabilityDeletePaymentMethod,
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

	resp, err := s.paymentService.ListPaymentMethods(ctx, customerID, &dto.ListSavedPaymentMethodsRequest{
		Providers: []types.PaymentGatewayType{resolvedGateway},
	})
	if err != nil {
		s.Logger.Error(ctx, "failed to re-read saved payment methods after write",
			"error", err, "customer_id", customerID, "provider", resolvedGateway)
		return &dto.SavedPaymentMethodsResponse{
			Providers: []*dto.ProviderSavedPaymentMethods{{
				Provider: resolvedGateway,
				Items:    []*dto.SavedPaymentMethod{},
				Error:    &dto.ProviderError{Message: "Could not read updated saved payment methods from this provider"},
			}},
		}, nil
	}

	return resp, nil
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
