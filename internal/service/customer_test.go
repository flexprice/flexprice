// Test code for the customer service
package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type CustomerServiceSuite struct {
	suite.Suite
	ctx             context.Context
	customerService *customerService
	repo            *testutil.InMemoryCustomerStore
}

func TestCustomerService(t *testing.T) {
	suite.Run(t, new(CustomerServiceSuite))
}

func (s *CustomerServiceSuite) SetupTest() {
	s.ctx = context.Background()
	s.repo = testutil.NewInMemoryCustomerStore()
	s.customerService = &customerService{
		repo: s.repo,
	}
}

func (s *CustomerServiceSuite) TestCreateCustomer() {
	testCases := []struct {
		name          string
		setup         func()
		expectedError bool
	}{
		{
			name: "successful_creation",
			setup: func() {
				// Ensure the repository is empty before test
				s.repo = testutil.NewInMemoryCustomerStore()
				s.customerService = &customerService{repo: s.repo}
			},
			expectedError: false,
		},
		{
			name: "duplicate_creation",
			setup: func() {
				// Add a customer with the same ID
				_ = s.repo.Create(s.ctx, &customer.Customer{
					ID:    "cust-1",
					Name:  "Test Customer",
					Email: "test@example.com",
				})
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			req := &customer.Customer{
				ID:    "cust-1",
				Name:  "Test Customer",
				Email: "test@example.com",
			}

			err := s.repo.Create(s.ctx, req)

			if tc.expectedError {
				s.Error(err)
			} else {
				s.NoError(err)
			}
		})
	}
}

func (s *CustomerServiceSuite) TestGetCustomer() {
	_ = s.repo.Create(s.ctx, &customer.Customer{
		ID:    "cust-1",
		Name:  "Test Customer",
		Email: "test@example.com",
	})

	testCases := []struct {
		name          string
		id            string
		expectedError bool
		expectedName  string
	}{
		{
			name:          "customer_found",
			id:            "cust-1",
			expectedError: false,
			expectedName:  "Test Customer",
		},
		{
			name:          "customer_not_found",
			id:            "nonexistent-id",
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.repo.Get(s.ctx, tc.id)

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

func (s *CustomerServiceSuite) TestGetCustomers() {
	// Reset and prepopulate the repository with customers
	s.repo = testutil.NewInMemoryCustomerStore()
	s.customerService = &customerService{repo: s.repo}
	_ = s.repo.Create(s.ctx, &customer.Customer{
		ID:    "cust-1",
		Name:  "Customer One",
		Email: "one@example.com",
	})
	_ = s.repo.Create(s.ctx, &customer.Customer{
		ID:    "cust-2",
		Name:  "Customer Two",
		Email: "two@example.com",
	})

	testCases := []struct {
		name          string
		filter        *types.CustomerFilter
		expectedError bool
		expectedCount int
	}{
		{
			name: "all_customers",
			filter: &types.CustomerFilter{
				QueryFilter: &types.QueryFilter{
					Limit:  lo.ToPtr(int(10)),
					Offset: lo.ToPtr(int(0)),
				},
			},
			expectedError: false,
			expectedCount: 2,
		},
		{
			name: "no_customers",
			filter: &types.CustomerFilter{
				QueryFilter: &types.QueryFilter{
					Limit:  lo.ToPtr(int(10)),
					Offset: lo.ToPtr(int(10)),
				},
			},
			expectedError: false,
			expectedCount: 0,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.customerService.GetCustomers(s.ctx, tc.filter)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
			} else {
				s.NoError(err)
				s.NotNil(resp)
				s.Equal(tc.expectedCount, len(resp.Items))
			}
		})
	}
}

func (s *CustomerServiceSuite) TestUpdateCustomer() {
	// Prepopulate the repository with a customer
	_ = s.repo.Create(s.ctx, &customer.Customer{
		ID:    "cust-1",
		Name:  "Old Name",
		Email: "old@example.com",
	})

	testCases := []struct {
		name          string
		id            string
		req           dto.UpdateCustomerRequest
		expectedError bool
		expectedName  string
	}{
		{
			name: "valid_update",
			id:   "cust-1",
			req: dto.UpdateCustomerRequest{
				Name:  lo.ToPtr("New Name"),
				Email: lo.ToPtr("new@example.com"),
			},
			expectedError: false,
			expectedName:  "New Name",
		},
		{
			name: "customer_not_found",
			id:   "nonexistent-id",
			req: dto.UpdateCustomerRequest{
				Name: lo.ToPtr("Should Not Work"),
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.customerService.UpdateCustomer(s.ctx, tc.id, tc.req)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
			} else {
				s.NoError(err)
				s.NotNil(resp)
				s.Equal(tc.expectedName, resp.Customer.Name)
			}
		})
	}
}

func (s *CustomerServiceSuite) TestDeleteCustomer() {
	// Prepopulate the repository with a customer
	_ = s.repo.Create(s.ctx, &customer.Customer{
		ID:    "cust-1",
		Name:  "To Be Deleted",
		Email: "delete@example.com",
	})

	testCases := []struct {
		name          string
		id            string
		expectedError bool
	}{
		{
			name:          "delete_existing_customer",
			id:            "cust-1",
			expectedError: false,
		},
		{
			name:          "delete_nonexistent_customer",
			id:            "nonexistent-id",
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			err := s.customerService.DeleteCustomer(s.ctx, tc.id)

			if tc.expectedError {
				s.Error(err)
			} else {
				s.NoError(err)

				// Ensure the customer no longer exists
				_, err := s.repo.Get(s.ctx, tc.id)
				s.Error(err)
			}
		})
	}
}
