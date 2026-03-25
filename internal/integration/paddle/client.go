package paddle

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	// SandboxBaseURL is the base URL for the Paddle sandbox API.
	// Use when the API key has the pdl_sdbx_ prefix (sandbox API key).
	SandboxBaseURL = "https://sandbox-api.paddle.com"
)

// PaddleClient defines the interface for Paddle API operations
type PaddleClient interface {
	GetPaddleConfig(ctx context.Context) (*PaddleConfig, error)
	GetDecryptedPaddleConfig(conn *connection.Connection) (*PaddleConfig, error)
	HasPaddleConnection(ctx context.Context) bool
	GetConnection(ctx context.Context) (*connection.Connection, error)
	GetSDKClient(ctx context.Context) (*paddle.SDK, *PaddleConfig, error)
	CreateCustomer(ctx context.Context, req *paddle.CreateCustomerRequest) (*paddle.Customer, error)
	CreateAddress(ctx context.Context, customerID string, req *paddle.CreateAddressRequest) (*paddle.Address, error)
	CreateTransaction(ctx context.Context, req *paddle.CreateTransactionRequest) (*paddle.Transaction, error)
	PreviewTransaction(ctx context.Context, req *paddle.PreviewTransactionCreateRequest) (*paddle.TransactionPreview, error)
	VerifyWebhookSignature(ctx context.Context, payload []byte, signature string, webhookSecret string) error
}

// PaddleConfig holds decrypted Paddle connection configuration
type PaddleConfig struct {
	APIKey          string
	WebhookSecret   string
	ClientSideToken string
}

// Client handles Paddle API client setup and configuration
type Client struct {
	connectionRepo    connection.Repository
	encryptionService security.EncryptionService
	logger            *logger.Logger
}

// NewClient creates a new Paddle client
func NewClient(
	connectionRepo connection.Repository,
	encryptionService security.EncryptionService,
	logger *logger.Logger,
) PaddleClient {
	return &Client{
		connectionRepo:    connectionRepo,
		encryptionService: encryptionService,
		logger:            logger,
	}
}

// GetPaddleConfig retrieves and decrypts Paddle configuration for the current environment
func (c *Client) GetPaddleConfig(ctx context.Context) (*PaddleConfig, error) {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderPaddle)
	if err != nil {
		return nil, ierr.NewError("failed to get Paddle connection").
			WithHint("Paddle connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	config, err := c.GetDecryptedPaddleConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Paddle configuration").
			WithHint("Invalid Paddle configuration").
			Mark(ierr.ErrValidation)
	}

	if config.APIKey == "" {
		c.logger.Errorw("missing Paddle API key",
			"connection_id", conn.ID,
			"environment_id", conn.EnvironmentID)
		return nil, ierr.NewError("missing Paddle API key").
			WithHint("Configure Paddle API key in the connection settings").
			Mark(ierr.ErrValidation)
	}

	return config, nil
}

// GetDecryptedPaddleConfig decrypts and returns Paddle configuration
func (c *Client) GetDecryptedPaddleConfig(conn *connection.Connection) (*PaddleConfig, error) {
	decryptedMetadata, err := c.decryptConnectionMetadata(conn)
	if err != nil {
		return nil, err
	}

	config := &PaddleConfig{}
	if apiKey, exists := decryptedMetadata["api_key"]; exists {
		config.APIKey = apiKey
	}
	if webhookSecret, exists := decryptedMetadata["webhook_secret"]; exists {
		config.WebhookSecret = webhookSecret
	}
	if clientSideToken, exists := decryptedMetadata["client_side_token"]; exists {
		config.ClientSideToken = clientSideToken
	}

	return config, nil
}

