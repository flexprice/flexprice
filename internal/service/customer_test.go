// Test code for the customer service
package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/errors"
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
		request       dto.CreateCustomerRequest
		expectedError bool
		errorCode     string
	}{
		{
			name: "successful_creation",
			setup: func() {
				// Ensure the repository is empty before test
				s.repo = testutil.NewInMemoryCustomerStore()
				s.customerService = &customerService{repo: s.repo}
			},
			request: dto.CreateCustomerRequest{
				ExternalID:        "ext-1",
				Name:              "Test Customer",
				Email:             "test@example.com",
				AddressLine1:      "123 Main St",
				AddressLine2:      "Apt 4B",
				AddressCity:       "New York",
				AddressState:      "NY",
				AddressPostalCode: "10001",
				AddressCountry:    "US",
				Metadata: map[string]string{
					"source": "web",
					"type":   "business",
				},
			},
			expectedError: false,
		},
		{
			name: "invalid_country_code",
			request: dto.CreateCustomerRequest{
				ExternalID:     "ext-2",
				Name:           "Test Customer",
				AddressCountry: "USA", // Invalid: should be 2 characters
			},
			expectedError: true,
			errorCode:     errors.ErrCodeValidation,
		},
		{
			name: "invalid_postal_code",
			request: dto.CreateCustomerRequest{
				ExternalID:        "ext-3",
				Name:              "Test Customer",
				AddressPostalCode: "12345678901234567890123", // Too long
			},
			expectedError: true,
			errorCode:     errors.ErrCodeValidation,
		},
		{
			name: "invalid_address_line1",
			request: dto.CreateCustomerRequest{
				ExternalID:   "ext-4",
				Name:         "Test Customer",
				AddressLine1: string(make([]byte, 256)), // Too long
			},
			expectedError: true,
			errorCode:     errors.ErrCodeValidation,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			resp, err := s.customerService.CreateCustomer(s.ctx, tc.request)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
				if tc.errorCode != "" {
					var ierr *errors.InternalError
					s.True(stderrors.As(err, &ierr))
					s.Equal(tc.errorCode, ierr.Code)
				}
			} else {
				s.NoError(err)
				s.NotNil(resp)
				// Verify the customer was created with correct fields
				s.Equal(tc.request.Name, resp.Customer.Name)
				s.Equal(tc.request.Email, resp.Customer.Email)
				s.Equal(tc.request.AddressLine1, resp.Customer.AddressLine1)
				s.Equal(tc.request.AddressLine2, resp.Customer.AddressLine2)
				s.Equal(tc.request.AddressCity, resp.Customer.AddressCity)
				s.Equal(tc.request.AddressState, resp.Customer.AddressState)
				s.Equal(tc.request.AddressPostalCode, resp.Customer.AddressPostalCode)
				s.Equal(tc.request.AddressCountry, resp.Customer.AddressCountry)
				s.Equal(tc.request.Metadata, resp.Customer.Metadata)
			}
		})
	}
}

func (s *CustomerServiceSuite) TestGetCustomer() {
	customer := &customer.Customer{
		ID:                "cust-1",
		Name:              "Test Customer",
		Email:             "test@example.com",
		AddressLine1:      "123 Main St",
		AddressLine2:      "Apt 4B",
		AddressCity:       "New York",
		AddressState:      "NY",
		AddressPostalCode: "10001",
		AddressCountry:    "US",
		Metadata: map[string]string{
			"source": "web",
		},
	}
	_ = s.repo.Create(s.ctx, customer)

	testCases := []struct {
		name          string
		id            string
		expectedError bool
		errorCode     string
	}{
		{
			name:          "customer_found",
			id:            "cust-1",
			expectedError: false,
		},
		{
			name:          "customer_not_found",
			id:            "nonexistent-id",
			expectedError: true,
			errorCode:     errors.ErrCodeNotFound,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.customerService.GetCustomer(s.ctx, tc.id)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
				if tc.errorCode != "" {
					var ierr *errors.InternalError
					s.True(stderrors.As(err, &ierr))
					s.Equal(tc.errorCode, ierr.Code)
				}
			} else {
				s.NoError(err)
				s.NotNil(resp)
				s.Equal(customer.Name, resp.Customer.Name)
				s.Equal(customer.AddressLine1, resp.Customer.AddressLine1)
				s.Equal(customer.AddressCountry, resp.Customer.AddressCountry)
				s.Equal(customer.Metadata["source"], resp.Customer.Metadata["source"])
			}
		})
	}
}

