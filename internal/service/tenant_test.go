package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/stretchr/testify/suite"
)

type TenantServiceSuite struct {
	suite.Suite
	ctx           context.Context
	tenantService *tenantService // Pointer to the concrete implementation
	tenantRepo    *testutil.InMemoryTenantStore
}

func TestTenantService(t *testing.T) {
	suite.Run(t, new(TenantServiceSuite))
}

func (s *TenantServiceSuite) SetupTest() {
	// Initialize context and repository
	s.ctx = testutil.SetupContext()
	s.tenantRepo = testutil.NewInMemoryTenantStore()

	// Create the tenantService with the repository
	s.tenantService = &tenantService{repo: s.tenantRepo} // Directly assign the concrete struct
}

func (s *TenantServiceSuite) TestCreateTenant() {
	testCases := []struct {
		name          string
		request       dto.CreateTenantRequest
		expectedError bool
		expectedName  string
	}{
		{
			name: "valid_tenant",
			request: dto.CreateTenantRequest{
				Name: "New Tenant",
			},
			expectedError: false,
			expectedName:  "New Tenant",
		},
		{
			name: "invalid_tenant",
			request: dto.CreateTenantRequest{
				Name: "",
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Call the service method
			resp, err := s.tenantService.CreateTenant(s.ctx, tc.request)

			// Assert results
			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
			} else {
				s.NoError(err)
				s.NotNil(resp)
				s.Equal(tc.expectedName, resp.Name)
			}
		})
	}
}

func (s *TenantServiceSuite) TestGetTenantByID() {
	_ = s.tenantRepo.Create(s.ctx, &tenant.Tenant{
		ID:   "tenant-1",
		Name: "Test Tenant",
	})

	testCases := []struct {
		name          string
		id            string
		expectedError bool
		expectedName  string
	}{
		{
			name:          "tenant_found",
			id:            "tenant-1",
			expectedError: false,
			expectedName:  "Test Tenant",
		},
		{
			name:          "tenant_not_found",
			id:            "nonexistent-id",
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Call the service method
			resp, err := s.tenantService.GetTenantByID(s.ctx, tc.id)

			// Assert results
			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
			} else {
				s.NoError(err)
				s.NotNil(resp)
				s.Equal(tc.expectedName, resp.Name)
			}
		})
	}
}