// decryptConnectionMetadata decrypts the connection encrypted secret data
func (c *Client) decryptConnectionMetadata(conn *connection.Connection) (types.Metadata, error) {
	if conn.ProviderType != types.SecretProviderPaddle || conn.EncryptedSecretData.Paddle == nil {
		c.logger.Warnw("no paddle metadata found", "connection_id", conn.ID)
		return types.Metadata{}, nil
	}

	apiKey, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.Paddle.APIKey)
	if err != nil {
		c.logger.Errorw("failed to decrypt Paddle API key", "connection_id", conn.ID, "error", err)
		return nil, ierr.NewError("failed to decrypt Paddle API key").Mark(ierr.ErrInternal)
	}

	var webhookSecret string
	if conn.EncryptedSecretData.Paddle.WebhookSecret != "" {
		webhookSecret, err = c.encryptionService.Decrypt(conn.EncryptedSecretData.Paddle.WebhookSecret)
		if err != nil {
			c.logger.Warnw("failed to decrypt Paddle webhook secret", "connection_id", conn.ID, "error", err)
		}
	}

	var clientSideToken string
	if conn.EncryptedSecretData.Paddle.ClientSideToken != "" {
		clientSideToken, err = c.encryptionService.Decrypt(conn.EncryptedSecretData.Paddle.ClientSideToken)
		if err != nil {
			c.logger.Warnw("failed to decrypt Paddle client_side_token", "connection_id", conn.ID, "error", err)
		}
	}

	return types.Metadata{
		"api_key":           apiKey,
		"webhook_secret":    webhookSecret,
		"client_side_token": clientSideToken,
	}, nil
}

// isSandboxAPIKey returns true if the API key is a Paddle sandbox key (pdl_sdbx_ prefix).
func isSandboxAPIKey(apiKey string) bool {
	return strings.HasPrefix(strings.TrimSpace(apiKey), "pdl_sdbx_")
}

// GetSDKClient returns a configured Paddle SDK client
func (c *Client) GetSDKClient(ctx context.Context) (*paddle.SDK, *PaddleConfig, error) {
	config, err := c.GetPaddleConfig(ctx)
	if err != nil {
		return nil, nil, err
	}

	baseURL := paddle.ProductionBaseURL
	if isSandboxAPIKey(config.APIKey) {
		baseURL = SandboxBaseURL
		c.logger.Debugw("using Paddle sandbox API",
			"base_url", baseURL)
	}

	client, err := paddle.New(
		config.APIKey,
		paddle.WithBaseURL(baseURL),
	)
	if err != nil {
		c.logger.Errorw("failed to create Paddle SDK client", "error", err)
		return nil, nil, ierr.NewError("failed to initialize Paddle client").
			WithHint("Unable to connect to Paddle").
			Mark(ierr.ErrInternal)
	}

	return client, config, nil
}

// HasPaddleConnection checks if the tenant has a Paddle connection available
func (c *Client) HasPaddleConnection(ctx context.Context) bool {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderPaddle)
	return err == nil && conn != nil && conn.Status == types.StatusPublished
}