func (s *CustomerServiceSuite) TestGetCustomers() {
	// Reset and prepopulate the repository with customers
	s.repo = testutil.NewInMemoryCustomerStore()
	s.customerService = &customerService{repo: s.repo}
	_ = s.repo.Create(s.ctx, &customer.Customer{
		ID:             "cust-1",
		Name:           "Customer One",
		Email:          "one@example.com",
		AddressCountry: "US",
	})
	_ = s.repo.Create(s.ctx, &customer.Customer{
		ID:             "cust-2",
		Name:           "Customer Two",
		Email:          "two@example.com",
		AddressCountry: "GB",
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
		ID:             "cust-1",
		Name:           "Old Name",
		Email:          "old@example.com",
		AddressCountry: "US",
	})

	testCases := []struct {
		name          string
		id            string
		req           dto.UpdateCustomerRequest
		expectedError bool
		errorCode     string
	}{
		{
			name: "valid_update",
			id:   "cust-1",
			req: dto.UpdateCustomerRequest{
				Name:              lo.ToPtr("New Name"),
				Email:             lo.ToPtr("new@example.com"),
				AddressCountry:    lo.ToPtr("GB"),
				AddressCity:       lo.ToPtr("London"),
				AddressPostalCode: lo.ToPtr("SW1A 1AA"),
			},
			expectedError: false,
		},
		{
			name: "invalid_country_code",
			id:   "cust-1",
			req: dto.UpdateCustomerRequest{
				AddressCountry: lo.ToPtr("GBR"), // Invalid: should be 2 characters
			},
			expectedError: true,
			errorCode:     errors.ErrCodeValidation,
		},
		{
			name: "customer_not_found",
			id:   "nonexistent-id",
			req: dto.UpdateCustomerRequest{
				Name: lo.ToPtr("New Name"),
			},
			expectedError: true,
			errorCode:     errors.ErrCodeNotFound,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.customerService.UpdateCustomer(s.ctx, tc.id, tc.req)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
				if tc.errorCode != "" {
					var ierr *errors.InternalError
					s.True(stderrors.As(err, &ierr))
					s.Equal(tc.errorCode, ierr.Code)
				}
			} else {
				s.NoError(err)
				s.NotNil(resp)
				if tc.req.Name != nil {
					s.Equal(*tc.req.Name, resp.Customer.Name)
				}
				if tc.req.AddressCountry != nil {
					s.Equal(*tc.req.AddressCountry, resp.Customer.AddressCountry)
				}
				if tc.req.AddressCity != nil {
					s.Equal(*tc.req.AddressCity, resp.Customer.AddressCity)
				}
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
		errorCode     string
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
				if tc.errorCode != "" {
					var ierr *errors.InternalError
					s.True(stderrors.As(err, &ierr))
					s.Equal(tc.errorCode, ierr.Code)
				}
			} else {
				s.NoError(err)

				// Ensure the customer no longer exists
				_, err := s.customerService.GetCustomer(s.ctx, tc.id)
				s.Error(err)
				var ierr *errors.InternalError
				s.True(stderrors.As(err, &ierr))
				s.Equal(errors.ErrCodeNotFound, ierr.Code)
			}
		})
	}
}

func (s *CustomerServiceSuite) TestGetCustomerByLookupKey() {
	customer := &customer.Customer{
		ID:                "cust-1",
		ExternalID:        "ext-1",
		Name:              "Test Customer",
		Email:             "test@example.com",
		AddressLine1:      "123 Main St",
		AddressLine2:      "Apt 4B",
		AddressCity:       "New York",
		AddressState:      "NY",
		AddressPostalCode: "10001",
		AddressCountry:    "US",
		Metadata: map[string]string{
			"source": "web",
		},
	}
	_ = s.repo.Create(s.ctx, customer)

	testCases := []struct {
		name          string
		lookupKey     string
		expectedError bool
		errorCode     string
	}{
		{
			name:          "customer_found",
			lookupKey:     "ext-1",
			expectedError: false,
		},
		{
			name:          "customer_not_found",
			lookupKey:     "nonexistent-key",
			expectedError: true,
			errorCode:     errors.ErrCodeNotFound,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.customerService.GetCustomerByLookupKey(s.ctx, tc.lookupKey)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
				if tc.errorCode != "" {
					var ierr *errors.InternalError
					s.True(stderrors.As(err, &ierr))
					s.Equal(tc.errorCode, ierr.Code)
				}
			} else {
				s.NoError(err)
				s.NotNil(resp)
				s.Equal(customer.Name, resp.Customer.Name)
				s.Equal(customer.AddressLine1, resp.Customer.AddressLine1)
				s.Equal(customer.AddressCountry, resp.Customer.AddressCountry)
				s.Equal(customer.Metadata["source"], resp.Customer.Metadata["source"])
			}
		})
	}
}
