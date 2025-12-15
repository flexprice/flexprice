package nomod

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

// NomodClient defines the interface for Nomod API operations
type NomodClient interface {
	GetNomodConfig(ctx context.Context) (*NomodConfig, error)
	GetDecryptedNomodConfig(conn *connection.Connection) (*NomodConfig, error)
	HasNomodConnection(ctx context.Context) bool
	GetConnection(ctx context.Context) (*connection.Connection, error)
	CreateCustomer(ctx context.Context, req CreateCustomerRequest) (*CustomerResponse, error)
	CreatePaymentLink(ctx context.Context, req CreatePaymentLinkRequest) (*PaymentLinkResponse, error)
	CreateInvoice(ctx context.Context, req CreateInvoiceRequest) (*InvoiceResponse, error)
	GetCharge(ctx context.Context, chargeID string) (*ChargeResponse, error)
	VerifyWebhookAuth(ctx context.Context, providedSecret string) error
}

// Client handles Nomod API client setup and configuration
type Client struct {
	connectionRepo    connection.Repository
	encryptionService security.EncryptionService
	httpClient        httpclient.Client
	logger            *logger.Logger
}

// NewClient creates a new Nomod client
func NewClient(
	connectionRepo connection.Repository,
	encryptionService security.EncryptionService,
	logger *logger.Logger,
) NomodClient {
	return &Client{
		connectionRepo:    connectionRepo,
		encryptionService: encryptionService,
		httpClient:        httpclient.NewDefaultClient(),
		logger:            logger,
	}
}

// GetNomodConfig retrieves and decrypts Nomod configuration for the current environment
func (c *Client) GetNomodConfig(ctx context.Context) (*NomodConfig, error) {
	// Get Nomod connection for this environment
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderNomod)
	if err != nil {
		return nil, ierr.NewError("failed to get Nomod connection").
			WithHint("Nomod connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	nomodConfig, err := c.GetDecryptedNomodConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Nomod configuration").
			WithHint("Invalid Nomod configuration").
			Mark(ierr.ErrValidation)
	}

	// Validate required fields
	if nomodConfig.APIKey == "" {
		c.logger.Errorw("missing Nomod API key",
			"connection_id", conn.ID,
			"environment_id", conn.EnvironmentID)
		return nil, ierr.NewError("missing Nomod API key").
			WithHint("Configure Nomod API key in the connection settings").
			Mark(ierr.ErrValidation)
	}

	return nomodConfig, nil
}

// GetDecryptedNomodConfig decrypts and returns Nomod configuration
func (c *Client) GetDecryptedNomodConfig(conn *connection.Connection) (*NomodConfig, error) {
	// Decrypt the connection metadata if it's encrypted
	decryptedMetadata, err := c.decryptConnectionMetadata(conn)
	if err != nil {
		return nil, err
	}

	// Extract Nomod configuration from decrypted metadata
	nomodConfig := &NomodConfig{}

	if apiKey, exists := decryptedMetadata["api_key"]; exists {
		nomodConfig.APIKey = apiKey
	}

	if webhookSecret, exists := decryptedMetadata["webhook_secret"]; exists {
		nomodConfig.WebhookSecret = webhookSecret
	}

	return nomodConfig, nil
}

