package service

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *PortalWalletSuite) TestUpdateCustomerOnlyAffectsSessionCustomer() {
	s.NoError(s.GetStores().CustomerRepo.Create(s.ctx, &customer.Customer{
		ID:         "cust_other",
		ExternalID: "ext_other",
		Name:       "Other",
		Email:      "other@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.ctx),
	}))

	resp, err := s.svc.UpdateCustomer(s.ctx, dto.PortalUpdateCustomerRequest{Name: lo.ToPtr("Renamed")})
	s.NoError(err)
	s.Equal("cust_portal", resp.ID)
	s.Equal("Renamed", resp.Name)
	s.Equal("ext_cust_portal", resp.ExternalID)

	other, err := s.GetStores().CustomerRepo.Get(s.ctx, "cust_other")
	s.NoError(err)
	s.Equal("Other", other.Name)
}

func (s *PortalWalletSuite) TestUpdateCustomerRequiresSessionCustomer() {
	ctx := types.SetCustomerID(s.ctx, "")
	_, err := s.svc.UpdateCustomer(ctx, dto.PortalUpdateCustomerRequest{Name: lo.ToPtr("x")})
	s.Error(err)
}
