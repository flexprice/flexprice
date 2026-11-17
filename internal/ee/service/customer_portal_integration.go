package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *customerPortalService) GetIntegrations(ctx context.Context) (*dto.IntegrationsResponse, error) {
	customerID, err := s.portalCustomerID(ctx)
	if err != nil {
		return nil, err
	}

	return listPaymentIntegrations(ctx, s.ServiceParams, customerID)
}

// listPaymentIntegrations is the customer's usable gateways with what each can do.
func listPaymentIntegrations(ctx context.Context, params ServiceParams, customerID string) (*dto.IntegrationsResponse, error) {
	providers, err := NewPaymentProviderResolver(params).ListProviders(ctx, customerID)
	if err != nil {
		return nil, err
	}

	resp := &dto.IntegrationsResponse{
		PaymentIntegrations: make([]*dto.PaymentIntegration, 0, len(providers)),
	}
	for _, p := range providers {
		resp.PaymentIntegrations = append(resp.PaymentIntegrations, &dto.PaymentIntegration{
			Provider:     p.Gateway,
			Capabilities: p.Capabilities,
		})
	}

	return resp, nil
}

func (s *customerPortalService) portalCustomerID(ctx context.Context) (string, error) {
	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return "", ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	return customerID, nil
}
