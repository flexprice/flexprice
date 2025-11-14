package hubspot

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// HubSpotCustomerService defines the interface for HubSpot customer operations
type HubSpotCustomerService interface {
	CreateCustomerFromHubSpot(
		ctx context.Context,
		hubspotContact *ContactResponse,
		dealID string,
		customerService interfaces.CustomerService,
	) error
}

// CustomerService handles HubSpot customer operations
type CustomerService struct {
	client                       HubSpotClient
	customerRepo                 customer.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewCustomerService creates a new HubSpot customer service
func NewCustomerService(
	client HubSpotClient,
	customerRepo customer.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) HubSpotCustomerService {
	return &CustomerService{
		client:                       client,
		customerRepo:                 customerRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// CreateCustomerFromHubSpot creates a customer in FlexPrice from HubSpot contact data
func (s *CustomerService) CreateCustomerFromHubSpot(
	ctx context.Context,
	hubspotContact *ContactResponse,
	dealID string,
	customerService interfaces.CustomerService,
) error {
	if hubspotContact == nil {
		return ierr.NewError("nil hubspot contact").Mark(ierr.ErrValidation)
	}
	if customerService == nil {
		return ierr.NewError("nil customer service").Mark(ierr.ErrInternal)
	}
	// Check if customer already exists by HubSpot contact ID in entity integration mapping
	if s.entityIntegrationMappingRepo != nil {
		filter := &types.EntityIntegrationMappingFilter{
			EntityType:        types.IntegrationEntityTypeCustomer,
			ProviderTypes:     []string{string(types.SecretProviderHubSpot)},
			ProviderEntityIDs: []string{hubspotContact.ID},
		}

		existingMappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
		if err == nil && existingMappings != nil && len(existingMappings) > 0 {
			s.logger.Infow("customer already mapped to HubSpot contact, skipping creation",
				"hubspot_contact_id", hubspotContact.ID,
				"customer_id", existingMappings[0].EntityID)
			return nil
		}
	}

	// Check if customer exists by email
	if hubspotContact.Properties.Email != "" {
		filter := &types.CustomerFilter{
			Email:       hubspotContact.Properties.Email,
			QueryFilter: types.NewDefaultQueryFilter(),
		}

		existingCustomers, err := customerService.GetCustomers(ctx, filter)
		if err == nil && existingCustomers != nil && len(existingCustomers.Items) > 0 {
			existingCustomer := existingCustomers.Items[0]
			s.logger.Infow("customer with same email already exists, creating mapping",
				"customer_id", existingCustomer.ID,
				"hubspot_contact_id", hubspotContact.ID)

			// Create entity mapping for existing customer
			if err := s.createEntityIntegrationMapping(ctx, existingCustomer.ID, hubspotContact, dealID); err != nil {
				s.logger.Warnw("failed to create mapping for existing customer",
					"error", err,
					"customer_id", existingCustomer.ID,
					"hubspot_contact_id", hubspotContact.ID)
			}
			return nil
		}
	}

	// Create new customer
	name := fmt.Sprintf("%s %s", hubspotContact.Properties.FirstName, hubspotContact.Properties.LastName)
	if name == " " {
		name = hubspotContact.Properties.Email
	}

	createReq := dto.CreateCustomerRequest{
		ExternalID: hubspotContact.ID, // Use HubSpot contact ID as external ID
		Name:       name,
		Email:      hubspotContact.Properties.Email,
		Metadata: map[string]string{
			"hubspot_contact_id": hubspotContact.ID,
			"hubspot_deal_id":    dealID,
			"source":             "hubspot",
		},
	}

	// Add address if available
	if hubspotContact.Properties.Address != "" {
		createReq.AddressLine1 = hubspotContact.Properties.Address
	}
	if hubspotContact.Properties.City != "" {
		createReq.AddressCity = hubspotContact.Properties.City
	}
	if hubspotContact.Properties.State != "" {
		createReq.AddressState = hubspotContact.Properties.State
	}
	if hubspotContact.Properties.Country != "" {
		createReq.AddressCountry = hubspotContact.Properties.Country
	}
	if hubspotContact.Properties.Zip != "" {
		createReq.AddressPostalCode = hubspotContact.Properties.Zip
	}

	customerResp, err := customerService.CreateCustomer(ctx, createReq)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create customer in FlexPrice").
			Mark(ierr.ErrInternal)
	}

	s.logger.Infow("successfully created customer from HubSpot contact",
		"customer_id", customerResp.ID,
		"hubspot_contact_id", hubspotContact.ID)

	// Create entity mapping for new customer
	if err := s.createEntityIntegrationMapping(ctx, customerResp.ID, hubspotContact, dealID); err != nil {
		s.logger.Warnw("failed to create mapping for new customer",
			"error", err,
			"customer_id", customerResp.ID,
			"hubspot_contact_id", hubspotContact.ID)
	}

	return nil
}

// createEntityIntegrationMapping creates an entity integration mapping for a customer
func (s *CustomerService) createEntityIntegrationMapping(
	ctx context.Context,
	customerID string,
	hubspotContact *ContactResponse,
	dealID string,
) error {
	if s.entityIntegrationMappingRepo == nil {
		return nil
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         customerID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderHubSpot),
		ProviderEntityID: hubspotContact.ID,
		Metadata: map[string]interface{}{
			"created_via":     "provider_to_flexprice",
			"hubspot_deal_id": dealID,
			"synced_at":       time.Now().UTC().Format(time.RFC3339),
		},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	err := s.entityIntegrationMappingRepo.Create(ctx, mapping)
	if err != nil {
		s.logger.Warnw("failed to create entity mapping for customer",
			"error", err,
			"customer_id", customerID,
			"hubspot_contact_id", hubspotContact.ID)
		return err
	}

	s.logger.Infow("created entity integration mapping",
		"flexprice_customer_id", customerID,
		"hubspot_contact_id", hubspotContact.ID)

	return nil
}
