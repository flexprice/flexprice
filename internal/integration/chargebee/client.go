package chargebee

import (
	"context"

	"github.com/chargebee/chargebee-go/v3"
	customerAction "github.com/chargebee/chargebee-go/v3/actions/customer"
	invoiceAction "github.com/chargebee/chargebee-go/v3/actions/invoice"
	itemAction "github.com/chargebee/chargebee-go/v3/actions/item"
	itemFamilyAction "github.com/chargebee/chargebee-go/v3/actions/itemfamily"
	itemPriceAction "github.com/chargebee/chargebee-go/v3/actions/itemprice"
	"github.com/chargebee/chargebee-go/v3/models/customer"
	chargebeeInvoice "github.com/chargebee/chargebee-go/v3/models/invoice"
	"github.com/chargebee/chargebee-go/v3/models/item"
	"github.com/chargebee/chargebee-go/v3/models/itemfamily"
	"github.com/chargebee/chargebee-go/v3/models/itemprice"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

// ChargebeeClient defines the interface for Chargebee API operations
type ChargebeeClient interface {
	// Configuration and initialization
	GetChargebeeConfig(ctx context.Context) (*ChargebeeConfig, error)
	GetDecryptedChargebeeConfig(conn *connection.Connection) (*ChargebeeConfig, error)
	HasChargebeeConnection(ctx context.Context) bool
	GetConnection(ctx context.Context) (*connection.Connection, error)
	InitializeChargebeeSDK(ctx context.Context) error
	VerifyWebhookSignature(ctx context.Context, payload []byte, signature string) error
	VerifyWebhookBasicAuth(ctx context.Context, username, password string) error

	// Item Family API wrappers
	CreateItemFamily(ctx context.Context, params *itemfamily.CreateRequestParams) (*chargebee.Result, error)
	ListItemFamilies(ctx context.Context, params *itemfamily.ListRequestParams) (*chargebee.ResultList, error)

	// Item API wrappers
	CreateItem(ctx context.Context, params *item.CreateRequestParams) (*chargebee.Result, error)
	RetrieveItem(ctx context.Context, itemID string) (*chargebee.Result, error)

	// Item Price API wrappers
	CreateItemPrice(ctx context.Context, params *itemprice.CreateRequestParams) (*chargebee.Result, error)
	RetrieveItemPrice(ctx context.Context, itemPriceID string) (*chargebee.Result, error)

	// Customer API wrappers
	CreateCustomer(ctx context.Context, params *customer.CreateRequestParams) (*chargebee.Result, error)
	RetrieveCustomer(ctx context.Context, customerID string) (*chargebee.Result, error)

	// Invoice API wrappers
	CreateInvoice(ctx context.Context, params *chargebeeInvoice.CreateForChargeItemsAndChargesRequestParams) (*chargebee.Result, error)
	RetrieveInvoice(ctx context.Context, invoiceID string, params *chargebeeInvoice.RetrieveRequestParams) (*chargebee.Result, error)
}

// Client handles Chargebee API client setup and configuration
type Client struct {
	connectionRepo    connection.Repository
	encryptionService security.EncryptionService
	logger            *logger.Logger
	isInitialized     bool
}

// ChargebeeConfig holds decrypted Chargebee configuration
type ChargebeeConfig struct {
	Site            string // Chargebee site name (e.g., "acme-test")
	APIKey          string // Chargebee API key
	WebhookSecret   string // Webhook secret for verification (optional, NOT USED in v2)
	WebhookUsername string // Basic Auth username for webhook verification (Chargebee v2 security)
	WebhookPassword string // Basic Auth password for webhook verification (Chargebee v2 security)
}

// NewClient creates a new Chargebee client
func NewClient(
	connectionRepo connection.Repository,
	encryptionService security.EncryptionService,
	logger *logger.Logger,
) ChargebeeClient {
	return &Client{
		connectionRepo:    connectionRepo,
		encryptionService: encryptionService,
		logger:            logger,
		isInitialized:     false,
	}
}

// GetChargebeeConfig retrieves and decrypts Chargebee configuration for the current environment
func (c *Client) GetChargebeeConfig(ctx context.Context) (*ChargebeeConfig, error) {
	// Get Chargebee connection for this environment
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderChargebee)
	if err != nil {
		return nil, ierr.NewError("failed to get Chargebee connection").
			WithHint("Chargebee connection not configured for this environment").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrNotFound)
	}

	chargebeeConfig, err := c.GetDecryptedChargebeeConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Chargebee configuration").
			WithHint("Invalid Chargebee configuration").
			Mark(ierr.ErrValidation)
	}

	// Validate required fields
	if chargebeeConfig.Site == "" {
		c.logger.Errorw("missing Chargebee site",
			"connection_id", conn.ID,
			"environment_id", conn.EnvironmentID)
		return nil, ierr.NewError("missing Chargebee site").
			WithHint("Configure Chargebee site in the connection settings").
			Mark(ierr.ErrValidation)
	}

	if chargebeeConfig.APIKey == "" {
		c.logger.Errorw("missing Chargebee API key",
			"connection_id", conn.ID,
			"environment_id", conn.EnvironmentID)
		return nil, ierr.NewError("missing Chargebee API key").
			WithHint("Configure Chargebee API key in the connection settings").
			Mark(ierr.ErrValidation)
	}

	return chargebeeConfig, nil
}