// decryptConnectionMetadata decrypts the connection encrypted secret data
func (c *Client) decryptConnectionMetadata(conn *connection.Connection) (types.Metadata, error) {
	// For Nomod connections, decrypt the structured metadata
	if conn.ProviderType == types.SecretProviderNomod {
		if conn.EncryptedSecretData.Nomod == nil {
			c.logger.Warnw("no nomod metadata found", "connection_id", conn.ID)
			return types.Metadata{}, nil
		}

		// Decrypt API key
		apiKey, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.Nomod.APIKey)
		if err != nil {
			c.logger.Errorw("failed to decrypt API key", "connection_id", conn.ID, "error", err)
			return nil, ierr.NewError("failed to decrypt API key").Mark(ierr.ErrInternal)
		}

		decryptedMetadata := types.Metadata{
			"api_key": apiKey,
		}

		// Decrypt webhook secret if present
		if conn.EncryptedSecretData.Nomod.WebhookSecret != "" {
			webhookSecret, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.Nomod.WebhookSecret)
			if err != nil {
				c.logger.Errorw("failed to decrypt webhook secret", "connection_id", conn.ID, "error", err)
				// Don't fail completely, just skip webhook secret
			} else {
				decryptedMetadata["webhook_secret"] = webhookSecret
			}
		}

		c.logger.Infow("successfully decrypted nomod credentials",
			"connection_id", conn.ID,
			"has_api_key", apiKey != "",
			"has_webhook_secret", decryptedMetadata["webhook_secret"] != "")

		return decryptedMetadata, nil
	}

	return types.Metadata{}, nil
}

// HasNomodConnection checks if the tenant has a Nomod connection available
func (c *Client) HasNomodConnection(ctx context.Context) bool {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderNomod)
	return err == nil && conn != nil && conn.Status == types.StatusPublished
}

// GetConnection retrieves the Nomod connection for the current context
func (c *Client) GetConnection(ctx context.Context) (*connection.Connection, error) {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderNomod)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get Nomod connection").
			Mark(ierr.ErrDatabase)
	}
	if conn == nil {
		return nil, ierr.NewError("Nomod connection not found").
			WithHint("Nomod connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}
	return conn, nil
}

// makeRequest makes a generic HTTP request to Nomod API
func (c *Client) makeRequest(ctx context.Context, method, endpoint string, body interface{}, response interface{}) error {
	// Get Nomod configuration
	config, err := c.GetNomodConfig(ctx)
	if err != nil {
		return err
	}

	// Build full URL
	fullURL := fmt.Sprintf("%s%s", NomodBaseURL, endpoint)

	// Marshal request body if provided
	var jsonBody []byte
	if body != nil {
		jsonBody, err = json.Marshal(body)
		if err != nil {
			c.logger.Errorw("failed to marshal request body", "error", err)
			return ierr.NewError("failed to marshal request body").
				WithHint("Invalid request data").
				Mark(ierr.ErrInternal)
		}
	}

	// Create HTTP request
	httpReq := &httpclient.Request{
		Method: method,
		URL:    fullURL,
		Headers: map[string]string{
			"X-API-KEY":    config.APIKey,
			"Content-Type": "application/json",
			"Accept":       "application/json",
		},
		Body: jsonBody,
	}

	// Send request
	resp, err := c.httpClient.Send(ctx, httpReq)
	if err != nil {
		c.logger.Errorw("nomod API request failed",
			"error", err,
			"method", method,
			"endpoint", endpoint,
			"url", fullURL)
		return ierr.NewError("failed to make request to Nomod API").
			WithHint("Unable to connect to Nomod").
			WithReportableDetails(map[string]interface{}{
				"method":   method,
				"endpoint": endpoint,
				"error":    err.Error(),
			}).
			Mark(ierr.ErrHTTPClient)
	}

	// Check for successful status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logger.Errorw("nomod API returned error",
			"status_code", resp.StatusCode,
			"method", method,
			"endpoint", endpoint,
			"response_body", string(resp.Body))
		return ierr.NewError("Nomod API returned error").
			WithHint(fmt.Sprintf("Nomod API returned status %d", resp.StatusCode)).
			WithReportableDetails(map[string]interface{}{
				"status_code":   resp.StatusCode,
				"method":        method,
				"endpoint":      endpoint,
				"response_body": string(resp.Body),
			}).
			Mark(ierr.ErrHTTPClient)
	}

	// Unmarshal response if provided
	if response != nil {
		if err := json.Unmarshal(resp.Body, response); err != nil {
			c.logger.Errorw("failed to unmarshal response", "error", err, "body", string(resp.Body))
			return ierr.NewError("failed to unmarshal response").
				WithHint("Invalid response from Nomod").
				Mark(ierr.ErrInternal)
		}
	}

	return nil
}

