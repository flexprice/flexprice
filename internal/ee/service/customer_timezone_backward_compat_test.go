package service

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// Backward-compatibility for customer creation/update with the new timezone field:
//   - existing callers that omit timezone must get the UTC default (no behaviour change)
//   - a valid IANA timezone is stored
//   - an invalid timezone is rejected at the DTO boundary
//   - updates that omit timezone must preserve the stored value

func (s *CustomerServiceSuite) TestCreateCustomer_TimezoneBackwardCompat() {
	s.Run("omitted timezone defaults to UTC", func() {
		resp, err := s.service.CreateCustomer(s.ctx, dto.CreateCustomerRequest{
			ExternalID: "tz-bc-default",
			Name:       "No TZ Customer",
		})
		s.NoError(err)
		s.NotNil(resp)
		s.Equal("UTC", resp.Customer.Timezone, "omitted timezone must default to UTC for backward compatibility")
	})

	s.Run("valid IANA timezone is stored", func() {
		resp, err := s.service.CreateCustomer(s.ctx, dto.CreateCustomerRequest{
			ExternalID: "tz-bc-ist",
			Name:       "IST Customer",
			Timezone:   "Asia/Kolkata",
		})
		s.NoError(err)
		s.NotNil(resp)
		s.Equal("Asia/Kolkata", resp.Customer.Timezone)
	})

	s.Run("invalid timezone is rejected", func() {
		resp, err := s.service.CreateCustomer(s.ctx, dto.CreateCustomerRequest{
			ExternalID: "tz-bc-invalid",
			Name:       "Bad TZ Customer",
			Timezone:   "Not/AZone",
		})
		s.Error(err)
		s.Nil(resp)
		s.True(ierr.IsValidation(err), "invalid timezone must be a validation error")
	})
}

func (s *CustomerServiceSuite) TestUpdateCustomer_TimezoneBackwardCompat() {
	created, err := s.service.CreateCustomer(s.ctx, dto.CreateCustomerRequest{
		ExternalID: "tz-bc-update",
		Name:       "Update TZ Customer",
		Timezone:   "America/New_York",
	})
	s.NoError(err)
	s.Require().NotNil(created)

	s.Run("update omitting timezone preserves stored value", func() {
		resp, err := s.service.UpdateCustomer(s.ctx, created.Customer.ID, dto.UpdateCustomerRequest{
			Name: lo.ToPtr("Renamed, Same TZ"),
		})
		s.NoError(err)
		s.Equal("America/New_York", resp.Customer.Timezone, "omitting timezone on update must not change it")
	})

	s.Run("update with new valid timezone changes it", func() {
		resp, err := s.service.UpdateCustomer(s.ctx, created.Customer.ID, dto.UpdateCustomerRequest{
			Timezone: lo.ToPtr("Asia/Kolkata"),
		})
		s.NoError(err)
		s.Equal("Asia/Kolkata", resp.Customer.Timezone)
	})
}
