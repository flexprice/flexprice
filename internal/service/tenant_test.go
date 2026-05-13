package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type TenantServiceSuite struct {
	testutil.BaseServiceTestSuite
	tenantService TenantService // Pointer to the concrete implementation
	tenantRepo    *testutil.InMemoryTenantStore
}

func TestTenantService(t *testing.T) {
	suite.Run(t, new(TenantServiceSuite))
}

func (s *TenantServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *TenantServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.tenantRepo.Clear()
}

func (s *TenantServiceSuite) setupService() {
	s.tenantRepo = s.GetStores().TenantRepo.(*testutil.InMemoryTenantStore)

	s.tenantService = NewTenantService(ServiceParams{
		Logger:           s.GetLogger(),
		Config:           s.GetConfig(),
		DB:               s.GetDB(),
		SubRepo:          s.GetStores().SubscriptionRepo,
		PlanRepo:         s.GetStores().PlanRepo,
		PriceRepo:        s.GetStores().PriceRepo,
		EventRepo:        s.GetStores().EventRepo,
		MeterRepo:        s.GetStores().MeterRepo,
		CustomerRepo:     s.GetStores().CustomerRepo,
		InvoiceRepo:      s.GetStores().InvoiceRepo,
		EntitlementRepo:  s.GetStores().EntitlementRepo,
		EnvironmentRepo:  s.GetStores().EnvironmentRepo,
		FeatureRepo:      s.GetStores().FeatureRepo,
		TenantRepo:       s.GetStores().TenantRepo,
		UserRepo:         s.GetStores().UserRepo,
		AuthRepo:         s.GetStores().AuthRepo,
		WalletRepo:       s.GetStores().WalletRepo,
		PaymentRepo:      s.GetStores().PaymentRepo,
		EventPublisher:   s.GetPublisher(),
		WebhookPublisher: s.GetWebhookPublisher(),
	})
}

const (
	accessTestTargetTenantID   = "tenant-target"
	accessTestOperatorTenantID = "tenant-operator"
)

