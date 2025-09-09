package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stripe/stripe-go/v82"
)

// IntegrationService handles generic integration operations with multiple providers
type IntegrationService interface {
	// SyncEntityToProviders syncs an entity to all available providers for the tenant
	SyncEntityToProviders(ctx context.Context, entityType types.IntegrationEntityType, entityID string) error

	// SyncCustomerFromProvider syncs a customer from a specific provider to FlexPrice
	SyncCustomerFromProvider(ctx context.Context, providerType string, providerCustomerID string, customerData map[string]interface{}) error

	// GetAvailableProviders returns all available providers for the current tenant
	GetAvailableProviders(ctx context.Context) ([]*connection.Connection, error)

	// ValidateIntegrationEntityMappings validates that the provided integration entity mappings exist in their respective providers
	ValidateIntegrationEntityMappings(ctx context.Context, mappings []*dto.IntegrationEntityMapping) error

	// UpdateProviderCustomerMetadata updates the customer metadata in the external provider
	UpdateProviderCustomerMetadata(ctx context.Context, provider, providerCustomerID string, cust *customer.Customer) error
}

type integrationService struct {
	ServiceParams
}

func NewIntegrationService(params ServiceParams) IntegrationService {
	return &integrationService{
		ServiceParams: params,
	}
}

// SyncEntityToProviders syncs an entity to all available providers for the tenant
func (s *integrationService) SyncEntityToProviders(ctx context.Context, entityType types.IntegrationEntityType, entityID string) error {
	// Get all available connections for this tenant
	connections, err := s.getAvailableConnections(ctx)
	if err != nil {
		return err
	}

	if len(connections) == 0 {
		s.Logger.Infow("no integrations available for entity sync",
			"entity_type", entityType,
			"entity_id", entityID,
			"tenant_id", types.GetTenantID(ctx))
		return nil
	}

	// Only support customer sync for now
	if entityType != types.IntegrationEntityTypeCustomer {
		return ierr.NewError("unsupported entity type").
			WithHint(fmt.Sprintf("Entity type %s is not supported for sync", entityType)).
			Mark(ierr.ErrValidation)
	}

	return s.syncCustomerToProviders(ctx, entityID, connections)
}

// syncCustomerToProviders syncs a customer to all available providers for the tenant
func (s *integrationService) syncCustomerToProviders(ctx context.Context, customerID string, connections []*connection.Connection) error {
	// Get the customer
	customerService := NewCustomerService(s.ServiceParams)
	customerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return err
	}
	customer := customerResp.Customer

	// Sync to each provider synchronously (caller already runs this in a goroutine)
	for _, conn := range connections {
		if err := s.syncCustomerToProvider(ctx, customer, conn); err != nil {
			s.Logger.Errorw("failed to sync customer to provider",
				"customer_id", customerID,
				"provider_type", conn.ProviderType,
				"error", err)
			// Continue syncing other providers even if one fails
			continue
		}
		s.Logger.Infow("customer synced to provider successfully",
			"customer_id", customerID,
			"provider_type", conn.ProviderType)
	}

	return nil
}

