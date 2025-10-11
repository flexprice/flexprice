package stripe

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
	"github.com/stripe/stripe-go/v82"
)

// CustomerService handles Stripe customer operations
type CustomerService struct {
	client                       *Client
	customerRepo                 customer.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewCustomerService creates a new Stripe customer service
func NewCustomerService(
	client *Client,
	customerRepo customer.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) *CustomerService {
	return &CustomerService{
		client:                       client,
		customerRepo:                 customerRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// EnsureCustomerSyncedToStripe checks if customer is synced to Stripe and syncs if needed
func (s *CustomerService) EnsureCustomerSyncedToStripe(ctx context.Context, customerID string, customerService interfaces.CustomerService) (*dto.CustomerResponse, error) {
	// Get our customer
	ourCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	ourCustomer := ourCustomerResp.Customer

	// Check if customer already has Stripe ID in metadata
	if stripeID, exists := ourCustomer.Metadata["stripe_customer_id"]; exists && stripeID != "" {
		s.logger.Infow("customer already synced to Stripe",
			"customer_id", customerID,
			"stripe_customer_id", stripeID)
		return ourCustomerResp, nil
	}

	// Check if customer is synced via integration mapping table
	if s.entityIntegrationMappingRepo != nil {
		filter := &types.EntityIntegrationMappingFilter{
			EntityID:      customerID,
			EntityType:    types.IntegrationEntityTypeCustomer,
			ProviderTypes: []string{string(types.SecretProviderStripe)},
		}

		existingMappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
		if err == nil && existingMappings != nil && len(existingMappings) > 0 {
			existingMapping := existingMappings[0]
			s.logger.Infow("customer already mapped to Stripe via integration mapping",
				"customer_id", customerID,
				"stripe_customer_id", existingMapping.ProviderEntityID)

			// Update customer metadata with Stripe ID for faster future lookups
			updateReq := dto.UpdateCustomerRequest{
				Metadata: s.mergeCustomerMetadata(ourCustomer.Metadata, map[string]string{
					"stripe_customer_id": existingMapping.ProviderEntityID,
				}),
			}
			updatedCustomerResp, err := customerService.UpdateCustomer(ctx, ourCustomer.ID, updateReq)
			if err != nil {
				s.logger.Warnw("failed to update customer metadata with Stripe ID",
					"customer_id", customerID,
					"error", err)
				// Return original customer info if update fails
				return ourCustomerResp, nil
			}
			return updatedCustomerResp, nil
		}
	}

	// Customer is not synced, create in Stripe
	s.logger.Infow("customer not synced to Stripe, creating in Stripe",
		"customer_id", customerID)
	err = s.CreateCustomerInStripe(ctx, customerID, customerService)
	if err != nil {
		return nil, err
	}

	// Get updated customer after sync
	updatedCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}

	return updatedCustomerResp, nil
}

// CreateCustomerInStripe creates a customer in Stripe and updates our customer with Stripe ID
func (s *CustomerService) CreateCustomerInStripe(ctx context.Context, customerID string, customerService interfaces.CustomerService) error {
	// Get our customer
	ourCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return err
	}
	ourCustomer := ourCustomerResp.Customer

	// Get Stripe client and config
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return err
	}

	// Check if customer already has Stripe ID
	if stripeID, exists := ourCustomer.Metadata["stripe_customer_id"]; exists && stripeID != "" {
		return ierr.NewError("customer already has Stripe ID").
			WithHint("Customer is already synced with Stripe").
			Mark(ierr.ErrAlreadyExists)
	}

	// Create customer in Stripe
	params := &stripe.CustomerCreateParams{
		Name:  stripe.String(ourCustomer.Name),
		Email: stripe.String(ourCustomer.Email),
		Metadata: map[string]string{
			"flexprice_customer_id": ourCustomer.ID,
			"flexprice_environment": ourCustomer.EnvironmentID,
			"external_id":           ourCustomer.ExternalID,
		},
	}

	// Add address if available
	if ourCustomer.AddressLine1 != "" || ourCustomer.AddressCity != "" {
		params.Address = &stripe.AddressParams{
			Line1:      stripe.String(ourCustomer.AddressLine1),
			Line2:      stripe.String(ourCustomer.AddressLine2),
			City:       stripe.String(ourCustomer.AddressCity),
			State:      stripe.String(ourCustomer.AddressState),
			PostalCode: stripe.String(ourCustomer.AddressPostalCode),
			Country:    stripe.String(ourCustomer.AddressCountry),
		}
	}

	stripeCustomer, err := stripeClient.V1Customers.Create(ctx, params)
	if err != nil {
		return ierr.NewError("failed to create customer in Stripe").
			WithHint("Stripe API error").
			Mark(ierr.ErrHTTPClient)
	}

	// Update our customer with Stripe ID
	updateReq := dto.UpdateCustomerRequest{
		Metadata: s.mergeCustomerMetadata(ourCustomer.Metadata, map[string]string{
			"stripe_customer_id": stripeCustomer.ID,
		}),
	}

	_, err = customerService.UpdateCustomer(ctx, ourCustomer.ID, updateReq)
	if err != nil {
		return err
	}

	// Create entity mapping if repository is available
	if s.entityIntegrationMappingRepo != nil {
		mapping := &entityintegrationmapping.EntityIntegrationMapping{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
			EntityID:         ourCustomer.ID,
			EntityType:       types.IntegrationEntityTypeCustomer,
			ProviderType:     string(types.SecretProviderStripe),
			ProviderEntityID: stripeCustomer.ID,
			Metadata: map[string]interface{}{
				"created_via":           "flexprice_to_provider",
				"stripe_customer_email": ourCustomer.Email,
				"stripe_customer_name":  ourCustomer.Name,
				"synced_at":             time.Now().UTC().Format(time.RFC3339),
			},
			EnvironmentID: types.GetEnvironmentID(ctx),
			BaseModel:     types.GetDefaultBaseModel(ctx),
		}

		err = s.entityIntegrationMappingRepo.Create(ctx, mapping)
		if err != nil {
			s.logger.Warnw("failed to create entity mapping for customer",
				"error", err,
				"customer_id", ourCustomer.ID,
				"stripe_customer_id", stripeCustomer.ID)
			// Don't fail the entire operation if entity mapping creation fails
		}
	}

	return nil
}

