package nomod

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// NomodCustomerService defines the interface for Nomod customer operations
type NomodCustomerService interface {
	EnsureCustomerSyncedToNomod(ctx context.Context, customerID string, customerService interfaces.CustomerService) (*customer.Customer, error)
	SyncCustomerToNomod(ctx context.Context, flexpriceCustomer *customer.Customer) (string, error)
	GetNomodCustomerID(ctx context.Context, customerID string) (string, error)
}

// CustomerService handles Nomod customer operations
type CustomerService struct {
	client                       NomodClient
	customerRepo                 customer.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewCustomerService creates a new Nomod customer service
func NewCustomerService(
	client NomodClient,
	customerRepo customer.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) NomodCustomerService {
	return &CustomerService{
		client:                       client,
		customerRepo:                 customerRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// EnsureCustomerSyncedToNomod ensures a customer is synced to Nomod
func (s *CustomerService) EnsureCustomerSyncedToNomod(ctx context.Context, customerID string, customerService interfaces.CustomerService) (*customer.Customer, error) {
	// Get FlexPrice customer
	customerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get customer").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrNotFound)
	}
	flexpriceCustomer := customerResp.Customer

	// Check if customer already has Nomod ID in metadata
	if nomodID, exists := flexpriceCustomer.Metadata["nomod_customer_id"]; exists && nomodID != "" {
		s.logger.Infow("customer already synced to Nomod",
			"customer_id", customerID,
			"nomod_customer_id", nomodID)
		return flexpriceCustomer, nil
	}

	// Check if customer is synced via integration mapping table
	if s.entityIntegrationMappingRepo != nil {
		filter := &types.EntityIntegrationMappingFilter{
			EntityID:      customerID,
			EntityType:    types.IntegrationEntityTypeCustomer,
			ProviderTypes: []string{string(types.SecretProviderNomod)},
		}

		existingMappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
		if err == nil && existingMappings != nil && len(existingMappings) > 0 {
			existingMapping := existingMappings[0]
			s.logger.Infow("customer already mapped to Nomod via integration mapping",
				"customer_id", customerID,
				"nomod_customer_id", existingMapping.ProviderEntityID)

			// Update customer metadata with Nomod ID for faster future lookups
			updateReq := dto.UpdateCustomerRequest{
				Metadata: s.mergeCustomerMetadata(flexpriceCustomer.Metadata, map[string]string{
					"nomod_customer_id": existingMapping.ProviderEntityID,
				}),
			}
			updatedCustomerResp, err := customerService.UpdateCustomer(ctx, flexpriceCustomer.ID, updateReq)
			if err != nil {
				s.logger.Warnw("failed to update customer metadata with Nomod customer ID",
					"customer_id", customerID,
					"error", err)
				// Return original customer info if update fails
				return flexpriceCustomer, nil
			}
			return updatedCustomerResp.Customer, nil
		}
	}

	// Customer is not synced, create in Nomod
	s.logger.Infow("customer not synced to Nomod, creating in Nomod",
		"customer_id", customerID)
	err = s.CreateCustomerInNomod(ctx, customerID, customerService)
	if err != nil {
		return nil, err
	}

	// Get updated customer after sync
	updatedCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}

	return updatedCustomerResp.Customer, nil
}

// CreateCustomerInNomod creates a customer in Nomod and updates our customer with Nomod ID
func (s *CustomerService) CreateCustomerInNomod(ctx context.Context, customerID string, customerService interfaces.CustomerService) error {
	// Get FlexPrice customer
	customerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return err
	}
	flexpriceCustomer := customerResp.Customer

	// Check if customer already has Nomod ID
	if nomodID, exists := flexpriceCustomer.Metadata["nomod_customer_id"]; exists && nomodID != "" {
		return ierr.NewError("customer already has Nomod ID").
			WithHint("Customer is already synced with Nomod").
			Mark(ierr.ErrAlreadyExists)
	}

	// Create customer in Nomod
	nomodCustomerID, err := s.SyncCustomerToNomod(ctx, flexpriceCustomer)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to sync customer to Nomod").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrSystem)
	}

	// Update our customer with Nomod ID
	updateReq := dto.UpdateCustomerRequest{
		Metadata: s.mergeCustomerMetadata(flexpriceCustomer.Metadata, map[string]string{
			"nomod_customer_id": nomodCustomerID,
		}),
	}

	_, err = customerService.UpdateCustomer(ctx, flexpriceCustomer.ID, updateReq)
	if err != nil {
		return err
	}

	return nil
}

// SyncCustomerToNomod creates a customer in Nomod and stores the mapping
func (s *CustomerService) SyncCustomerToNomod(ctx context.Context, flexpriceCustomer *customer.Customer) (string, error) {
	// Prepare customer data for Nomod
	req := CreateCustomerRequest{
		FirstName: flexpriceCustomer.Name,
		Email:     flexpriceCustomer.Email,
	}

	s.logger.Infow("creating customer in Nomod",
		"customer_id", flexpriceCustomer.ID,
		"email", flexpriceCustomer.Email)

	// Create customer in Nomod
	nomodCustomer, err := s.client.CreateCustomer(ctx, req)
	if err != nil {
		s.logger.Errorw("failed to create customer in Nomod",
			"error", err,
			"customer_id", flexpriceCustomer.ID)
		return "", err
	}

	nomodCustomerID := nomodCustomer.ID

	s.logger.Infow("created customer in Nomod",
		"customer_id", flexpriceCustomer.ID,
		"nomod_customer_id", nomodCustomerID)

	// Store mapping in entity_integration_mapping
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         flexpriceCustomer.ID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderNomod),
		ProviderEntityID: nomodCustomerID,
		Metadata: map[string]interface{}{
			"created_via":       "flexprice_to_provider",
			"nomod_customer_id": nomodCustomerID,
			"synced_at":         time.Now().UTC().Format(time.RFC3339),
		},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	err = s.entityIntegrationMappingRepo.Create(ctx, mapping)
	if err != nil {
		s.logger.Errorw("failed to store Nomod customer mapping",
			"error", err,
			"customer_id", flexpriceCustomer.ID,
			"nomod_customer_id", nomodCustomerID)
		// Don't fail the entire operation if mapping storage fails
		// The customer was created successfully in Nomod
	} else {
		s.logger.Infow("stored Nomod customer mapping",
			"customer_id", flexpriceCustomer.ID,
			"nomod_customer_id", nomodCustomerID)
	}

	return nomodCustomerID, nil
}

// GetNomodCustomerID retrieves the Nomod customer ID for a FlexPrice customer
func (s *CustomerService) GetNomodCustomerID(ctx context.Context, customerID string) (string, error) {
	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      customerID,
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{string(types.SecretProviderNomod)},
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to get Nomod customer mapping").
			Mark(ierr.ErrSystem)
	}

	if len(mappings) == 0 {
		return "", ierr.NewError("customer not found in Nomod").
			WithHint("Customer has not been synced to Nomod").
			Mark(ierr.ErrNotFound)
	}

	return mappings[0].ProviderEntityID, nil
}

// mergeCustomerMetadata merges new metadata with existing customer metadata
func (s *CustomerService) mergeCustomerMetadata(existingMetadata map[string]string, newMetadata map[string]string) map[string]string {
	merged := make(map[string]string)

	// Copy existing metadata
	for k, v := range existingMetadata {
		merged[k] = v
	}

	// Add/override with new metadata
	for k, v := range newMetadata {
		merged[k] = v
	}

	return merged
}
