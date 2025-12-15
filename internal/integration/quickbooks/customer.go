package quickbooks

import (
	"context"
	"time"

	customerDomain "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// QuickBooksCustomerService defines the interface for QuickBooks customer operations
type QuickBooksCustomerService interface {
	GetOrCreateQuickBooksCustomer(ctx context.Context, flexpriceCustomer *customerDomain.Customer) (string, error)
	GetQuickBooksCustomerID(ctx context.Context, flexpriceCustomerID string) (string, error)
}

// CustomerServiceParams holds dependencies for CustomerService
type CustomerServiceParams struct {
	Client                       QuickBooksClient
	CustomerRepo                 customerDomain.Repository
	EntityIntegrationMappingRepo entityintegrationmapping.Repository
	Logger                       *logger.Logger
}

// CustomerService handles QuickBooks customer synchronization
type CustomerService struct {
	CustomerServiceParams
}

// NewCustomerService creates a new QuickBooks customer service
func NewCustomerService(params CustomerServiceParams) QuickBooksCustomerService {
	return &CustomerService{
		CustomerServiceParams: params,
	}
}

// GetQuickBooksCustomerID retrieves the QuickBooks customer ID from entity mapping
func (s *CustomerService) GetQuickBooksCustomerID(ctx context.Context, flexpriceCustomerID string) (string, error) {
	filter := types.NewEntityIntegrationMappingFilter()
	filter.EntityType = types.IntegrationEntityTypeCustomer
	filter.EntityID = flexpriceCustomerID
	filter.ProviderTypes = []string{string(types.SecretProviderQuickBooks)}

	mappings, err := s.EntityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to query entity mapping for customer").
			WithReportableDetails(map[string]interface{}{
				"flexprice_customer_id": flexpriceCustomerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		return "", ierr.NewError("customer not synced to QuickBooks").
			WithHint("Please sync customer to QuickBooks first").
			WithReportableDetails(map[string]interface{}{
				"flexprice_customer_id": flexpriceCustomerID,
			}).
			Mark(ierr.ErrNotFound)
	}

	return mappings[0].ProviderEntityID, nil
}

// GetOrCreateQuickBooksCustomer gets existing or creates a new customer in QuickBooks.
// This method implements the customer sync strategy:
// 1. First checks if customer is already mapped in entity_integration_mapping
// 2. If not mapped, tries to find existing customer in QuickBooks by email
// 3. If not found, creates a new customer in QuickBooks
// Returns the QuickBooks customer ID for use in invoice creation.
func (s *CustomerService) GetOrCreateQuickBooksCustomer(ctx context.Context, flexpriceCustomer *customerDomain.Customer) (string, error) {
	// Check if customer already has a mapping in our database
	quickBooksCustomerID, err := s.GetQuickBooksCustomerID(ctx, flexpriceCustomer.ID)
	if err == nil && quickBooksCustomerID != "" {
		return quickBooksCustomerID, nil
	}

	// If error is not "not found", it's a database/infrastructure error - return it
	if err != nil && !ierr.IsNotFound(err) {
		return "", err
	}

	// Try to find existing customer in QuickBooks by email to avoid duplicates
	// This handles cases where customer was created manually in QuickBooks
	if flexpriceCustomer.Email != "" {
		existingCustomer, err := s.Client.QueryCustomerByEmail(ctx, flexpriceCustomer.Email)
		if err == nil && existingCustomer != nil && existingCustomer.ID != "" {
			s.Logger.Debugw("found existing customer in QuickBooks by email",
				"flexprice_customer_id", flexpriceCustomer.ID,
				"quickbooks_customer_id", existingCustomer.ID,
				"email", flexpriceCustomer.Email)
			// Create mapping for the existing QuickBooks customer
			if err := s.createCustomerMapping(ctx, flexpriceCustomer.ID, existingCustomer.ID, existingCustomer); err != nil {
				s.Logger.Warnw("failed to create customer mapping",
					"error", err,
					"flexprice_customer_id", flexpriceCustomer.ID)
			}
			return existingCustomer.ID, nil
		}
	}

	// Customer doesn't exist in QuickBooks, create new one
	customerResp, err := s.SyncCustomerToQuickBooks(ctx, flexpriceCustomer)
	if err != nil {
		return "", err
	}

	return customerResp.ID, nil
}

// SyncCustomerToQuickBooks syncs Flexprice customer to QuickBooks.
// Creates a customer in QuickBooks with DisplayName, email, and billing address.
// If customer creation fails (e.g., name already exists), attempts to find existing customer by name
// and creates a mapping for it to avoid duplicate creation attempts.
func (s *CustomerService) SyncCustomerToQuickBooks(ctx context.Context, flexpriceCustomer *customerDomain.Customer) (*CustomerResponse, error) {
	displayName := flexpriceCustomer.Name
	if displayName == "" {
		return nil, ierr.NewError("customer name is required").
			WithHint("DisplayName is required for QuickBooks customer").
			Mark(ierr.ErrValidation)
	}

	createReq := &CustomerCreateRequest{
		DisplayName: displayName,
	}

	// Add email if available - used for customer lookup and communication
	if flexpriceCustomer.Email != "" {
		createReq.PrimaryEmailAddr = &EmailAddress{
			Address: flexpriceCustomer.Email,
		}
	}

	// Add billing address if available - required for proper invoice generation
	if flexpriceCustomer.AddressLine1 != "" || flexpriceCustomer.AddressCity != "" {
		createReq.BillAddr = &Address{
			Line1:                  flexpriceCustomer.AddressLine1,
			Line2:                  flexpriceCustomer.AddressLine2,
			City:                   flexpriceCustomer.AddressCity,
			CountrySubDivisionCode: flexpriceCustomer.AddressState,
			PostalCode:             flexpriceCustomer.AddressPostalCode,
			Country:                flexpriceCustomer.AddressCountry,
		}
	}

	customerResp, err := s.Client.CreateCustomer(ctx, createReq)
	if err != nil {
		// If creation fails, customer might already exist with same name
		// Try to find existing customer and create mapping instead of failing
		existingCustomer, queryErr := s.Client.QueryCustomerByName(ctx, displayName)
		if queryErr == nil && existingCustomer != nil {
			s.Logger.Infow("found existing customer in QuickBooks by name",
				"customer_id", flexpriceCustomer.ID,
				"quickbooks_customer_id", existingCustomer.ID,
				"display_name", displayName)
			// Create mapping for existing customer to prevent future creation attempts
			if mapErr := s.createCustomerMapping(ctx, flexpriceCustomer.ID, existingCustomer.ID, existingCustomer); mapErr != nil {
				s.Logger.Errorw("failed to create mapping for existing customer",
					"customer_id", flexpriceCustomer.ID,
					"quickbooks_customer_id", existingCustomer.ID,
					"error", mapErr)
			}
			return existingCustomer, nil
		}

		return nil, ierr.WithError(err).
			WithHint("Failed to create customer in QuickBooks and could not find existing customer").
			Mark(ierr.ErrValidation)
	}

	s.Logger.Infow("successfully created customer in QuickBooks",
		"flexprice_customer_id", flexpriceCustomer.ID,
		"quickbooks_customer_id", customerResp.ID,
		"display_name", customerResp.DisplayName)

	// Create entity integration mapping using the reusable method
	if err := s.createCustomerMapping(ctx, flexpriceCustomer.ID, customerResp.ID, customerResp); err != nil {
		s.Logger.Errorw("failed to save entity mapping",
			"customer_id", flexpriceCustomer.ID,
			"quickbooks_customer_id", customerResp.ID,
			"error", err)
		// Note: Customer was created in QuickBooks but mapping failed.
		// The name-based lookup in SyncCustomerToQuickBooks (line ~156) should prevent duplicates on retry.
	}

	return customerResp, nil
}

// createCustomerMapping creates an entity integration mapping for customer.
// Maps Flexprice customer_id to QuickBooks customer ID for use in invoice creation.
// Stores customer details (display name, email, billing address) in metadata as per requirements.
func (s *CustomerService) createCustomerMapping(
	ctx context.Context,
	flexPriceCustomerID string,
	quickBooksCustomerID string,
	quickBooksCustomer *CustomerResponse,
) error {
	if s.EntityIntegrationMappingRepo == nil {
		s.Logger.Warnw("EntityIntegrationMappingRepo is nil, skipping mapping creation",
			"flexprice_customer_id", flexPriceCustomerID,
			"quickbooks_customer_id", quickBooksCustomerID)
		return nil
	}

	flexpriceCustomer, err := s.CustomerRepo.Get(ctx, flexPriceCustomerID)
	if err != nil {
		return err
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         flexPriceCustomerID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderQuickBooks),
		ProviderEntityID: quickBooksCustomerID,
		Metadata: map[string]interface{}{
			"synced_at":                        time.Now().UTC().Format(time.RFC3339),
			"quickbooks_customer_display_name": quickBooksCustomer.DisplayName,
		},
		EnvironmentID: flexpriceCustomer.EnvironmentID,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	mapping.TenantID = flexpriceCustomer.TenantID

	if quickBooksCustomer.PrimaryEmailAddr != nil && quickBooksCustomer.PrimaryEmailAddr.Address != "" {
		mapping.Metadata["quickbooks_customer_primary_email_addr_address"] = quickBooksCustomer.PrimaryEmailAddr.Address
	}

	if quickBooksCustomer.BillAddr != nil {
		if quickBooksCustomer.BillAddr.Line1 != "" {
			mapping.Metadata["quickbooks_customer_bill_addr_line1"] = quickBooksCustomer.BillAddr.Line1
		}
		if quickBooksCustomer.BillAddr.Line2 != "" {
			mapping.Metadata["quickbooks_customer_bill_addr_line2"] = quickBooksCustomer.BillAddr.Line2
		}
		if quickBooksCustomer.BillAddr.City != "" {
			mapping.Metadata["quickbooks_customer_bill_addr_city"] = quickBooksCustomer.BillAddr.City
		}
		if quickBooksCustomer.BillAddr.Country != "" {
			mapping.Metadata["quickbooks_customer_bill_addr_country"] = quickBooksCustomer.BillAddr.Country
		}
		if quickBooksCustomer.BillAddr.PostalCode != "" {
			mapping.Metadata["quickbooks_customer_bill_addr_postal_code"] = quickBooksCustomer.BillAddr.PostalCode
		}
	}

	return s.EntityIntegrationMappingRepo.Create(ctx, mapping)
}