func (s *TenantServiceSuite) setupTestData() {
	_ = s.tenantRepo.Create(s.GetContext(), &tenant.Tenant{
		ID:             accessTestTargetTenantID,
		Name:           "Target Tenant",
		InternalStatus: types.TenantInternalStatusTrialing,
	})
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
			resp, err := s.tenantService.CreateTenant(s.GetContext(), tc.request)

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
	_ = s.tenantRepo.Create(s.GetContext(), &tenant.Tenant{
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
			resp, err := s.tenantService.GetTenantByID(s.GetContext(), tc.id)

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

func (s *TenantServiceSuite) TestGetTenantWithBillingDetails() {
	// Create tenant with billing details
	tenantWithBilling := &tenant.Tenant{
		ID:   "tenant-with-billing",
		Name: "Billing Tenant",
		BillingDetails: tenant.TenantBillingDetails{
			Email:     "billing@example.com",
			HelpEmail: "help@example.com",
			Phone:     "+1-555-987-6543",
			Address: tenant.TenantAddress{
				Line1:      "456 Market Street",
				Line2:      "Floor 3",
				City:       "San Francisco",
				State:      "CA",
				PostalCode: "94105",
				Country:    "US",
			},
		},
	}
	err := s.tenantRepo.Create(s.GetContext(), tenantWithBilling)
	s.NoError(err)

	// Create tenant without billing details
	tenantWithoutBilling := &tenant.Tenant{
		ID:   "tenant-without-billing",
		Name: "No Billing Tenant",
		BillingDetails: tenant.TenantBillingDetails{
			Email:     "",
			HelpEmail: "",
			Phone:     "",
			Address: tenant.TenantAddress{
				Line1:      "",
				Line2:      "",
				City:       "",
				State:      "",
				PostalCode: "",
				Country:    "",
			},
		},
	}
	err = s.tenantRepo.Create(s.GetContext(), tenantWithoutBilling)
	s.NoError(err)

	testCases := []struct {
		name                string
		id                  string
		expectedError       bool
		expectedName        string
		expectedBillingInfo bool
		expectedEmail       string
		expectedPhone       string
		expectedCountry     string
	}{
		{
			name:                "tenant_with_billing_details",
			id:                  "tenant-with-billing",
			expectedError:       false,
			expectedName:        "Billing Tenant",
			expectedBillingInfo: true,
			expectedEmail:       "billing@example.com",
			expectedPhone:       "+1-555-987-6543",
			expectedCountry:     "US",
		},
		{
			name:                "tenant_without_billing_details",
			id:                  "tenant-without-billing",
			expectedError:       false,
			expectedName:        "No Billing Tenant",
			expectedBillingInfo: true,
			expectedEmail:       "",
			expectedPhone:       "",
			expectedCountry:     "",
		},
		{
			name:          "tenant_not_found",
			id:            "nonexistent-tenant",
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Call the service method
			resp, err := s.tenantService.GetTenantByID(s.GetContext(), tc.id)

			// Assert results
			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
			} else {
				s.NoError(err)
				s.NotNil(resp)
				s.Equal(tc.expectedName, resp.Name)

				if tc.expectedBillingInfo {
					s.Equal(tc.expectedEmail, resp.BillingDetails.Email)
					s.Equal(tc.expectedPhone, resp.BillingDetails.Phone)
					s.Equal(tc.expectedCountry, resp.BillingDetails.Address.Country)
				}
			}
		})
	}
}

func (s *TenantServiceSuite) TestUpdateTenantAccess() {
	operatorCtx := context.WithValue(s.GetContext(), types.CtxTenantID, accessTestOperatorTenantID)

	testCases := []struct {
		name           string
		ctx            context.Context
		targetID       string
		initialStatus  types.TenantInternalStatus
		request        dto.UpdateTenantAccessRequest
		expectedError  bool
		expectedStatus types.TenantInternalStatus
	}{
		{
			name:           "operator_promotes_trialing_to_active",
			ctx:            operatorCtx,
			targetID:       accessTestTargetTenantID,
			initialStatus:  types.TenantInternalStatusTrialing,
			request:        dto.UpdateTenantAccessRequest{InternalStatus: types.TenantInternalStatusActive},
			expectedStatus: types.TenantInternalStatusActive,
		},
		{
			name:           "operator_suspends_active_tenant",
			ctx:            operatorCtx,
			targetID:       accessTestTargetTenantID,
			initialStatus:  types.TenantInternalStatusActive,
			request:        dto.UpdateTenantAccessRequest{InternalStatus: types.TenantInternalStatusSuspended},
			expectedStatus: types.TenantInternalStatusSuspended,
		},
		{
			name:           "operator_reinstates_suspended_tenant",
			ctx:            operatorCtx,
			targetID:       accessTestTargetTenantID,
			initialStatus:  types.TenantInternalStatusSuspended,
			request:        dto.UpdateTenantAccessRequest{InternalStatus: types.TenantInternalStatusActive},
			expectedStatus: types.TenantInternalStatusActive,
		},
		{
			name:          "tenant_cannot_update_own_status",
			ctx:           context.WithValue(s.GetContext(), types.CtxTenantID, accessTestTargetTenantID),
			targetID:      accessTestTargetTenantID,
			initialStatus: types.TenantInternalStatusTrialing,
			request:       dto.UpdateTenantAccessRequest{InternalStatus: types.TenantInternalStatusActive},
			expectedError: true,
		},
		{
			name:          "same_status_rejected",
			ctx:           operatorCtx,
			targetID:      accessTestTargetTenantID,
			initialStatus: types.TenantInternalStatusActive,
			request:       dto.UpdateTenantAccessRequest{InternalStatus: types.TenantInternalStatusActive},
			expectedError: true,
		},
		{
			name:          "invalid_status_rejected",
			ctx:           operatorCtx,
			targetID:      accessTestTargetTenantID,
			initialStatus: types.TenantInternalStatusTrialing,
			request:       dto.UpdateTenantAccessRequest{InternalStatus: "unknown"},
			expectedError: true,
		},
		{
			name:          "target_tenant_not_found",
			ctx:           operatorCtx,
			targetID:      "nonexistent-tenant",
			initialStatus: types.TenantInternalStatusTrialing,
			request:       dto.UpdateTenantAccessRequest{InternalStatus: types.TenantInternalStatusActive},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			_ = s.tenantRepo.Update(s.GetContext(), &tenant.Tenant{
				ID:             accessTestTargetTenantID,
				Name:           "Target Tenant",
				InternalStatus: tc.initialStatus,
			})

			resp, err := s.tenantService.UpdateTenantAccess(tc.ctx, tc.targetID, tc.request)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
			} else {
				s.NoError(err)
				s.NotNil(resp)
				updated, err := s.tenantRepo.GetByID(s.GetContext(), tc.targetID)
				s.NoError(err)
				s.Equal(tc.expectedStatus, updated.InternalStatus)
			}
		})
	}
}
