// Test code for the customer service
package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type CustomerServiceSuite struct {
	testutil.BaseServiceTestSuite
	service CustomerService
	ctx     context.Context
}

func TestCustomerService(t *testing.T) {
	suite.Run(t, new(CustomerServiceSuite))
}

func (s *CustomerServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.ctx = context.Background()
	s.service = NewCustomerService(ServiceParams{
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

func (s *CustomerServiceSuite) TestCreateCustomer() {
	testCases := []struct {
		name          string
		request       dto.CreateCustomerRequest
		setup         func()
		expectedError bool
		errorCode     string
	}{
		{
			name: "valid_customer",
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
			errorCode:     ierr.ErrCodeValidation,
		},
		{
			name: "invalid_postal_code",
			request: dto.CreateCustomerRequest{
				ExternalID:        "ext-3",
				Name:              "Test Customer",
				AddressPostalCode: "12345678901234567890123", // Too long
			},
			expectedError: true,
			errorCode:     ierr.ErrCodeValidation,
		},
		{
			name: "invalid_address_line1",
			request: dto.CreateCustomerRequest{
				ExternalID:   "ext-4",
				Name:         "Test Customer",
				AddressLine1: string(make([]byte, 256)), // Too long
			},
			expectedError: true,
			errorCode:     ierr.ErrCodeValidation,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			resp, err := s.service.CreateCustomer(s.ctx, tc.request)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
				if tc.errorCode == ierr.ErrCodeValidation {
					s.True(ierr.IsValidation(err), "Expected validation error")
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
				s.NotEmpty(resp.Customer.ID)
				s.NotEmpty(resp.Customer.CreatedAt)
				s.NotEmpty(resp.Customer.UpdatedAt)
			}
		})
	}
}

func (s *CustomerServiceSuite) TestGetCustomer() {
	customer := &domainCustomer.Customer{
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
	_ = s.GetStores().CustomerRepo.Create(s.ctx, customer)

	testCases := []struct {
		name             string
		id               string
		setup            func()
		expectedError    bool
		errorCode        string
		expectedCustomer *domainCustomer.Customer
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
			errorCode:     ierr.ErrCodeNotFound,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.GetCustomer(s.ctx, tc.id)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
				if tc.errorCode == ierr.ErrCodeNotFound {
					s.True(ierr.IsNotFound(err), "Expected not found error")
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
	_ = s.GetStores().CustomerRepo.Create(s.ctx, &domainCustomer.Customer{
		ID:             "cust-1",
		Name:           "Customer One",
		Email:          "one@example.com",
		AddressCountry: "US",
	})
	_ = s.GetStores().CustomerRepo.Create(s.ctx, &domainCustomer.Customer{
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
			resp, err := s.service.GetCustomers(s.ctx, tc.filter)

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
	_ = s.GetStores().CustomerRepo.Create(s.ctx, &domainCustomer.Customer{
		ID:             "cust-1",
		Name:           "Old Name",
		Email:          "old@example.com",
		AddressCountry: "US",
	})

	testCases := []struct {
		name          string
		id            string
		request       dto.UpdateCustomerRequest
		setup         func()
		expectedError bool
		errorCode     string
	}{
		{
			name: "valid_update",
			id:   "cust-1",
			request: dto.UpdateCustomerRequest{
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
			request: dto.UpdateCustomerRequest{
				AddressCountry: lo.ToPtr("GBR"), // Invalid: should be 2 characters
			},
			setup: func() {
				// Create a customer to update
				s.service.CreateCustomer(s.ctx, dto.CreateCustomerRequest{
					ExternalID:     "cust-1",
					Name:           "Original Name",
					AddressCountry: "US",
				})
			},
			expectedError: true,
			errorCode:     ierr.ErrCodeValidation,
		},
		{
			name: "customer_not_found",
			id:   "nonexistent-id",
			request: dto.UpdateCustomerRequest{
				Name: lo.ToPtr("New Name"),
			},
			expectedError: true,
			errorCode:     ierr.ErrCodeNotFound,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.UpdateCustomer(s.ctx, tc.id, tc.request)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
				if tc.errorCode == ierr.ErrCodeNotFound {
					s.True(ierr.IsNotFound(err), "Expected not found error")
				} else if tc.errorCode == ierr.ErrCodeValidation {
					s.True(ierr.IsValidation(err), "Expected validation error")
				}
			} else {
				s.NoError(err)
				s.NotNil(resp)
				if tc.request.Name != nil {
					s.Equal(*tc.request.Name, resp.Customer.Name)
				}
				if tc.request.AddressCountry != nil {
					s.Equal(*tc.request.AddressCountry, resp.Customer.AddressCountry)
				}
				if tc.request.AddressCity != nil {
					s.Equal(*tc.request.AddressCity, resp.Customer.AddressCity)
				}
			}
		})
	}
}

func (s *CustomerServiceSuite) TestDeleteCustomer() {
	testCases := []struct {
		name          string
		id            string
		setup         func()
		expectedError bool
		errorCode     string
	}{
		{
			name: "delete_existing_customer",
			id:   "cust-1",
			setup: func() {
				_ = s.GetStores().CustomerRepo.Create(s.ctx, &domainCustomer.Customer{
					ID:    "cust-1",
					Name:  "To Be Deleted",
					Email: "delete@example.com",
					BaseModel: types.BaseModel{
						Status: types.StatusPublished,
					},
				})
			},
			expectedError: false,
		},
		{
			name:          "delete_nonexistent_customer",
			id:            "nonexistent-id",
			expectedError: true,
			errorCode:     ierr.ErrCodeNotFound,
		},
		{
			name: "delete_unpublished_customer",
			id:   "cust-unpublished",
			setup: func() {
				_ = s.GetStores().CustomerRepo.Create(s.ctx, &domainCustomer.Customer{
					ID:    "cust-unpublished",
					Name:  "Unpublished Customer",
					Email: "unpublished@example.com",
					BaseModel: types.BaseModel{
						Status: types.StatusDeleted,
					},
				})
			},
			expectedError: true,
			errorCode:     ierr.ErrCodeNotFound,
		},
		{
			name: "customer_with_active_subscription",
			id:   "cust-2",
			setup: func() {
				_ = s.GetStores().CustomerRepo.Create(s.ctx, &domainCustomer.Customer{
					ID:    "cust-2",
					Name:  "Customer with Subscription",
					Email: "sub@example.com",
					BaseModel: types.BaseModel{
						Status: types.StatusPublished,
					},
				})
				_ = s.GetStores().SubscriptionRepo.Create(s.ctx, &subscription.Subscription{
					ID:                 "sub-1",
					CustomerID:         "cust-2",
					SubscriptionStatus: types.SubscriptionStatusActive,
				})
			},
			expectedError: true,
			errorCode:     ierr.ErrCodeInvalidOperation,
		},
		{
			name: "customer_with_wallets",
			id:   "cust-4",
			setup: func() {
				_ = s.GetStores().CustomerRepo.Create(s.ctx, &domainCustomer.Customer{
					ID:    "cust-4",
					Name:  "Customer with Wallet",
					Email: "wallet@example.com",
					BaseModel: types.BaseModel{
						Status: types.StatusPublished,
					},
				})
				_ = s.GetStores().WalletRepo.CreateWallet(s.ctx, &wallet.Wallet{
					ID:         "wallet-1",
					CustomerID: "cust-4",
				})
			},
			expectedError: true,
			errorCode:     ierr.ErrCodeInvalidOperation,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Reset repositories for each test case
			s.SetupTest()

			if tc.setup != nil {
				tc.setup()
			}

			err := s.service.DeleteCustomer(s.ctx, tc.id)

			if tc.expectedError {
				s.Error(err)
				if tc.errorCode == ierr.ErrCodeNotFound {
					s.True(ierr.IsNotFound(err), "Expected not found error")
					if tc.name == "delete_unpublished_customer" {
						s.Contains(err.Error(), "customer is not published")
					}
				} else if tc.errorCode == ierr.ErrCodeInvalidOperation {
					s.True(ierr.IsInvalidOperation(err), "Expected invalid operation error")
					switch tc.name {
					case "customer_with_active_subscription":
						s.Contains(err.Error(), "customer cannot be deleted due to active subscriptions")
					case "customer_with_invoices":
						s.Contains(err.Error(), "customer cannot be deleted due to active invoices")
					case "customer_with_wallets":
						s.Contains(err.Error(), "customer cannot be deleted due to associated wallets")
					}
				}
			} else {
				s.NoError(err)
				// Verify the customer was deleted
				_, err := s.service.GetCustomer(s.ctx, tc.id)
				s.Error(err)
				s.True(ierr.IsNotFound(err), "Expected not found error after deletion")
			}
		})
	}
}

func (s *CustomerServiceSuite) TestGetCustomerByLookupKey() {
	customer := &domainCustomer.Customer{
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
	_ = s.GetStores().CustomerRepo.Create(s.ctx, customer)

	testCases := []struct {
		name             string
		lookupKey        string
		setup            func()
		expectedError    bool
		errorCode        string
		expectedCustomer *domainCustomer.Customer
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
			errorCode:     ierr.ErrCodeNotFound,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.GetCustomerByLookupKey(s.ctx, tc.lookupKey)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
				if tc.errorCode == ierr.ErrCodeNotFound {
					s.True(ierr.IsNotFound(err), "Expected not found error")
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