// CreateCustomer creates a customer in Nomod
func (c *Client) CreateCustomer(ctx context.Context, req CreateCustomerRequest) (*CustomerResponse, error) {
	c.logger.Infow("creating customer in Nomod",
		"email", req.Email,
		"first_name", req.FirstName)

	var response CustomerResponse
	err := c.makeRequest(ctx, http.MethodPost, "/v1/customers", req, &response)
	if err != nil {
		c.logger.Errorw("failed to create customer in Nomod", "error", err)
		return nil, err
	}

	c.logger.Infow("successfully created customer in Nomod", "customer_id", response.ID)
	return &response, nil
}

// CreatePaymentLink creates a payment link in Nomod
func (c *Client) CreatePaymentLink(ctx context.Context, req CreatePaymentLinkRequest) (*PaymentLinkResponse, error) {
	c.logger.Infow("creating payment link in Nomod",
		"currency", req.Currency,
		"items_count", len(req.Items))

	var response PaymentLinkResponse
	err := c.makeRequest(ctx, http.MethodPost, "/v1/links", req, &response)
	if err != nil {
		c.logger.Errorw("failed to create payment link in Nomod", "error", err)
		return nil, err
	}

	c.logger.Infow("successfully created payment link in Nomod", "link_id", response.ID, "url", response.URL)
	return &response, nil
}

// CreateInvoice creates an invoice in Nomod
func (c *Client) CreateInvoice(ctx context.Context, req CreateInvoiceRequest) (*InvoiceResponse, error) {
	c.logger.Infow("creating invoice in Nomod",
		"currency", req.Currency,
		"customer", req.Customer,
		"items_count", len(req.Items))

	var response InvoiceResponse
	err := c.makeRequest(ctx, http.MethodPost, "/v1/invoices", req, &response)
	if err != nil {
		c.logger.Errorw("failed to create invoice in Nomod", "error", err)
		return nil, err
	}

	c.logger.Infow("successfully created invoice in Nomod",
		"invoice_id", response.ID,
		"reference_id", response.ReferenceID,
		"status", response.Status)
	return &response, nil
}

// GetCharge retrieves charge details from Nomod API
func (c *Client) GetCharge(ctx context.Context, chargeID string) (*ChargeResponse, error) {
	c.logger.Infow("fetching charge details from Nomod", "charge_id", chargeID)

	var response ChargeResponse
	err := c.makeRequest(ctx, http.MethodGet, fmt.Sprintf("/v1/charges/%s", chargeID), nil, &response)
	if err != nil {
		c.logger.Errorw("failed to get charge from Nomod",
			"error", err,
			"charge_id", chargeID)
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch charge details from Nomod").
			WithReportableDetails(map[string]interface{}{
				"charge_id": chargeID,
			}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully fetched charge from Nomod",
		"charge_id", response.ID,
		"status", response.Status,
		"currency", response.Currency,
		"total", response.Total)

	return &response, nil
}

// VerifyWebhookAuth verifies X-API-KEY header for webhooks
func (c *Client) VerifyWebhookAuth(ctx context.Context, providedAPIKey string) error {
	config, err := c.GetNomodConfig(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get Nomod configuration for webhook verification").
			Mark(ierr.ErrInternal)
	}

	// If no webhook secret configured, skip verification
	if config.WebhookSecret == "" {
		c.logger.Debugw("no webhook secret configured, skipping verification")
		return nil
	}

	// Verify the provided API key matches the webhook secret
	if providedAPIKey != config.WebhookSecret {
		c.logger.Warnw("webhook authentication failed - invalid X-API-KEY")
		return ierr.NewError("invalid webhook API key").
			WithHint("X-API-KEY header does not match configured webhook secret").
			Mark(ierr.ErrValidation)
	}

	c.logger.Debugw("webhook authentication successful")
	return nil
}