// GetConnection retrieves the Paddle connection for the current context
func (c *Client) GetConnection(ctx context.Context) (*connection.Connection, error) {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderPaddle)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get Paddle connection").
			Mark(ierr.ErrDatabase)
	}
	if conn == nil {
		return nil, ierr.NewError("Paddle connection not found").
			WithHint("Paddle connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}
	return conn, nil
}

// CreateCustomer creates a customer in Paddle
func (c *Client) CreateCustomer(ctx context.Context, req *paddle.CreateCustomerRequest) (*paddle.Customer, error) {
	client, _, err := c.GetSDKClient(ctx)
	if err != nil {
		return nil, err
	}

	customer, err := client.CreateCustomer(ctx, req)
	if err != nil {
		c.logger.Errorw("failed to create customer in Paddle", "error", err)
		return nil, ierr.NewError("failed to create customer in Paddle").
			WithHint("Unable to create customer in Paddle").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully created customer in Paddle", "customer_id", customer.ID)
	return customer, nil
}

// CreateAddress creates an address for a customer in Paddle
func (c *Client) CreateAddress(ctx context.Context, customerID string, req *paddle.CreateAddressRequest) (*paddle.Address, error) {
	client, _, err := c.GetSDKClient(ctx)
	if err != nil {
		return nil, err
	}

	// Set CustomerID for path parameter (CreateAddressRequest embeds it)
	req.CustomerID = customerID
	address, err := client.CreateAddress(ctx, req)
	if err != nil {
		c.logger.Errorw("failed to create address in Paddle",
			"error", err,
			"customer_id", customerID)
		return nil, ierr.NewError("failed to create address in Paddle").
			WithHint("Unable to create address in Paddle").
			WithReportableDetails(map[string]interface{}{
				"error":       err.Error(),
				"customer_id": customerID,
			}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully created address in Paddle",
		"address_id", address.ID,
		"customer_id", customerID)
	return address, nil
}

// CreateTransaction creates a transaction (invoice) in Paddle
func (c *Client) CreateTransaction(ctx context.Context, req *paddle.CreateTransactionRequest) (*paddle.Transaction, error) {
	client, _, err := c.GetSDKClient(ctx)
	if err != nil {
		return nil, err
	}

	txn, err := client.CreateTransaction(ctx, req)
	if err != nil {
		c.logger.Errorw("failed to create transaction in Paddle",
			"error", err)
		return nil, ierr.NewError("failed to create transaction in Paddle").
			WithHint("Unable to create transaction in Paddle").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully created transaction in Paddle",
		"transaction_id", txn.ID)
	return txn, nil
}

// PreviewTransaction calls the Paddle transactions/preview API to calculate tax and totals
// without creating a real transaction. This is used to pre-sync Paddle tax to FlexPrice invoices.
func (c *Client) PreviewTransaction(ctx context.Context, req *paddle.PreviewTransactionCreateRequest) (*paddle.TransactionPreview, error) {
	client, _, err := c.GetSDKClient(ctx)
	if err != nil {
		return nil, err
	}

	preview, err := client.PreviewTransactionCreate(ctx, req)
	if err != nil {
		c.logger.Errorw("failed to preview transaction in Paddle",
			"error", err)
		return nil, ierr.NewError("failed to preview transaction in Paddle").
			WithHint("Unable to get tax preview from Paddle").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully previewed transaction in Paddle")
	return preview, nil
}

// toCountryCode converts a string to Paddle CountryCode (uppercase ISO 3166-1 alpha-2)
func toCountryCode(s string) paddle.CountryCode {
	return paddle.CountryCode(strings.ToUpper(strings.TrimSpace(s)))
}

// VerifyWebhookSignature verifies the Paddle-Signature header against the raw payload using the webhook secret.
// Uses HMAC-SHA256 of ts:body as per Paddle docs. Rejects if signature is missing, invalid, or replay attack.
func (c *Client) VerifyWebhookSignature(ctx context.Context, payload []byte, signature string, webhookSecret string) error {
	if webhookSecret == "" {
		return ierr.NewError("webhook secret is required for signature verification").
			WithHint("Configure webhook_secret in Paddle connection").
			Mark(ierr.ErrValidation)
	}
	if signature == "" {
		return ierr.NewError("missing Paddle-Signature header").
			WithHint("Paddle webhooks must include Paddle-Signature header").
			Mark(ierr.ErrValidation)
	}

	verifier := paddle.NewWebhookVerifier(webhookSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(payload))
	if err != nil {
		return ierr.WithError(err).Mark(ierr.ErrInternal)
	}
	req.Header.Set("Paddle-Signature", signature)

	ok, err := verifier.Verify(req)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Webhook signature verification failed").
			Mark(ierr.ErrValidation)
	}
	if !ok {
		return ierr.NewError("webhook signature mismatch").
			WithHint("Request may have been tampered with").
			Mark(ierr.ErrValidation)
	}
	return nil
}
