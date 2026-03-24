package paddle

import (
	"context"
	"time"

	"github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/PaddleHQ/paddle-go-sdk/v4/pkg/paddlenotification"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// PaddleCustomerService defines the interface for Paddle customer operations
type PaddleCustomerService interface {
	EnsureCustomerSyncedToPaddle(ctx context.Context, customerID string, customerService interfaces.CustomerService) (*customer.Customer, error)
	SyncCustomerToPaddle(ctx context.Context, flexpriceCustomer *customer.Customer) (string, error)
	GetPaddleCustomerID(ctx context.Context, customerID string) (string, error)
	CreateCustomerFromPaddle(ctx context.Context, paddleCustomer *paddlenotification.CustomerNotification, customerService interfaces.CustomerService) error
}

// CustomerService handles Paddle customer operations
type CustomerService struct {
	client                       PaddleClient
	customerRepo                 customer.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewCustomerService creates a new Paddle customer service
func NewCustomerService(
	client PaddleClient,
	customerRepo customer.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) PaddleCustomerService {
	return &CustomerService{
		client:                       client,
		customerRepo:                 customerRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// EnsureCustomerSyncedToPaddle ensures a customer is synced to Paddle
func (s *CustomerService) EnsureCustomerSyncedToPaddle(ctx context.Context, customerID string, customerService interfaces.CustomerService) (*customer.Customer, error) {
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

	// Check if customer already has Paddle ID in metadata
	if paddleID, exists := flexpriceCustomer.Metadata["paddle_customer_id"]; exists && paddleID != "" {
		s.logger.Infow("customer already synced to Paddle",
			"customer_id", customerID,
			"paddle_customer_id", paddleID)
		return flexpriceCustomer, nil
	}

	// Check if customer is synced via integration mapping table
	if s.entityIntegrationMappingRepo != nil {
		filter := &types.EntityIntegrationMappingFilter{
			EntityID:      customerID,
			EntityType:    types.IntegrationEntityTypeCustomer,
			ProviderTypes: []string{string(types.SecretProviderPaddle)},
		}

		existingMappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
		if err == nil && existingMappings != nil && len(existingMappings) > 0 {
			existingMapping := existingMappings[0]
			s.logger.Infow("customer already mapped to Paddle via integration mapping",
				"customer_id", customerID,
				"paddle_customer_id", existingMapping.ProviderEntityID)

			updateReq := dto.UpdateCustomerRequest{
				Metadata: s.mergeCustomerMetadata(flexpriceCustomer.Metadata, map[string]string{
					"paddle_customer_id": existingMapping.ProviderEntityID,
				}),
			}
			updatedCustomerResp, err := customerService.UpdateCustomer(ctx, flexpriceCustomer.ID, updateReq)
			if err != nil {
				s.logger.Warnw("failed to update customer metadata with Paddle customer ID",
					"customer_id", customerID,
					"error", err)
				return flexpriceCustomer, nil
			}
			return updatedCustomerResp.Customer, nil
		}
	}

	// Customer is not synced, create in Paddle
	s.logger.Infow("customer not synced to Paddle, creating in Paddle",
		"customer_id", customerID)
	err = s.CreateCustomerInPaddle(ctx, customerID, customerService)
	if err != nil {
		return nil, err
	}

	updatedCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}

	return updatedCustomerResp.Customer, nil
}

// CreateCustomerInPaddle creates a customer in Paddle and updates our customer with Paddle ID
func (s *CustomerService) CreateCustomerInPaddle(ctx context.Context, customerID string, customerService interfaces.CustomerService) error {
	customerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return err
	}
	flexpriceCustomer := customerResp.Customer

	if paddleID, exists := flexpriceCustomer.Metadata["paddle_customer_id"]; exists && paddleID != "" {
		return ierr.NewError("customer already has Paddle ID").
			WithHint("Customer is already synced with Paddle").
			Mark(ierr.ErrAlreadyExists)
	}

	paddleCustomerID, err := s.SyncCustomerToPaddle(ctx, flexpriceCustomer)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to sync customer to Paddle").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrSystem)
	}

	updateReq := dto.UpdateCustomerRequest{
		Metadata: s.mergeCustomerMetadata(flexpriceCustomer.Metadata, map[string]string{
			"paddle_customer_id": paddleCustomerID,
		}),
	}

	_, err = customerService.UpdateCustomer(ctx, flexpriceCustomer.ID, updateReq)
	if err != nil {
		return err
	}

	return nil
}

// SyncCustomerToPaddle creates a customer and address in Paddle and stores the mapping
func (s *CustomerService) SyncCustomerToPaddle(ctx context.Context, flexpriceCustomer *customer.Customer) (string, error) {
	// Paddle requires email for customer creation
	if flexpriceCustomer.Email == "" {
		s.logger.Errorw("customer not synced to Paddle: email is required",
			"customer_id", flexpriceCustomer.ID,
			"reason", "email_required")
		return "", ierr.NewError("customer email is required for Paddle sync").
			WithHint("Add email to the customer before syncing to Paddle").
			WithReportableDetails(map[string]interface{}{
				"customer_id": flexpriceCustomer.ID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Build CreateCustomerRequest
	createCustomerReq := &paddle.CreateCustomerRequest{
		Email: flexpriceCustomer.Email,
	}
	if flexpriceCustomer.Name != "" {
		createCustomerReq.Name = paddle.PtrTo(flexpriceCustomer.Name)
	}
	createCustomerReq.CustomData = map[string]interface{}{
		"flexprice_customer_id": flexpriceCustomer.ID,
		"environment_id":        types.GetEnvironmentID(ctx),
	}

	s.logger.Infow("creating customer in Paddle",
		"customer_id", flexpriceCustomer.ID)

	paddleCustomer, err := s.client.CreateCustomer(ctx, createCustomerReq)
	if err != nil {
		s.logger.Errorw("failed to create customer in Paddle",
			"error", err,
			"customer_id", flexpriceCustomer.ID)
		return "", err
	}

	paddleCustomerID := paddleCustomer.ID

	s.logger.Infow("created customer in Paddle",
		"customer_id", flexpriceCustomer.ID,
		"paddle_customer_id", paddleCustomerID)

	// Create address if we have address data (country_code is required for Paddle address)
	var paddleAddressID string
	if flexpriceCustomer.AddressCountry != "" {
		createAddressReq := &paddle.CreateAddressRequest{
			CustomerID:  paddleCustomerID,
			CountryCode: toCountryCode(flexpriceCustomer.AddressCountry),
		}
		if flexpriceCustomer.AddressLine1 != "" {
			createAddressReq.FirstLine = paddle.PtrTo(flexpriceCustomer.AddressLine1)
		}
		if flexpriceCustomer.AddressLine2 != "" {
			createAddressReq.SecondLine = paddle.PtrTo(flexpriceCustomer.AddressLine2)
		}
		if flexpriceCustomer.AddressCity != "" {
			createAddressReq.City = paddle.PtrTo(flexpriceCustomer.AddressCity)
		}
		if flexpriceCustomer.AddressPostalCode != "" {
			createAddressReq.PostalCode = paddle.PtrTo(flexpriceCustomer.AddressPostalCode)
		}
		if flexpriceCustomer.AddressState != "" {
			createAddressReq.Region = paddle.PtrTo(flexpriceCustomer.AddressState)
		}

		address, err := s.client.CreateAddress(ctx, paddleCustomerID, createAddressReq)
		if err != nil {
			s.logger.Warnw("failed to create address in Paddle (customer was created)",
				"error", err,
				"customer_id", flexpriceCustomer.ID,
				"paddle_customer_id", paddleCustomerID)
			// Don't fail - customer was created successfully
		} else {
			paddleAddressID = address.ID
			s.logger.Infow("created address in Paddle",
				"customer_id", flexpriceCustomer.ID,
				"paddle_customer_id", paddleCustomerID,
				"paddle_address_id", paddleAddressID)
		}
	}

	// Store mapping in entity_integration_mapping
	mappingMetadata := map[string]interface{}{
		"created_via":         "flexprice_to_provider",
		"paddle_customer_id":  paddleCustomerID,
		"synced_at":           time.Now().UTC().Format(time.RFC3339),
	}
	if paddleAddressID != "" {
		mappingMetadata["paddle_address_id"] = paddleAddressID
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         flexpriceCustomer.ID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: paddleCustomerID,
		Metadata:         mappingMetadata,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}

	err = s.entityIntegrationMappingRepo.Create(ctx, mapping)
	if err != nil {
		if ierr.IsAlreadyExists(err) {
			// Concurrent race: another goroutine created the mapping between our check and now.
			// The mapping that won the race is the authoritative one — return its Paddle customer
			// ID so the caller does not overwrite metadata with the ID of a duplicate Paddle customer.
			existingMappings, listErr := s.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
				EntityID:      flexpriceCustomer.ID,
				EntityType:    types.IntegrationEntityTypeCustomer,
				ProviderTypes: []string{string(types.SecretProviderPaddle)},
			})
			if listErr == nil && len(existingMappings) > 0 {
				s.logger.Warnw("Paddle customer mapping already exists (concurrent creation), using existing mapping",
					"customer_id", flexpriceCustomer.ID,
					"existing_paddle_customer_id", existingMappings[0].ProviderEntityID,
					"discarded_paddle_customer_id", paddleCustomerID)
				return existingMappings[0].ProviderEntityID, nil
			}
		}
		s.logger.Errorw("failed to store Paddle customer mapping",
			"error", err,
			"customer_id", flexpriceCustomer.ID,
			"paddle_customer_id", paddleCustomerID)
		// Don't fail for non-conflict errors: the Paddle customer was created successfully and the
		// caller will write paddle_customer_id to customer metadata, which acts as the idempotency
		// guard for future retries (EnsureCustomerSyncedToPaddle checks metadata first).
	} else {
		s.logger.Infow("stored Paddle customer mapping",
			"customer_id", flexpriceCustomer.ID,
			"paddle_customer_id", paddleCustomerID)
	}

	return paddleCustomerID, nil
}

// GetPaddleCustomerID retrieves the Paddle customer ID for a FlexPrice customer
func (s *CustomerService) GetPaddleCustomerID(ctx context.Context, customerID string) (string, error) {
	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      customerID,
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to get Paddle customer mapping").
			Mark(ierr.ErrSystem)
	}

	if len(mappings) == 0 {
		return "", ierr.NewError("customer not found in Paddle").
			WithHint("Customer has not been synced to Paddle").
			Mark(ierr.ErrNotFound)
	}

	return mappings[0].ProviderEntityID, nil
}

// CreateCustomerFromPaddle creates a FlexPrice customer from Paddle webhook data (customer.created).
func (s *CustomerService) CreateCustomerFromPaddle(ctx context.Context, paddleCustomer *paddlenotification.CustomerNotification, customerService interfaces.CustomerService) error {
	paddleCustomerID := paddleCustomer.ID

	// Idempotency: check if mapping already exists
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderPaddle)},
		ProviderEntityIDs: []string{paddleCustomerID},
		EntityType:        types.IntegrationEntityTypeCustomer,
	}
	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		s.logger.Errorw("failed to check Paddle customer mapping",
			"error", err,
			"paddle_customer_id", paddleCustomerID)
		return err
	}
	if len(mappings) > 0 {
		s.logger.Infow("FlexPrice customer already exists for Paddle customer, skipping creation",
			"flexprice_customer_id", mappings[0].EntityID,
			"paddle_customer_id", paddleCustomerID)
		return nil
	}

	// Deduplication by email: if customer exists by email, create mapping and skip creation
	if paddleCustomer.Email != "" {
		emailFilter := &types.CustomerFilter{
			Email:       paddleCustomer.Email,
			QueryFilter: types.NewDefaultQueryFilter(),
		}
		existingCustomers, err := customerService.GetCustomers(ctx, emailFilter)
		if err == nil && existingCustomers != nil && len(existingCustomers.Items) > 0 {
			existingCustomer := existingCustomers.Items[0]
			s.logger.Infow("customer with same email already exists, creating mapping",
				"customer_id", existingCustomer.ID,
				"paddle_customer_id", paddleCustomerID)

			mapping := &entityintegrationmapping.EntityIntegrationMapping{
				ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
				EntityID:         existingCustomer.ID,
				EntityType:       types.IntegrationEntityTypeCustomer,
				ProviderType:     string(types.SecretProviderPaddle),
				ProviderEntityID: paddleCustomerID,
				Metadata: map[string]interface{}{
					"created_via":           "provider_to_flexprice",
					"paddle_customer_email": paddleCustomer.Email,
					"synced_at":             time.Now().UTC().Format(time.RFC3339),
				},
				EnvironmentID: types.GetEnvironmentID(ctx),
				BaseModel:     types.GetDefaultBaseModel(ctx),
			}
			if err := s.entityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
				s.logger.Warnw("failed to create mapping for existing customer",
					"error", err,
					"customer_id", existingCustomer.ID,
					"paddle_customer_id", paddleCustomerID)
			}
			return nil
		}
	}

	// Create new customer
	name := paddleCustomerID
	if paddleCustomer.Name != nil && *paddleCustomer.Name != "" {
		name = *paddleCustomer.Name
	} else if paddleCustomer.Email != "" {
		name = paddleCustomer.Email
	}

	createReq := dto.CreateCustomerRequest{
		ExternalID:            paddleCustomerID,
		Name:                  name,
		Email:                 paddleCustomer.Email,
		SkipOnboardingWorkflow: true,
		Metadata: map[string]string{
			"paddle_customer_id": paddleCustomerID,
		},
	}

	customerResp, err := customerService.CreateCustomer(ctx, createReq)
	if err != nil {
		return err
	}

	// Create entity mapping
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         customerResp.ID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: paddleCustomerID,
		Metadata: map[string]interface{}{
			"created_via":           "provider_to_flexprice",
			"paddle_customer_email": paddleCustomer.Email,
			"synced_at":             time.Now().UTC().Format(time.RFC3339),
		},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	if err := s.entityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		s.logger.Warnw("failed to create mapping for new customer",
			"error", err,
			"customer_id", customerResp.ID,
			"paddle_customer_id", paddleCustomerID)
	}

	return nil
}

// mergeCustomerMetadata merges new metadata with existing customer metadata
func (s *CustomerService) mergeCustomerMetadata(existingMetadata map[string]string, newMetadata map[string]string) map[string]string {
	merged := make(map[string]string)

	for k, v := range existingMetadata {
		merged[k] = v
	}

	for k, v := range newMetadata {
		merged[k] = v
	}

	return merged
}
