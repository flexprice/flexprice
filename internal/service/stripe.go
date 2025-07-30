package service

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stripe/stripe-go/v79"
	"github.com/stripe/stripe-go/v79/client"
	"github.com/stripe/stripe-go/v79/webhook"
)

// StripeService handles Stripe integration operations
type StripeService struct {
	ServiceParams
	encryptionService security.EncryptionService
}

// NewStripeService creates a new Stripe service instance
func NewStripeService(params ServiceParams) *StripeService {
	encryptionService, err := security.NewEncryptionService(params.Config, params.Logger)
	if err != nil {
		params.Logger.Fatalw("failed to create encryption service", "error", err)
	}

	return &StripeService{
		ServiceParams:     params,
		encryptionService: encryptionService,
	}
}

// decryptConnectionMetadata decrypts the connection metadata if it's encrypted
func (s *StripeService) decryptConnectionMetadata(metadata map[string]interface{}) (map[string]interface{}, error) {
	if metadata == nil {
		return nil, nil
	}

	decryptedMetadata := make(map[string]interface{})

	// Traverse metadata by key-value pairs
	for key, value := range metadata {
		// Check if the value is encrypted (string)
		if encryptedValue, ok := value.(string); ok {
			// Decrypt the JSON string
			decryptedJSON, err := s.encryptionService.Decrypt(encryptedValue)
			if err != nil {
				return nil, err
			}

			// Deserialize back to original type
			var decryptedValue interface{}
			if err := json.Unmarshal([]byte(decryptedJSON), &decryptedValue); err != nil {
				return nil, err
			}

			decryptedMetadata[key] = decryptedValue
		} else {
			// If value is not encrypted (for backward compatibility), keep as-is
			decryptedMetadata[key] = value
		}
	}

	return decryptedMetadata, nil
}

// GetDecryptedStripeConfig gets the decrypted Stripe configuration from a connection
func (s *StripeService) GetDecryptedStripeConfig(conn *connection.Connection) (*connection.StripeConnection, error) {
	// Decrypt metadata if needed
	decryptedMetadata, err := s.decryptConnectionMetadata(conn.Metadata)
	if err != nil {
		return nil, err
	}

	// Create a temporary connection with decrypted metadata
	tempConn := &connection.Connection{
		ID:            conn.ID,
		Name:          conn.Name,
		ProviderType:  conn.ProviderType,
		Metadata:      decryptedMetadata,
		EnvironmentID: conn.EnvironmentID,
		BaseModel:     conn.BaseModel,
	}

	// Now call GetStripeConfig on the decrypted connection
	return tempConn.GetStripeConfig()
}