// syncCustomerToProvider syncs a customer to a specific provider
func (s *integrationService) syncCustomerToProvider(ctx context.Context, customer *customer.Customer, conn *connection.Connection) error {
	// Use database transaction to prevent race conditions
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Check if mapping already exists for this customer_id, provider, tenant, and environment
		// This check is now within the transaction, preventing race conditions
		entityMappingService := NewEntityIntegrationMappingService(s.ServiceParams)

		// Use standard list/search pattern instead of specific endpoint
		filter := &types.EntityIntegrationMappingFilter{
			EntityID:      customer.ID,
			EntityType:    types.IntegrationEntityTypeCustomer,
			ProviderTypes: []string{string(conn.ProviderType)},
		}

		existingMappings, err := entityMappingService.GetEntityIntegrationMappings(txCtx, filter)
		if err == nil && existingMappings != nil && len(existingMappings.Items) > 0 {
			existingMapping := existingMappings.Items[0]
			// Mapping exists, customer already synced
			s.Logger.Infow("customer already mapped to provider",
				"customer_id", customer.ID,
				"provider_type", conn.ProviderType,
				"provider_entity_id", existingMapping.ProviderEntityID)
			return nil
		}

		// Sync based on provider type (API calls outside transaction to avoid long-running transactions)
		var providerEntityID string
		var metadata map[string]interface{}

		switch conn.ProviderType {
		case types.SecretProviderStripe:
			providerEntityID, metadata, err = s.syncCustomerToStripe(ctx, customer, conn)
		// Add more providers as needed
		default:
			return ierr.NewError("unsupported provider type").
				WithHint(fmt.Sprintf("Provider type %s is not supported", conn.ProviderType)).
				Mark(ierr.ErrValidation)
		}

		if err != nil {
			return err
		}

		// Create entity mapping (within transaction)
		mappingReq := dto.CreateEntityIntegrationMappingRequest{
			EntityID:         customer.ID,
			EntityType:       types.IntegrationEntityTypeCustomer,
			ProviderType:     string(conn.ProviderType),
			ProviderEntityID: providerEntityID,
			Metadata:         metadata,
		}

		_, err = entityMappingService.CreateEntityIntegrationMapping(txCtx, mappingReq)
		if err != nil {
			s.Logger.Errorw("failed to create entity mapping",
				"customer_id", customer.ID,
				"provider_type", conn.ProviderType,
				"provider_entity_id", providerEntityID,
				"error", err)
			return err
		}

		// Update customer metadata with provider ID (within transaction)
		updateReq := dto.UpdateCustomerRequest{
			Metadata: map[string]string{
				fmt.Sprintf("%s_customer_id", conn.ProviderType): providerEntityID,
			},
		}

		// Merge with existing metadata
		if customer.Metadata != nil {
			for k, v := range customer.Metadata {
				updateReq.Metadata[k] = v
			}
		}

		customerService := NewCustomerService(s.ServiceParams)
		_, err = customerService.UpdateCustomer(txCtx, customer.ID, updateReq)
		if err != nil {
			s.Logger.Errorw("failed to update customer metadata",
				"customer_id", customer.ID,
				"provider_type", conn.ProviderType,
				"error", err)
			return err
		}

		s.Logger.Infow("customer synced to provider successfully",
			"customer_id", customer.ID,
			"provider_type", conn.ProviderType,
			"provider_entity_id", providerEntityID)

		return nil
	})
}