// CreateCustomerFromStripe creates a customer in our system from Stripe webhook data
func (s *CustomerService) CreateCustomerFromStripe(ctx context.Context, stripeCustomer *stripe.Customer, environmentID string, customerService interfaces.CustomerService) error {
	externalID := stripeCustomer.ID

	// Step 1: Check by flexprice_customer_id, if exists just return
	if flexpriceID, exists := stripeCustomer.Metadata["flexprice_customer_id"]; exists && flexpriceID != "" {
		existing, err := customerService.GetCustomer(ctx, flexpriceID)
		if err == nil && existing != nil {
			s.logger.Infow("FlexPrice customer already exists, skipping creation",
				"flexprice_customer_id", flexpriceID,
				"stripe_customer_id", stripeCustomer.ID)
			return nil
		}
	}

	// Step 2: Check by flexprice_lookup_key
	if lookupKey, exists := stripeCustomer.Metadata["flexprice_lookup_key"]; exists && lookupKey != "" {
		externalID = lookupKey
		existing, err := customerService.GetCustomerByLookupKey(ctx, lookupKey)
		if err == nil && existing != nil {
			// Customer found, check for existing mapping
			filter := &types.EntityIntegrationMappingFilter{
				EntityID:          existing.Customer.ID,
				EntityType:        types.IntegrationEntityTypeCustomer,
				ProviderTypes:     []string{string(types.SecretProviderStripe)},
				ProviderEntityIDs: []string{stripeCustomer.ID},
			}

			existingMapping, err := s.entityIntegrationMappingRepo.List(ctx, filter)
			if err == nil && len(existingMapping) > 0 {
				// Mapping exists, just return
				s.logger.Infow("FlexPrice customer and mapping already exist, skipping creation",
					"flexprice_customer_id", existing.Customer.ID,
					"stripe_customer_id", stripeCustomer.ID)
				return nil
			}

			// Customer exists but no mapping, create mapping
			err = s.createEntityIntegrationMapping(ctx, existing.Customer.ID, stripeCustomer)
			if err != nil {
				s.logger.Warnw("failed to create mapping for existing customer",
					"error", err,
					"customer_id", existing.Customer.ID,
					"stripe_customer_id", stripeCustomer.ID)
			}

			return nil
		}
	}

	// Step 3: Create new customer
	createReq := dto.CreateCustomerRequest{
		ExternalID: externalID,
		Name:       stripeCustomer.Name,
		Email:      stripeCustomer.Email,
		Metadata: map[string]string{
			"stripe_customer_id": stripeCustomer.ID,
		},
	}

	// Add address if available
	if stripeCustomer.Address != nil {
		createReq.AddressLine1 = stripeCustomer.Address.Line1
		createReq.AddressLine2 = stripeCustomer.Address.Line2
		createReq.AddressCity = stripeCustomer.Address.City
		createReq.AddressState = stripeCustomer.Address.State
		createReq.AddressPostalCode = stripeCustomer.Address.PostalCode
		createReq.AddressCountry = stripeCustomer.Address.Country
	}

	customerResp, err := customerService.CreateCustomer(ctx, createReq)
	if err != nil {
		return err
	}

	// Create entity mapping for new customer
	err = s.createEntityIntegrationMapping(ctx, customerResp.ID, stripeCustomer)
	if err != nil {
		s.logger.Warnw("failed to create mapping for new customer",
			"error", err,
			"customer_id", customerResp.ID,
			"stripe_customer_id", stripeCustomer.ID)
	}

	return nil
}