// GetDecryptedChargebeeConfig decrypts and returns Chargebee configuration
func (c *Client) GetDecryptedChargebeeConfig(conn *connection.Connection) (*ChargebeeConfig, error) {
	// Decrypt the connection metadata if it's encrypted
	decryptedMetadata, err := c.decryptConnectionMetadata(conn)
	if err != nil {
		return nil, err
	}

	// Extract Chargebee configuration from decrypted metadata
	chargebeeConfig := &ChargebeeConfig{}

	if site, exists := decryptedMetadata["site"]; exists {
		chargebeeConfig.Site = site
	}

	if apiKey, exists := decryptedMetadata["api_key"]; exists {
		chargebeeConfig.APIKey = apiKey
	}

	if webhookSecret, exists := decryptedMetadata["webhook_secret"]; exists {
		chargebeeConfig.WebhookSecret = webhookSecret
	}

	if webhookUsername, exists := decryptedMetadata["webhook_username"]; exists {
		chargebeeConfig.WebhookUsername = webhookUsername
	}

	if webhookPassword, exists := decryptedMetadata["webhook_password"]; exists {
		chargebeeConfig.WebhookPassword = webhookPassword
	}

	c.logger.Infow("retrieved Chargebee config",
		"site", chargebeeConfig.Site,
		"has_api_key", chargebeeConfig.APIKey != "",
		"has_webhook_secret", chargebeeConfig.WebhookSecret != "",
		"has_webhook_auth", chargebeeConfig.WebhookUsername != "" && chargebeeConfig.WebhookPassword != "")

	return chargebeeConfig, nil
}

// decryptConnectionMetadata decrypts the connection encrypted secret data
func (c *Client) decryptConnectionMetadata(conn *connection.Connection) (types.Metadata, error) {
	// Check if the connection has encrypted secret data
	if conn.EncryptedSecretData.Chargebee == nil {
		c.logger.Warnw("no chargebee metadata found in encrypted secret data", "connection_id", conn.ID)
		return types.Metadata{}, nil
	}

	// For Chargebee connections, decrypt the structured metadata
	if conn.ProviderType == types.SecretProviderChargebee {
		if conn.EncryptedSecretData.Chargebee == nil {
			c.logger.Warnw("no chargebee metadata found", "connection_id", conn.ID)
			return types.Metadata{}, nil
		}

		// Site is not encrypted (it's public information)
		site := conn.EncryptedSecretData.Chargebee.Site

		// Decrypt API key
		apiKey, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.Chargebee.APIKey)
		if err != nil {
			c.logger.Errorw("failed to decrypt API key", "connection_id", conn.ID, "error", err)
			return nil, ierr.NewError("failed to decrypt API key").Mark(ierr.ErrInternal)
		}

		// Decrypt webhook secret (optional field, NOT USED in v2)
		var webhookSecret string
		if conn.EncryptedSecretData.Chargebee.WebhookSecret != "" {
			webhookSecret, err = c.encryptionService.Decrypt(conn.EncryptedSecretData.Chargebee.WebhookSecret)
			if err != nil {
				c.logger.Warnw("failed to decrypt webhook secret", "connection_id", conn.ID, "error", err)
				// Don't fail - webhook secret is optional
				webhookSecret = ""
			}
		}

		// Decrypt webhook username (optional field)
		var webhookUsername string
		if conn.EncryptedSecretData.Chargebee.WebhookUsername != "" {
			webhookUsername, err = c.encryptionService.Decrypt(conn.EncryptedSecretData.Chargebee.WebhookUsername)
			if err != nil {
				c.logger.Warnw("failed to decrypt webhook username", "connection_id", conn.ID, "error", err)
				// Don't fail - webhook username is optional
				webhookUsername = ""
			}
		}

		// Decrypt webhook password (optional field)
		var webhookPassword string
		if conn.EncryptedSecretData.Chargebee.WebhookPassword != "" {
			webhookPassword, err = c.encryptionService.Decrypt(conn.EncryptedSecretData.Chargebee.WebhookPassword)
			if err != nil {
				c.logger.Warnw("failed to decrypt webhook password", "connection_id", conn.ID, "error", err)
				// Don't fail - webhook password is optional
				webhookPassword = ""
			}
		}

		decryptedMetadata := types.Metadata{
			"site":             site,
			"api_key":          apiKey,
			"webhook_secret":   webhookSecret,
			"webhook_username": webhookUsername,
			"webhook_password": webhookPassword,
		}

		c.logger.Infow("successfully decrypted chargebee credentials",
			"connection_id", conn.ID,
			"site", site,
			"has_api_key", apiKey != "",
			"has_webhook_secret", webhookSecret != "",
			"has_webhook_auth", webhookUsername != "" && webhookPassword != "")

		return decryptedMetadata, nil
	}

	return types.Metadata{}, nil
}