// syncCustomerToStripe syncs a customer to Stripe
func (s *integrationService) syncCustomerToStripe(ctx context.Context, customer *customer.Customer, conn *connection.Connection) (string, map[string]interface{}, error) {
	stripeService := NewStripeService(s.ServiceParams)

	// Get Stripe configuration
	stripeConfig, err := stripeService.GetDecryptedStripeConfig(conn)
	if err != nil {
		return "", nil, err
	}

	// Check if customer with same email already exists in Stripe
	if customer.Email != "" {
		existingStripeCustomer, err := s.findStripeCustomerByEmail(ctx, stripeConfig.SecretKey, customer.Email)
		if err == nil && existingStripeCustomer != nil {
			s.Logger.Infow("customer with same email already exists in Stripe",
				"customer_id", customer.ID,
				"email", customer.Email,
				"stripe_customer_id", existingStripeCustomer.ID)
			return existingStripeCustomer.ID, map[string]interface{}{
				"stripe_customer_email": customer.Email,
				"sync_direction":        "flexprice_to_provider",
				"created_via":           "api",
				"found_existing":        true,
			}, nil
		}
	}

	// Create customer in Stripe
	if err := stripeService.CreateCustomerInStripe(ctx, customer.ID); err != nil {
		return "", nil, err
	}

	// Get updated customer to get the Stripe ID
	customerService := NewCustomerService(s.ServiceParams)
	customerResp, err := customerService.GetCustomer(ctx, customer.ID)
	if err != nil {
		return "", nil, err
	}

	stripeID := customerResp.Customer.Metadata["stripe_customer_id"]
	if stripeID == "" {
		return "", nil, ierr.NewError("failed to get Stripe customer ID").
			WithHint("Stripe customer ID not found in metadata").
			Mark(ierr.ErrInternal)
	}

	return stripeID, map[string]interface{}{
		"stripe_customer_email": customer.Email,
		"stripe_customer_name":  customer.Name,
		"sync_direction":        "flexprice_to_provider",
		"created_via":           "api",
		"synced_at":             time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// SyncCustomerFromProvider syncs a customer from a specific provider to FlexPrice
func (s *integrationService) SyncCustomerFromProvider(ctx context.Context, providerType string, providerCustomerID string, customerData map[string]interface{}) error {
	// Use database transaction to prevent race conditions
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Check if mapping already exists (within transaction)
		entityMappingService := NewEntityIntegrationMappingService(s.ServiceParams)

		// Use standard list/search pattern instead of specific endpoint
		filter := &types.EntityIntegrationMappingFilter{
			ProviderTypes:     []string{providerType},
			ProviderEntityIDs: []string{providerCustomerID},
		}

		existingMappings, err := entityMappingService.GetEntityIntegrationMappings(txCtx, filter)
		if err == nil && existingMappings != nil && len(existingMappings.Items) > 0 {
			existingMapping := existingMappings.Items[0]
			// Mapping exists, customer already synced
			s.Logger.Infow("customer already exists from provider",
				"provider_type", providerType,
				"provider_customer_id", providerCustomerID,
				"flexprice_customer_id", existingMapping.EntityID)
			return nil
		}

		// Check for existing customer by email in FlexPrice (within transaction)
		if email, exists := customerData["email"].(string); exists && email != "" {
			customerService := NewCustomerService(s.ServiceParams)
			existingCustomer, err := s.findCustomerByEmail(txCtx, email)
			if err == nil && existingCustomer != nil {
				// Customer with same email exists, update with provider ID
				s.Logger.Infow("customer with same email already exists in FlexPrice",
					"email", email,
					"flexprice_customer_id", existingCustomer.ID,
					"provider_type", providerType,
					"provider_customer_id", providerCustomerID)

				// Update existing customer with provider ID (within transaction)
				updateReq := dto.UpdateCustomerRequest{
					Metadata: map[string]string{
						fmt.Sprintf("%s_customer_id", providerType): providerCustomerID,
					},
				}

				// Merge with existing metadata
				if existingCustomer.Metadata != nil {
					for k, v := range existingCustomer.Metadata {
						updateReq.Metadata[k] = v
					}
				}

				_, err = customerService.UpdateCustomer(txCtx, existingCustomer.ID, updateReq)
				if err != nil {
					return err
				}

				// Create entity mapping for existing customer (within transaction)
				mappingReq := dto.CreateEntityIntegrationMappingRequest{
					EntityID:         existingCustomer.ID,
					EntityType:       types.IntegrationEntityTypeCustomer,
					ProviderType:     providerType,
					ProviderEntityID: providerCustomerID,
					Metadata: map[string]interface{}{
						"sync_direction": "provider_to_flexprice",
						"created_via":    "webhook",
						"found_existing": true,
					},
				}

				_, err = entityMappingService.CreateEntityIntegrationMapping(txCtx, mappingReq)
				if err != nil {
					s.Logger.Errorw("failed to create entity mapping for existing customer",
						"customer_id", existingCustomer.ID,
						"provider_type", providerType,
						"provider_customer_id", providerCustomerID,
						"error", err)
					return err
				}

				s.Logger.Infow("existing customer updated with provider mapping",
					"customer_id", existingCustomer.ID,
					"provider_type", providerType,
					"provider_customer_id", providerCustomerID)

				return nil
			}
		}

		// Create customer based on provider type (outside transaction for API calls)
		var customerID string
		var metadata map[string]interface{}

		switch providerType {
		case string(types.SecretProviderStripe):
			customerID, metadata, err = s.createCustomerFromStripe(ctx, providerCustomerID, customerData)
		default:
			return ierr.NewError("unsupported provider type").
				WithHint(fmt.Sprintf("Provider type %s is not supported", providerType)).
				Mark(ierr.ErrValidation)
		}

		if err != nil {
			return err
		}

		// Create entity mapping (within transaction)
		mappingReq := dto.CreateEntityIntegrationMappingRequest{
			EntityID:         customerID,
			EntityType:       types.IntegrationEntityTypeCustomer,
			ProviderType:     providerType,
			ProviderEntityID: providerCustomerID,
			Metadata:         metadata,
		}

		_, err = entityMappingService.CreateEntityIntegrationMapping(txCtx, mappingReq)
		if err != nil {
			s.Logger.Errorw("failed to create entity mapping from provider",
				"customer_id", customerID,
				"provider_type", providerType,
				"provider_customer_id", providerCustomerID,
				"error", err)
			return err
		}

		s.Logger.Infow("customer created from provider successfully",
			"customer_id", customerID,
			"provider_type", providerType,
			"provider_customer_id", providerCustomerID)

		return nil
	})
}