// GetDefaultPaymentMethod gets the default payment method for a customer
func (s *CustomerService) GetDefaultPaymentMethod(ctx context.Context, customerID string, customerService interfaces.CustomerService) (*dto.PaymentMethodResponse, error) {
	// Get Stripe client
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// Get our customer to find Stripe customer ID
	ourCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	ourCustomer := ourCustomerResp.Customer

	stripeCustomerID := ""
	if ourCustomer.Metadata != nil {
		stripeCustomerID = ourCustomer.Metadata["stripe_customer_id"]
	}
	if stripeCustomerID == "" && s.entityIntegrationMappingRepo != nil {
		filter := &types.EntityIntegrationMappingFilter{
			EntityID:      customerID,
			EntityType:    types.IntegrationEntityTypeCustomer,
			ProviderTypes: []string{string(types.SecretProviderStripe)},
		}
		mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
		if err == nil && len(mappings) > 0 {
			stripeCustomerID = mappings[0].ProviderEntityID
			updateReq := dto.UpdateCustomerRequest{
				Metadata: s.mergeCustomerMetadata(
					ourCustomer.Metadata,
					map[string]string{"stripe_customer_id": stripeCustomerID},
				),
			}
			if _, err := customerService.UpdateCustomer(ctx, ourCustomer.ID, updateReq); err != nil {
				s.logger.Warnw("failed to backfill stripe_customer_id metadata",
					"customer_id", customerID,
					"error", err)
			} else {
				ourCustomer.Metadata = updateReq.Metadata
			}
		}
	}
	if stripeCustomerID == "" {
		return nil, ierr.NewError("customer not found in Stripe").
			WithHint("Customer must have a Stripe account").
			Mark(ierr.ErrNotFound)
	}

	// Get customer from Stripe to find default payment method
	customer, err := stripeClient.V1Customers.Retrieve(ctx, stripeCustomerID, nil)
	if err != nil {
		s.logger.Errorw("failed to get customer from Stripe",
			"error", err,
			"customer_id", customerID,
			"stripe_customer_id", stripeCustomerID,
		)
		return nil, ierr.NewError("failed to get customer from Stripe").
			WithHint("Could not retrieve customer information from Stripe").
			Mark(ierr.ErrSystem)
	}

	// Check if customer has a default payment method
	if customer.InvoiceSettings == nil || customer.InvoiceSettings.DefaultPaymentMethod == nil {
		return nil, ierr.NewError("no default payment method").
			WithHint("Customer does not have a default payment method set in Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrNotFound)
	}

	defaultPaymentMethodID := customer.InvoiceSettings.DefaultPaymentMethod.ID

	// Get the payment method details
	paymentMethod, err := stripeClient.V1PaymentMethods.Retrieve(ctx, defaultPaymentMethodID, nil)
	if err != nil {
		s.logger.Errorw("failed to get default payment method from Stripe",
			"error", err,
			"customer_id", customerID,
			"payment_method_id", defaultPaymentMethodID,
		)
		return nil, ierr.NewError("failed to get payment method").
			WithHint("Could not retrieve payment method details from Stripe").
			Mark(ierr.ErrSystem)
	}

	// Convert to our DTO format
	response := &dto.PaymentMethodResponse{
		ID:       paymentMethod.ID,
		Type:     string(paymentMethod.Type),
		Customer: paymentMethod.Customer.ID,
		Created:  paymentMethod.Created,
		Metadata: make(map[string]interface{}),
	}

	// Convert metadata
	for k, v := range paymentMethod.Metadata {
		response.Metadata[k] = v
	}

	// Add card details if it's a card
	if paymentMethod.Type == stripe.PaymentMethodTypeCard && paymentMethod.Card != nil {
		response.Card = &dto.CardDetails{
			Brand:       string(paymentMethod.Card.Brand),
			Last4:       paymentMethod.Card.Last4,
			ExpMonth:    int(paymentMethod.Card.ExpMonth),
			ExpYear:     int(paymentMethod.Card.ExpYear),
			Fingerprint: paymentMethod.Card.Fingerprint,
		}
	}

	s.logger.Infow("successfully retrieved default payment method",
		"customer_id", customerID,
		"stripe_customer_id", stripeCustomerID,
		"payment_method_id", defaultPaymentMethodID,
	)

	return response, nil
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

// createEntityIntegrationMapping creates an entity integration mapping for a customer
func (s *CustomerService) createEntityIntegrationMapping(ctx context.Context, customerID string, stripeCustomer *stripe.Customer) error {
	if s.entityIntegrationMappingRepo == nil {
		return nil
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         customerID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderStripe),
		ProviderEntityID: stripeCustomer.ID,
		Metadata: map[string]interface{}{
			"created_via":           "provider_to_flexprice",
			"stripe_customer_email": stripeCustomer.Email,
			"stripe_customer_name":  stripeCustomer.Name,
			"synced_at":             time.Now().UTC().Format(time.RFC3339),
		},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	err := s.entityIntegrationMappingRepo.Create(ctx, mapping)
	if err != nil {
		s.logger.Warnw("failed to create entity mapping for customer",
			"error", err,
			"customer_id", customerID,
			"stripe_customer_id", stripeCustomer.ID)
		return err
	}

	s.logger.Infow("Created entity integration mapping",
		"flexprice_customer_id", customerID,
		"stripe_customer_id", stripeCustomer.ID)

	return nil
}

// findStripeCustomerByEmail finds a Stripe customer by email
func (s *CustomerService) findStripeCustomerByEmail(ctx context.Context, email string) (*stripe.Customer, error) {
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// Search for customer by email
	params := &stripe.CustomerSearchParams{}
	params.Query = "email:'" + email + "'"
	params.Limit = stripe.Int64(1)

	iter := stripeClient.V1Customers.Search(ctx, params)
	for customer, err := range iter {
		if err != nil {
			return nil, err
		}
		return customer, nil
	}

	return nil, ierr.NewError("customer not found").Mark(ierr.ErrNotFound)
}

// SyncCustomerToStripe syncs a customer to Stripe
func (s *CustomerService) SyncCustomerToStripe(ctx context.Context, customer *customer.Customer, customerService interfaces.CustomerService) (string, map[string]interface{}, error) {
	// Check if customer with same email already exists in Stripe
	if customer.Email != "" {
		existingStripeCustomer, err := s.findStripeCustomerByEmail(ctx, customer.Email)
		if err == nil && existingStripeCustomer != nil {
			s.logger.Infow("customer with same email already exists in Stripe",
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
	if err := s.CreateCustomerInStripe(ctx, customer.ID, customerService); err != nil {
		return "", nil, err
	}

	// Get updated customer to get the Stripe ID
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

// UpdateStripeCustomerMetadata updates the Stripe customer metadata with FlexPrice information
func (s *CustomerService) UpdateStripeCustomerMetadata(ctx context.Context, stripeCustomerID string, cust *customer.Customer) error {
	// Get Stripe client
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return err
	}

	// Create update parameters
	params := &stripe.CustomerUpdateParams{}
	params.AddMetadata("flexprice_customer_id", cust.ID)
	params.AddMetadata("flexprice_environment", cust.EnvironmentID)
	params.AddMetadata("external_id", cust.ExternalID)

	// Update the Stripe customer
	_, err = stripeClient.V1Customers.Update(ctx, stripeCustomerID, params)
	if err != nil {
		s.logger.Errorw("failed to update Stripe customer metadata",
			"stripe_customer_id", stripeCustomerID,
			"flexprice_customer_id", cust.ID,
			"error", err)
		return ierr.WithError(err).
			WithHint("Failed to update Stripe customer metadata").
			Mark(ierr.ErrInternal)
	}

	s.logger.Infow("successfully updated Stripe customer metadata",
		"stripe_customer_id", stripeCustomerID,
		"flexprice_customer_id", cust.ID)

	return nil
}

// HasCustomerStripeMapping checks if a customer has a Stripe mapping
func (s *CustomerService) HasCustomerStripeMapping(ctx context.Context, customerID string, customerService interfaces.CustomerService) bool {
	customerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return false
	}

	if customerResp.Customer.Metadata != nil {
		if stripeCustomerID := customerResp.Customer.Metadata["stripe_customer_id"]; stripeCustomerID != "" {
			return true
		}
	}
	if s.entityIntegrationMappingRepo == nil {
		return false
	}

	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      customerID,
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{string(types.SecretProviderStripe)},
	}
	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil || len(mappings) == 0 {
		return false
	}

	stripeCustomerID := mappings[0].ProviderEntityID
	updateReq := dto.UpdateCustomerRequest{
		Metadata: s.mergeCustomerMetadata(
			customerResp.Customer.Metadata,
			map[string]string{"stripe_customer_id": stripeCustomerID},
		),
	}
	if _, err := customerService.UpdateCustomer(ctx, customerResp.Customer.ID, updateReq); err != nil {
		s.logger.Warnw("failed to backfill stripe_customer_id metadata",
			"customer_id", customerID,
			"error", err)
	} else {
		customerResp.Customer.Metadata = updateReq.Metadata
	}

	return true
}