// CreateCustomerInStripe creates a customer in Stripe and updates our customer with Stripe ID
func (s *StripeService) CreateCustomerInStripe(ctx context.Context, customerID string) error {
	// Get our customer
	customerService := NewCustomerService(s.ServiceParams)
	ourCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return err
	}
	ourCustomer := ourCustomerResp.Customer

	// Get Stripe connection for this environment
	conn, err := s.ConnectionRepo.GetByEnvironmentAndProvider(ctx, ourCustomer.EnvironmentID, types.SecretProviderStripe)
	if err != nil {
		return ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	stripeConfig, err := s.GetDecryptedStripeConfig(conn)
	if err != nil {
		return ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	// Initialize Stripe client
	sc := &client.API{}
	sc.Init(stripeConfig.SecretKey, nil)

	// Check if customer already has Stripe ID
	if stripeID, exists := ourCustomer.Metadata["stripe_customer_id"]; exists && stripeID != "" {
		return ierr.NewError("customer already has Stripe ID").
			WithHint("Customer is already synced with Stripe").
			Mark(ierr.ErrAlreadyExists)
	}

	// Create customer in Stripe
	params := &stripe.CustomerParams{
		Name:  stripe.String(ourCustomer.Name),
		Email: stripe.String(ourCustomer.Email),
		Metadata: map[string]string{
			"flexprice_customer_id": ourCustomer.ID,
			"flexprice_environment": ourCustomer.EnvironmentID,
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

	stripeCustomer, err := sc.Customers.New(params)
	if err != nil {
		return ierr.NewError("failed to create customer in Stripe").
			WithHint("Stripe API error").
			Mark(ierr.ErrHTTPClient)
	}

	// Update our customer with Stripe ID
	updateReq := dto.UpdateCustomerRequest{
		Metadata: map[string]string{
			"stripe_customer_id": stripeCustomer.ID,
		},
	}
	// Merge with existing metadata
	if ourCustomer.Metadata != nil {
		for k, v := range ourCustomer.Metadata {
			updateReq.Metadata[k] = v
		}
	}

	_, err = customerService.UpdateCustomer(ctx, ourCustomer.ID, updateReq)
	if err != nil {
		return err
	}

	return nil
}

// CreateCustomerFromStripe creates a customer in our system from Stripe webhook data
func (s *StripeService) CreateCustomerFromStripe(ctx context.Context, stripeCustomer *stripe.Customer, environmentID string) error {
	// Create customer service instance
	customerService := NewCustomerService(s.ServiceParams)

	// Check if customer already exists by Stripe ID
	existingCustomer, err := s.findCustomerByStripeID(ctx, environmentID, stripeCustomer.ID)
	if err == nil && existingCustomer != nil {
		// Customer already exists, no need to create
		return ierr.NewError("customer already exists").
			WithHint("Customer with this Stripe ID already exists in our system").
			Mark(ierr.ErrAlreadyExists)
	}

	// Check for existing customer by external ID if flexprice_customer_id is present
	var externalID string
	if flexpriceID, exists := stripeCustomer.Metadata["flexprice_customer_id"]; exists {
		externalID = flexpriceID
		// Check if customer with this external ID already exists
		existing, err := s.findCustomerByExternalID(ctx, environmentID, externalID)
		if err == nil && existing != nil {
			// Customer exists with this external ID, update with Stripe ID
			updateReq := dto.UpdateCustomerRequest{
				Metadata: map[string]string{
					"stripe_customer_id": stripeCustomer.ID,
				},
			}
			// Merge with existing metadata
			if existing.Metadata != nil {
				for k, v := range existing.Metadata {
					updateReq.Metadata[k] = v
				}
			}
			_, err = customerService.UpdateCustomer(ctx, existing.ID, updateReq)
			return err
		}
	} else {
		// Generate external ID if not present
		externalID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER)
	}

	// Create new customer using DTO
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

	_, err = customerService.CreateCustomer(ctx, createReq)
	return err
}

// ParseWebhookEvent parses a Stripe webhook event with signature verification
func (s *StripeService) ParseWebhookEvent(payload []byte, signature string, webhookSecret string) (*stripe.Event, error) {
	// Verify the webhook signature, ignoring API version mismatch
	options := webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	}
	event, err := webhook.ConstructEventWithOptions(payload, signature, webhookSecret, options)
	if err != nil {
		// Log the error using structured logging
		s.Logger.Errorw("Stripe webhook verification failed", "error", err)
		return nil, ierr.NewError("failed to verify webhook signature").
			WithHint("Invalid webhook signature or payload").
			Mark(ierr.ErrValidation)
	}
	return &event, nil
}

// VerifyWebhookSignature verifies the Stripe webhook signature
func (s *StripeService) VerifyWebhookSignature(payload []byte, signature string, webhookSecret string) error {
	_, err := webhook.ConstructEvent(payload, signature, webhookSecret)
	if err != nil {
		return ierr.NewError("failed to verify webhook signature").
			WithHint("Invalid webhook signature or payload").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// findCustomerByStripeID finds a customer by Stripe customer ID in metadata
// TODO: This method fetches all customers and filters in memory, which is not scalable.
// Consider adding a database index on metadata->>'stripe_customer_id' for better performance.
func (s *StripeService) findCustomerByStripeID(ctx context.Context, environmentID, stripeCustomerID string) (*dto.CustomerResponse, error) {
	// Use customer filter to search by metadata
	filter := &types.CustomerFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	}

	customerService := NewCustomerService(s.ServiceParams)
	customers, err := customerService.GetCustomers(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Search through customers for matching Stripe ID
	for _, customer := range customers.Items {
		if customer.Metadata != nil {
			if stripeID, exists := customer.Metadata["stripe_customer_id"]; exists && stripeID == stripeCustomerID {
				return customer, nil
			}
		}
	}

	return nil, ierr.NewError("customer not found").Mark(ierr.ErrNotFound)
}

// findCustomerByExternalID finds a customer by external ID using efficient lookup
func (s *StripeService) findCustomerByExternalID(ctx context.Context, environmentID, externalID string) (*dto.CustomerResponse, error) {
	customerService := NewCustomerService(s.ServiceParams)
	return customerService.GetCustomerByLookupKey(ctx, externalID)
}
