package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

// PaddleService handles Paddle integration operations
type PaddleService struct {
	ServiceParams
	encryptionService security.EncryptionService
}

// NewPaddleService creates a new Paddle service instance
func NewPaddleService(params ServiceParams) *PaddleService {
	encryptionService, err := security.NewEncryptionService(params.Config, params.Logger)
	if err != nil {
		params.Logger.Fatalw("failed to create encryption service", "error", err)
	}

	return &PaddleService{
		ServiceParams:     params,
		encryptionService: encryptionService,
	}
}

// decryptConnectionMetadata decrypts the connection encrypted secret data if it's encrypted
func (s *PaddleService) decryptConnectionMetadata(encryptedSecretData types.ConnectionMetadata, providerType types.SecretProvider) (types.ConnectionMetadata, error) {
	decryptedMetadata := encryptedSecretData

	switch providerType {
	case types.SecretProviderPaddle:
		if encryptedSecretData.Paddle != nil {
			decryptedAPIKey, err := s.encryptionService.Decrypt(encryptedSecretData.Paddle.APIKey)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
			decryptedWebhookSecret, err := s.encryptionService.Decrypt(encryptedSecretData.Paddle.WebhookSecret)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}

			decryptedMetadata.Paddle = &types.PaddleConnectionMetadata{
				APIKey:        decryptedAPIKey,
				WebhookSecret: decryptedWebhookSecret,
			}
		}

	default:
		// For other providers or unknown types, use generic format
		if encryptedSecretData.Generic != nil {
			decryptedData := make(map[string]interface{})
			for key, value := range encryptedSecretData.Generic.Data {
				if strValue, ok := value.(string); ok {
					decryptedValue, err := s.encryptionService.Decrypt(strValue)
					if err != nil {
						return types.ConnectionMetadata{}, err
					}
					decryptedData[key] = decryptedValue
				} else {
					decryptedData[key] = value
				}
			}
			decryptedMetadata.Generic = &types.GenericConnectionMetadata{
				Data: decryptedData,
			}
		}
	}

	return decryptedMetadata, nil
}

// GetDecryptedPaddleConfig gets the decrypted Paddle configuration from a connection
func (s *PaddleService) GetDecryptedPaddleConfig(conn *connection.Connection) (*connection.PaddleConnection, error) {
	// Decrypt metadata if needed
	decryptedMetadata, err := s.decryptConnectionMetadata(conn.EncryptedSecretData, conn.ProviderType)
	if err != nil {
		return nil, err
	}

	// Create a temporary connection with decrypted encrypted secret data
	tempConn := &connection.Connection{
		ID:                  conn.ID,
		Name:                conn.Name,
		ProviderType:        conn.ProviderType,
		EncryptedSecretData: decryptedMetadata,
		EnvironmentID:       conn.EnvironmentID,
	}

	// Now call GetPaddleConfig on the decrypted connection
	return tempConn.GetPaddleConfig()
}

// CreatePaddleClient creates and configures a Paddle API client
func (s *PaddleService) CreatePaddleClient(ctx context.Context) (*paddle.SDK, error) {
	// Get Paddle connection for this environment
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderPaddle)
	if err != nil {
		return nil, ierr.NewError("failed to get Paddle connection").
			WithHint("Paddle connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	paddleConfig, err := s.GetDecryptedPaddleConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Paddle configuration").
			WithHint("Invalid Paddle configuration").
			Mark(ierr.ErrValidation)
	}

	// Create Paddle client - determine environment based on API key prefix
	var client *paddle.SDK
	var clientErr error

	// Handle different API key formats
	if strings.HasPrefix(paddleConfig.APIKey, "test_") {
		// Old sandbox format
		client, clientErr = paddle.NewSandbox(paddleConfig.APIKey)
	} else if strings.HasPrefix(paddleConfig.APIKey, "live_") {
		// Old production format
		client, clientErr = paddle.New(paddleConfig.APIKey)
	} else if strings.HasPrefix(paddleConfig.APIKey, "pdl_s") {
		// New sandbox format - still needs NewSandbox()
		client, clientErr = paddle.NewSandbox(paddleConfig.APIKey)
	} else if strings.HasPrefix(paddleConfig.APIKey, "pdl_l") {
		// New production format
		client, clientErr = paddle.New(paddleConfig.APIKey)
	} else if strings.HasPrefix(paddleConfig.APIKey, "pdl_") {
		// Other pdl_ keys - default to production
		client, clientErr = paddle.New(paddleConfig.APIKey)
	} else {
		// Debug: Log the first few characters of API key to verify decryption
		apiKeyPrefix := "unknown"
		if len(paddleConfig.APIKey) >= 5 {
			apiKeyPrefix = paddleConfig.APIKey[:5]
		}
		return nil, ierr.NewError("invalid Paddle API key format").
			WithHint(fmt.Sprintf("API key should start with 'test_', 'live_', or 'pdl_', got: %s", apiKeyPrefix)).
			Mark(ierr.ErrValidation)
	}

	if clientErr != nil {
		return nil, ierr.NewError("failed to create Paddle client").
			WithHint("Invalid Paddle API key or configuration").
			Mark(ierr.ErrValidation)
	}

	return client, nil
}