// HasChargebeeConnection checks if Chargebee connection exists for the current environment
func (c *Client) HasChargebeeConnection(ctx context.Context) bool {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderChargebee)
	return err == nil && conn != nil && conn.Status == types.StatusPublished
}

// GetConnection retrieves the Chargebee connection for the current context
func (c *Client) GetConnection(ctx context.Context) (*connection.Connection, error) {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderChargebee)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get Chargebee connection").
			Mark(ierr.ErrDatabase)
	}
	if conn == nil {
		return nil, ierr.NewError("Chargebee connection not found").
			WithHint("Chargebee connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}
	return conn, nil
}

// InitializeChargebeeSDK configures the global Chargebee SDK instance
// This should be called before making any Chargebee SDK API calls
func (c *Client) InitializeChargebeeSDK(ctx context.Context) error {
	if c.isInitialized {
		return nil
	}

	config, err := c.GetChargebeeConfig(ctx)
	if err != nil {
		return err
	}

	// Configure Chargebee SDK globally
	chargebee.Configure(config.APIKey, config.Site)

	c.isInitialized = true
	c.logger.Infow("initialized Chargebee SDK",
		"site", config.Site)

	return nil
}

// VerifyWebhookSignature verifies the Chargebee webhook signature
func (c *Client) VerifyWebhookSignature(ctx context.Context, payload []byte, signature string) error {
	c.logger.Debugw("Chargebee v2 webhook signature verification skipped - not supported",
		"note", "Use Basic Auth and IP whitelisting for security")
	return nil
}

// VerifyWebhookBasicAuth verifies Basic Authentication credentials for Chargebee webhooks
// Chargebee v2 uses Basic Auth (username/password) as the primary webhook security mechanism
func (c *Client) VerifyWebhookBasicAuth(ctx context.Context, username, password string) error {
	config, err := c.GetChargebeeConfig(ctx)
	if err != nil {
		c.logger.Errorw("failed to get Chargebee config for Basic Auth verification", "error", err)
		return ierr.NewError("failed to verify webhook authentication").
			WithHint("Unable to verify Chargebee webhook Basic Auth").
			Mark(ierr.ErrInternal)
	}

	// Check if webhook auth is configured
	// Note: WebhookUsername and WebhookPassword should be stored in your Chargebee connection config
	// These are the credentials you set in Chargebee UI: "Protect webhook URL with basic authentication"
	if config.WebhookUsername == "" || config.WebhookPassword == "" {
		c.logger.Warnw("webhook Basic Auth credentials not configured, skipping verification",
			"note", "Configure username/password in Chargebee webhook settings for security")
		return nil // Allow webhook without auth if not configured
	}

	// Verify credentials match what was configured
	if username != config.WebhookUsername || password != config.WebhookPassword {
		c.logger.Errorw("webhook Basic Auth verification failed",
			"remote_addr", "masked_for_security") // Don't log credentials or usernames
		return ierr.NewError("webhook authentication failed").
			WithHint("Invalid Basic Auth credentials").
			Mark(ierr.ErrValidation)
	}

	c.logger.Infow("webhook Basic Auth verified successfully")
	return nil
}

// Item Family API Wrappers
// CreateItemFamily creates an item family in Chargebee
func (c *Client) CreateItemFamily(ctx context.Context, params *itemfamily.CreateRequestParams) (*chargebee.Result, error) {
	if err := c.InitializeChargebeeSDK(ctx); err != nil {
		return nil, err
	}

	result, err := itemFamilyAction.Create(params).Request()
	if err != nil {
		c.logger.Errorw("failed to create item family in Chargebee API",
			"family_id", params.Id,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to create item family in Chargebee").
			Mark(ierr.ErrValidation)
	}

	return result, nil
}

// ListItemFamilies lists item families from Chargebee
func (c *Client) ListItemFamilies(ctx context.Context, params *itemfamily.ListRequestParams) (*chargebee.ResultList, error) {
	if err := c.InitializeChargebeeSDK(ctx); err != nil {
		return nil, err
	}

	result, err := itemFamilyAction.List(params).ListRequest()
	if err != nil {
		c.logger.Errorw("failed to list item families from Chargebee API", "error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list item families from Chargebee").
			Mark(ierr.ErrValidation)
	}

	return result, nil
}

// Item API Wrappers
// CreateItem creates an item in Chargebee
func (c *Client) CreateItem(ctx context.Context, params *item.CreateRequestParams) (*chargebee.Result, error) {
	if err := c.InitializeChargebeeSDK(ctx); err != nil {
		return nil, err
	}

	result, err := itemAction.Create(params).Request()
	if err != nil {
		c.logger.Errorw("failed to create item in Chargebee API",
			"item_id", params.Id,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to create item in Chargebee").
			Mark(ierr.ErrValidation)
	}

	return result, nil
}

// RetrieveItem retrieves an item from Chargebee
func (c *Client) RetrieveItem(ctx context.Context, itemID string) (*chargebee.Result, error) {
	if err := c.InitializeChargebeeSDK(ctx); err != nil {
		return nil, err
	}

	result, err := itemAction.Retrieve(itemID).Request()
	if err != nil {
		c.logger.Errorw("failed to retrieve item from Chargebee API",
			"item_id", itemID,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve item from Chargebee").
			Mark(ierr.ErrNotFound)
	}

	return result, nil
}

// Item Price API Wrappers
// CreateItemPrice creates an item price in Chargebee
func (c *Client) CreateItemPrice(ctx context.Context, params *itemprice.CreateRequestParams) (*chargebee.Result, error) {
	if err := c.InitializeChargebeeSDK(ctx); err != nil {
		return nil, err
	}

	result, err := itemPriceAction.Create(params).Request()
	if err != nil {
		c.logger.Errorw("failed to create item price in Chargebee API",
			"item_price_id", params.Id,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to create item price in Chargebee").
			Mark(ierr.ErrValidation)
	}

	return result, nil
}

// RetrieveItemPrice retrieves an item price from Chargebee
func (c *Client) RetrieveItemPrice(ctx context.Context, itemPriceID string) (*chargebee.Result, error) {
	if err := c.InitializeChargebeeSDK(ctx); err != nil {
		return nil, err
	}

	result, err := itemPriceAction.Retrieve(itemPriceID).Request()
	if err != nil {
		c.logger.Errorw("failed to retrieve item price from Chargebee API",
			"item_price_id", itemPriceID,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve item price from Chargebee").
			Mark(ierr.ErrNotFound)
	}

	return result, nil
}

// Customer API Wrappers
// CreateCustomer creates a customer in Chargebee
func (c *Client) CreateCustomer(ctx context.Context, params *customer.CreateRequestParams) (*chargebee.Result, error) {
	if err := c.InitializeChargebeeSDK(ctx); err != nil {
		return nil, err
	}

	result, err := customerAction.Create(params).Request()
	if err != nil {
		c.logger.Errorw("failed to create customer in Chargebee API",
			"customer_id", params.Id,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to create customer in Chargebee").
			Mark(ierr.ErrValidation)
	}

	return result, nil
}

// RetrieveCustomer retrieves a customer from Chargebee
func (c *Client) RetrieveCustomer(ctx context.Context, customerID string) (*chargebee.Result, error) {
	if err := c.InitializeChargebeeSDK(ctx); err != nil {
		return nil, err
	}

	result, err := customerAction.Retrieve(customerID).Request()
	if err != nil {
		c.logger.Errorw("failed to retrieve customer from Chargebee API",
			"customer_id", customerID,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve customer from Chargebee").
			Mark(ierr.ErrNotFound)
	}

	return result, nil
}

// Invoice API Wrappers
// CreateInvoice creates an invoice in Chargebee
func (c *Client) CreateInvoice(ctx context.Context, params *chargebeeInvoice.CreateForChargeItemsAndChargesRequestParams) (*chargebee.Result, error) {
	if err := c.InitializeChargebeeSDK(ctx); err != nil {
		return nil, err
	}

	result, err := invoiceAction.CreateForChargeItemsAndCharges(params).Request()
	if err != nil {
		c.logger.Errorw("failed to create invoice in Chargebee API",
			"customer_id", params.CustomerId,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to create invoice in Chargebee").
			Mark(ierr.ErrValidation)
	}

	return result, nil
}

// RetrieveInvoice retrieves an invoice from Chargebee
func (c *Client) RetrieveInvoice(ctx context.Context, invoiceID string, params *chargebeeInvoice.RetrieveRequestParams) (*chargebee.Result, error) {
	if err := c.InitializeChargebeeSDK(ctx); err != nil {
		return nil, err
	}

	result, err := invoiceAction.Retrieve(invoiceID, params).Request()
	if err != nil {
		c.logger.Errorw("failed to retrieve invoice from Chargebee API",
			"invoice_id", invoiceID,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve invoice from Chargebee").
			Mark(ierr.ErrNotFound)
	}

	return result, nil
}
