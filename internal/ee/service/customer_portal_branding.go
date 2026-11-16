package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *customerPortalService) GetBranding(ctx context.Context) (*dto.PortalBrandingResponse, error) {
	customerID, err := s.portalCustomerID(ctx)
	if err != nil {
		return nil, err
	}

	cust, err := s.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		return nil, err
	}

	brand, err := s.customerIssuerBranding(ctx, cust)
	if err != nil {
		return nil, err
	}

	return &dto.PortalBrandingResponse{
		Name:    brand.Name(),
		LogoURL: publicAssetURL(s.Config, brand.LogoKey()),
		IconURL: publicAssetURL(s.Config, brand.IconKey()),
	}, nil
}

// customerIssuerBranding is the name and image keys of the issuer that bills the customer.
func (s *customerPortalService) customerIssuerBranding(ctx context.Context, cust *customer.Customer) (*IssuerBranding, error) {
	ctx = types.SetEnvironmentID(ctx, cust.EnvironmentID)

	entity, err := NewBillingEntityService(s.ServiceParams).EffectiveEntity(ctx, cust)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}
	if entity == nil {
		return tenantIssuerBranding(ctx, s.ServiceParams, cust.EnvironmentID)
	}
	return issuerBranding(ctx, s.ServiceParams, entity.Name, entity, cust.EnvironmentID)
}