// CreateCustomerInPaddle creates a customer in Paddle and updates our customer with Paddle ID
func (s *PaddleService) CreateCustomerInPaddle(ctx context.Context, customerID string) error {
	// Get our customer
	customerService := NewCustomerService(s.ServiceParams)
	ourCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return err
	}
	ourCustomer := ourCustomerResp.Customer

	// Create Paddle client
	paddleClient, err := s.CreatePaddleClient(ctx)
	if err != nil {
		return err
	}

	// Check if customer already has Paddle ID
	if paddleID, exists := ourCustomer.Metadata["paddle_customer_id"]; exists && paddleID != "" {
		return ierr.NewError("customer already has Paddle ID").
			WithHint("Customer is already synced with Paddle").
			Mark(ierr.ErrAlreadyExists)
	}

	// Validate email format
	if ourCustomer.Email == "" {
		return ierr.NewError("customer email is required").
			WithHint("Paddle requires a valid email address").
			Mark(ierr.ErrValidation)
	}

	// Create customer in Paddle
	createReq := &paddle.CreateCustomerRequest{
		Email: ourCustomer.Email,
	}

	if ourCustomer.Name != "" {
		createReq.Name = &ourCustomer.Name
	}

	// Add custom data if needed
	createReq.CustomData = paddle.CustomData{
		"flexprice_customer_id": ourCustomer.ID,
		"flexprice_environment": ourCustomer.EnvironmentID,
	}

	// Debug: Log the request being sent (without sensitive data)
	fmt.Printf("DEBUG: Creating Paddle customer with email: %s, name: %v\n", createReq.Email, createReq.Name)

	paddleCustomer, err := paddleClient.CreateCustomer(ctx, createReq)
	if err != nil {
		return ierr.NewError("failed to create customer in Paddle").
			WithHint(fmt.Sprintf("Paddle API error: %v", err)).
			Mark(ierr.ErrHTTPClient)
	}

	// Update our customer with Paddle ID
	updateReq := dto.UpdateCustomerRequest{
		Metadata: map[string]string{
			"paddle_customer_id": paddleCustomer.ID,
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

// CreateCustomerFromPaddle creates a customer in our system from Paddle webhook data
func (s *PaddleService) CreateCustomerFromPaddle(ctx context.Context, paddleCustomer map[string]interface{}, environmentID string) error {
	// Create customer service instance
	customerService := NewCustomerService(s.ServiceParams)

	// Extract customer data from Paddle webhook
	email, _ := paddleCustomer["email"].(string)
	name, _ := paddleCustomer["name"].(string)
	paddleCustomerID, _ := paddleCustomer["id"].(string)

	if email == "" || paddleCustomerID == "" {
		return ierr.NewError("invalid paddle customer data").
			WithHint("Email and customer ID are required").
			Mark(ierr.ErrValidation)
	}

	// Check for existing customer by external ID if flexprice_customer_id is present
	var externalID string
	if customData, ok := paddleCustomer["custom_data"].(map[string]interface{}); ok {
		if flexpriceID, exists := customData["flexprice_customer_id"].(string); exists {
			externalID = flexpriceID
			// Check if customer with this external ID already exists
			existing, err := customerService.GetCustomerByLookupKey(ctx, externalID)
			if err == nil && existing != nil {
				// Customer exists with this external ID, update with Paddle ID
				updateReq := dto.UpdateCustomerRequest{
					Metadata: map[string]string{
						"paddle_customer_id": paddleCustomerID,
					},
				}
				// Merge with existing metadata
				if existing.Customer.Metadata != nil {
					for k, v := range existing.Customer.Metadata {
						updateReq.Metadata[k] = v
					}
				}
				_, err = customerService.UpdateCustomer(ctx, existing.Customer.ID, updateReq)
				return err
			}
		}
	}

	if externalID == "" {
		// Generate external ID if not present
		externalID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER)
	}

	// Create new customer using DTO
	createReq := dto.CreateCustomerRequest{
		ExternalID: externalID,
		Name:       name,
		Email:      email,
		Metadata: map[string]string{
			"paddle_customer_id": paddleCustomerID,
		},
	}

	_, err := customerService.CreateCustomer(ctx, createReq)
	return err
}

// VerifyWebhookSignature verifies the Paddle webhook signature using Paddle SDK
func (s *PaddleService) VerifyWebhookSignature(ctx context.Context, req *http.Request) error {
	// Get Paddle connection to retrieve webhook secret
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderPaddle)
	if err != nil {
		return ierr.NewError("failed to get Paddle connection for webhook verification").
			WithHint("Paddle connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	paddleConfig, err := s.GetDecryptedPaddleConfig(conn)
	if err != nil {
		return ierr.NewError("failed to get Paddle configuration").
			WithHint("Invalid Paddle configuration").
			Mark(ierr.ErrValidation)
	}

	// Verify webhook secret is configured
	if paddleConfig.WebhookSecret == "" {
		return ierr.NewError("webhook secret not configured for Paddle connection").
			WithHint("Paddle webhook secret is required").
			Mark(ierr.ErrValidation)
	}

	// Create Paddle webhook verifier
	verifier := paddle.NewWebhookVerifier(paddleConfig.WebhookSecret)

	// Verify the webhook signature
	verified, err := verifier.Verify(req)
	if err != nil {
		return ierr.NewError("failed to verify Paddle webhook signature").
			WithHint("Error during webhook verification").
			Mark(ierr.ErrValidation)
	}

	if !verified {
		return ierr.NewError("invalid Paddle webhook signature").
			WithHint("Webhook signature verification failed").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ParseWebhookEvent parses a Paddle webhook event
func (s *PaddleService) ParseWebhookEvent(payload []byte) (map[string]interface{}, error) {
	// Parse JSON payload
	var event map[string]interface{}
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, ierr.NewError("failed to parse webhook payload").
			WithHint("Invalid JSON payload").
			Mark(ierr.ErrValidation)
	}

	return event, nil
}
