package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
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

	brand, err := NewBrandingService(s.ServiceParams).ForCustomer(ctx, cust)
	if err != nil {
		return nil, err
	}

	return &dto.PortalBrandingResponse{
		Name:    brand.Name(),
		LogoURL: brandingImageURL(s.Config, brand.LogoKey()),
		IconURL: brandingImageURL(s.Config, brand.IconKey()),
	}, nil
}