// createCustomerFromStripe creates a customer in FlexPrice from Stripe webhook data
func (s *integrationService) createCustomerFromStripe(ctx context.Context, stripeCustomerID string, customerData map[string]interface{}) (string, map[string]interface{}, error) {
	// Convert customerData to Stripe customer format for the Stripe service
	stripeCustomer := &stripe.Customer{
		ID:       stripeCustomerID,
		Name:     customerData["name"].(string),
		Email:    customerData["email"].(string),
		Metadata: map[string]string{},
	}

	// Add flexprice_customer_id to metadata if it exists
	if flexpriceID, exists := customerData["flexprice_customer_id"]; exists && flexpriceID != nil {
		if flexpriceIDStr, ok := flexpriceID.(string); ok {
			stripeCustomer.Metadata["flexprice_customer_id"] = flexpriceIDStr
		}
	}

	// Add address if available
	if address, exists := customerData["address"]; exists && address != nil {
		if addrMap, ok := address.(map[string]interface{}); ok {
			stripeCustomer.Address = &stripe.Address{}

			if line1, exists := addrMap["line1"]; exists && line1 != nil {
				if line1Str, ok := line1.(string); ok {
					stripeCustomer.Address.Line1 = line1Str
				}
			}
			if line2, exists := addrMap["line2"]; exists && line2 != nil {
				if line2Str, ok := line2.(string); ok {
					stripeCustomer.Address.Line2 = line2Str
				}
			}
			if city, exists := addrMap["city"]; exists && city != nil {
				if cityStr, ok := city.(string); ok {
					stripeCustomer.Address.City = cityStr
				}
			}
			if state, exists := addrMap["state"]; exists && state != nil {
				if stateStr, ok := state.(string); ok {
					stripeCustomer.Address.State = stateStr
				}
			}
			if postalCode, exists := addrMap["postal_code"]; exists && postalCode != nil {
				if postalCodeStr, ok := postalCode.(string); ok {
					stripeCustomer.Address.PostalCode = postalCodeStr
				}
			}
			if country, exists := addrMap["country"]; exists && country != nil {
				if countryStr, ok := country.(string); ok {
					stripeCustomer.Address.Country = countryStr
				}
			}
		}
	}

	// Use the Stripe service to create customer
	stripeService := NewStripeService(s.ServiceParams)
	environmentID := types.GetEnvironmentID(ctx)
	if err := stripeService.CreateCustomerFromStripe(ctx, stripeCustomer, environmentID); err != nil {
		return "", nil, err
	}

	// Get the created customer by email (much more efficient than JSON metadata search)
	customerService := NewCustomerService(s.ServiceParams)
	email := customerData["email"].(string)
	filter := &types.CustomerFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		Email:       email,
	}

	customers, err := customerService.GetCustomers(ctx, filter)
	if err != nil {
		return "", nil, err
	}

	// Should find exactly one customer with this email
	if len(customers.Items) == 0 {
		return "", nil, ierr.NewError("failed to find created customer").
			WithHint("Customer was created but could not be found by email").
			Mark(ierr.ErrInternal)
	}

	if len(customers.Items) > 1 {
		s.Logger.Warnw("multiple customers found with same email",
			"email", email,
			"count", len(customers.Items))
	}

	createdCustomer := customers.Items[0].Customer

	return createdCustomer.ID, map[string]interface{}{
		"stripe_customer_email": customerData["email"].(string),
		"stripe_customer_name":  customerData["name"].(string),
		"sync_direction":        "provider_to_flexprice",
		"created_via":           "webhook",
		"webhook_event":         "customer.created",
		"synced_at":             time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// getAvailableConnections gets all available connections for the current tenant
func (s *integrationService) getAvailableConnections(ctx context.Context) ([]*connection.Connection, error) {
	if s.ConnectionRepo == nil {
		return nil, nil
	}

	filter := types.NewConnectionFilter()
	filter.Limit = lo.ToPtr(100) // Get up to 100 connections

	connections, err := s.ConnectionRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Filter out non-provider connections (like flexprice)
	var providerConnections []*connection.Connection
	for _, conn := range connections {
		if conn.ProviderType != types.SecretProviderFlexPrice {
			providerConnections = append(providerConnections, conn)
		}
	}

	return providerConnections, nil
}

// GetAvailableProviders returns all available providers for the current tenant
func (s *integrationService) GetAvailableProviders(ctx context.Context) ([]*connection.Connection, error) {
	return s.getAvailableConnections(ctx)
}

// findStripeCustomerByEmail finds a customer in Stripe by email
func (s *integrationService) findStripeCustomerByEmail(ctx context.Context, secretKey, email string) (*stripe.Customer, error) {
	// For now, we'll skip this check as it requires additional Stripe API calls
	// In a production environment, you might want to implement this using Stripe's search API
	// or maintain a local cache of email to customer ID mappings

	// TODO: Implement proper Stripe customer search by email
	// This would require using Stripe's search API or customer list API
	// For now, we'll return nil to indicate no existing customer found
	return nil, nil
}

// findCustomerByEmail finds a customer by email in FlexPrice
func (s *integrationService) findCustomerByEmail(ctx context.Context, email string) (*customer.Customer, error) {
	customerService := NewCustomerService(s.ServiceParams)

	// Use customer filter to search by email - optimized with database filtering
	filter := &types.CustomerFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		Email:       email, // Use database filtering instead of in-memory search
	}

	customers, err := customerService.GetCustomers(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Return first matching customer (should be only one due to email uniqueness)
	if len(customers.Items) > 0 {
		return customers.Items[0].Customer, nil
	}

	return nil, nil // No customer found
}

// ValidateIntegrationEntityMappings validates that the provided integration entity mappings exist in their respective providers
func (s *integrationService) ValidateIntegrationEntityMappings(ctx context.Context, mappings []*dto.IntegrationEntityMapping) error {
	if len(mappings) == 0 {
		return nil
	}

	// Get all available connections for this tenant
	connections, err := s.getAvailableConnections(ctx)
	if err != nil {
		return err
	}

	// Create a map of provider types to connections for quick lookup
	providerConnections := make(map[string]*connection.Connection)
	for _, conn := range connections {
		providerConnections[string(conn.ProviderType)] = conn
	}

	// Validate each mapping
	for _, mapping := range mappings {
		conn, exists := providerConnections[mapping.Provider]
		if !exists {
			return ierr.NewError("provider not configured").
				WithHint(fmt.Sprintf("Provider %s is not configured for this environment", mapping.Provider)).
				WithReportableDetails(map[string]interface{}{
					"provider": mapping.Provider,
				}).
				Mark(ierr.ErrValidation)
		}

		// Check if the provider entity ID already exists in our system
		filter := &types.EntityIntegrationMappingFilter{
			ProviderTypes:     []string{mapping.Provider},
			ProviderEntityIDs: []string{mapping.ID},
		}
		existingMappings, err := s.EntityIntegrationMappingRepo.List(ctx, filter)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to check for existing provider entity mapping").
				Mark(ierr.ErrInternal)
		}

		// If any mapping exists, return error with details
		if len(existingMappings) > 0 {
			existingMapping := existingMappings[0]
			return ierr.NewError("provider entity mapping already exists").
				WithHint(fmt.Sprintf("The provider entity ID '%s' for provider '%s' is already mapped to another entity", mapping.ID, mapping.Provider)).
				WithReportableDetails(map[string]interface{}{
					"provider":             mapping.Provider,
					"provider_entity_id":   mapping.ID,
					"existing_entity_id":   existingMapping.EntityID,
					"existing_entity_type": existingMapping.EntityType,
					"existing_mapping_id":  existingMapping.ID,
					"existing_environment": existingMapping.EnvironmentID,
					"existing_tenant":      existingMapping.TenantID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}

		// Validate based on provider type
		switch mapping.Provider {
		case "stripe":
			if err := s.validateStripeCustomer(ctx, conn, mapping.ID); err != nil {
				return err
			}
		case "razorpay":
			// TODO: Implement Razorpay validation when needed
			return ierr.NewError("razorpay validation not implemented").
				WithHint("Razorpay customer validation is not yet implemented").
				Mark(ierr.ErrNotFound)
		default:
			return ierr.NewError("unsupported provider").
				WithHint(fmt.Sprintf("Provider %s is not supported", mapping.Provider)).
				WithReportableDetails(map[string]interface{}{
					"provider": mapping.Provider,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// validateStripeCustomer validates that a customer exists in Stripe
func (s *integrationService) validateStripeCustomer(ctx context.Context, conn *connection.Connection, customerID string) error {
	stripeService := NewStripeService(s.ServiceParams)

	// Get Stripe configuration
	stripeConfig, err := stripeService.GetDecryptedStripeConfig(conn)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get Stripe configuration").
			Mark(ierr.ErrInternal)
	}

	// Initialize Stripe client with the secret key
	// Use dedicated client instance to avoid race conditions
	sc := stripe.NewClient(stripeConfig.SecretKey, nil)

	// Validate that the customer exists in Stripe
	cust, err := sc.V1Customers.Retrieve(context.Background(), customerID, nil)
	if err != nil {
		s.Logger.Errorw("failed to validate Stripe customer",
			"customer_id", customerID,
			"error", err)

		return ierr.WithError(err).
			WithHint(fmt.Sprintf("Customer with ID %s does not exist in Stripe or is not accessible", customerID)).
			WithReportableDetails(map[string]interface{}{
				"stripe_customer_id": customerID,
			}).
			Mark(ierr.ErrNotFound)
	}

	if cust == nil {
		return ierr.NewError("stripe customer not found").
			WithHint(fmt.Sprintf("Customer with ID %s was not found in Stripe", customerID)).
			WithReportableDetails(map[string]interface{}{
				"stripe_customer_id": customerID,
			}).
			Mark(ierr.ErrNotFound)
	}

	s.Logger.Infow("stripe customer validation successful",
		"customer_id", customerID,
		"stripe_customer_email", cust.Email)

	return nil
}

// UpdateProviderCustomerMetadata updates the customer metadata in the external provider
func (s *integrationService) UpdateProviderCustomerMetadata(ctx context.Context, provider, providerCustomerID string, cust *customer.Customer) error {
	switch provider {
	case "stripe":
		stripeService := NewStripeService(s.ServiceParams)
		return stripeService.UpdateStripeCustomerMetadata(ctx, providerCustomerID, cust)
	case "razorpay":
		// TODO: Implement Razorpay metadata update when needed
		s.Logger.Infow("razorpay metadata update not implemented", "provider_customer_id", providerCustomerID)
		return nil
	default:
		s.Logger.Infow("metadata update not supported for provider", "provider", provider)
		return nil
	}
}
