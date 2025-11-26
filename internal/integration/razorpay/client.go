package razorpay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
	razorpay "github.com/razorpay/razorpay-go"
)

// RazorpayClient defines the interface for Razorpay API operations
type RazorpayClient interface {
	GetRazorpayConfig(ctx context.Context) (*RazorpayConfig, error)
	GetDecryptedRazorpayConfig(conn *connection.Connection) (*RazorpayConfig, error)
	GetRazorpaySDKClient(ctx context.Context) (*razorpay.Client, *RazorpayConfig, error)
	HasRazorpayConnection(ctx context.Context) bool
	GetConnection(ctx context.Context) (*connection.Connection, error)
	CreateCustomer(ctx context.Context, customerData map[string]interface{}) (map[string]interface{}, error)
	CreatePaymentLink(ctx context.Context, paymentLinkData map[string]interface{}) (map[string]interface{}, error)
	CreateInvoice(ctx context.Context, invoiceData map[string]interface{}) (map[string]interface{}, error)
	GetInvoice(ctx context.Context, invoiceID string) (map[string]interface{}, error)
	VerifyWebhookSignature(ctx context.Context, payload []byte, signature string) error
}

// Client handles Razorpay API client setup and configuration
type Client struct {
	connectionRepo    connection.Repository
	encryptionService security.EncryptionService
	logger            *logger.Logger
}

// NewClient creates a new Razorpay client
func NewClient(
	connectionRepo connection.Repository,
	encryptionService security.EncryptionService,
	logger *logger.Logger,
) RazorpayClient {
	return &Client{
		connectionRepo:    connectionRepo,
		encryptionService: encryptionService,
		logger:            logger,
	}
}

// GetRazorpayConfig retrieves and decrypts Razorpay configuration for the current environment
func (c *Client) GetRazorpayConfig(ctx context.Context) (*RazorpayConfig, error) {
	// Get Razorpay connection for this environment
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderRazorpay)
	if err != nil {
		return nil, ierr.NewError("failed to get Razorpay connection").
			WithHint("Razorpay connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	razorpayConfig, err := c.GetDecryptedRazorpayConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Razorpay configuration").
			WithHint("Invalid Razorpay configuration").
			Mark(ierr.ErrValidation)
	}

	// Validate required fields
	if razorpayConfig.KeyID == "" {
		c.logger.Errorw("missing Razorpay key ID",
			"connection_id", conn.ID,
			"environment_id", conn.EnvironmentID)
		return nil, ierr.NewError("missing Razorpay key ID").
			WithHint("Configure Razorpay key ID in the connection settings").
			Mark(ierr.ErrValidation)
	}

	if razorpayConfig.SecretKey == "" {
		c.logger.Errorw("missing Razorpay secret key",
			"connection_id", conn.ID,
			"environment_id", conn.EnvironmentID)
		return nil, ierr.NewError("missing Razorpay secret key").
			WithHint("Configure Razorpay secret key in the connection settings").
			Mark(ierr.ErrValidation)
	}

	return razorpayConfig, nil
}

// GetDecryptedRazorpayConfig decrypts and returns Razorpay configuration
func (c *Client) GetDecryptedRazorpayConfig(conn *connection.Connection) (*RazorpayConfig, error) {
	// Decrypt the connection metadata if it's encrypted
	decryptedMetadata, err := c.decryptConnectionMetadata(conn)
	if err != nil {
		return nil, err
	}

	// Extract Razorpay configuration from decrypted metadata
	razorpayConfig := &RazorpayConfig{}

	if keyID, exists := decryptedMetadata["key_id"]; exists {
		razorpayConfig.KeyID = keyID
	}

	if secretKey, exists := decryptedMetadata["secret_key"]; exists {
		razorpayConfig.SecretKey = secretKey
	}

	if webhookSecret, exists := decryptedMetadata["webhook_secret"]; exists {
		razorpayConfig.WebhookSecret = webhookSecret
	}

	return razorpayConfig, nil
}

// decryptConnectionMetadata decrypts the connection encrypted secret data
func (c *Client) decryptConnectionMetadata(conn *connection.Connection) (types.Metadata, error) {
	// Check if the connection has encrypted secret data
	if conn.EncryptedSecretData.Razorpay == nil {
		c.logger.Warnw("no razorpay metadata found in encrypted secret data", "connection_id", conn.ID)
		return types.Metadata{}, nil
	}

	// For Razorpay connections, decrypt the structured metadata
	if conn.ProviderType == types.SecretProviderRazorpay {
		if conn.EncryptedSecretData.Razorpay == nil {
			c.logger.Warnw("no razorpay metadata found", "connection_id", conn.ID)
			return types.Metadata{}, nil
		}

		// Decrypt each field
		keyID, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.Razorpay.KeyID)
		if err != nil {
			c.logger.Errorw("failed to decrypt key ID", "connection_id", conn.ID, "error", err)
			return nil, ierr.NewError("failed to decrypt key ID").Mark(ierr.ErrInternal)
		}

		secretKey, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.Razorpay.SecretKey)
		if err != nil {
			c.logger.Errorw("failed to decrypt secret key", "connection_id", conn.ID, "error", err)
			return nil, ierr.NewError("failed to decrypt secret key").Mark(ierr.ErrInternal)
		}

		// Decrypt webhook secret (optional field)
		var webhookSecret string
		if conn.EncryptedSecretData.Razorpay.WebhookSecret != "" {
			webhookSecret, err = c.encryptionService.Decrypt(conn.EncryptedSecretData.Razorpay.WebhookSecret)
			if err != nil {
				c.logger.Warnw("failed to decrypt webhook secret", "connection_id", conn.ID, "error", err)
				// Don't fail - webhook secret is optional
				webhookSecret = ""
			}
		}

		decryptedMetadata := types.Metadata{
			"key_id":         keyID,
			"secret_key":     secretKey,
			"webhook_secret": webhookSecret,
		}

		c.logger.Infow("successfully decrypted razorpay credentials",
			"connection_id", conn.ID,
			"has_key_id", keyID != "",
			"has_secret_key", secretKey != "",
			"has_webhook_secret", webhookSecret != "")

		return decryptedMetadata, nil
	}

	return types.Metadata{}, nil
}

// GetRazorpaySDKClient returns a configured Razorpay SDK client
func (c *Client) GetRazorpaySDKClient(ctx context.Context) (*razorpay.Client, *RazorpayConfig, error) {
	// Get Razorpay configuration
	config, err := c.GetRazorpayConfig(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Initialize Razorpay SDK client
	razorpayClient := razorpay.NewClient(config.KeyID, config.SecretKey)

	return razorpayClient, config, nil
}

// HasRazorpayConnection checks if the tenant has a Razorpay connection available
func (c *Client) HasRazorpayConnection(ctx context.Context) bool {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderRazorpay)
	return err == nil && conn != nil && conn.Status == types.StatusPublished
}

// GetConnection retrieves the Razorpay connection for the current context
func (c *Client) GetConnection(ctx context.Context) (*connection.Connection, error) {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderRazorpay)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get Razorpay connection").
			Mark(ierr.ErrDatabase)
	}
	if conn == nil {
		return nil, ierr.NewError("Razorpay connection not found").
			WithHint("Razorpay connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}
	return conn, nil
}

// CreateCustomer creates a customer in Razorpay
func (c *Client) CreateCustomer(ctx context.Context, customerData map[string]interface{}) (map[string]interface{}, error) {
	razorpayClient, _, err := c.GetRazorpaySDKClient(ctx)
	if err != nil {
		c.logger.Errorw("failed to get Razorpay client", "error", err)
		return nil, ierr.NewError("failed to initialize Razorpay client").
			WithHint("Unable to connect to Razorpay").
			Mark(ierr.ErrInternal)
	}

	razorpayCustomer, err := razorpayClient.Customer.Create(customerData, nil)
	if err != nil {
		c.logger.Errorw("failed to create customer in Razorpay", "error", err)
		return nil, ierr.NewError("failed to create customer in Razorpay").
			WithHint("Unable to create customer in Razorpay").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully created customer in Razorpay", "customer_id", razorpayCustomer["id"])
	return razorpayCustomer, nil
}

// CreatePaymentLink creates a payment link in Razorpay
func (c *Client) CreatePaymentLink(ctx context.Context, paymentLinkData map[string]interface{}) (map[string]interface{}, error) {
	razorpayClient, _, err := c.GetRazorpaySDKClient(ctx)
	if err != nil {
		c.logger.Errorw("failed to get Razorpay client", "error", err)
		return nil, ierr.NewError("failed to initialize Razorpay client").
			WithHint("Unable to connect to Razorpay").
			Mark(ierr.ErrInternal)
	}

	razorpayPaymentLink, err := razorpayClient.PaymentLink.Create(paymentLinkData, nil)
	// todo
	// lets make a struct from here
	if err != nil {
		c.logger.Errorw("failed to create payment link in Razorpay", "error", err)
		return nil, ierr.NewError("failed to create payment link in Razorpay").
			WithHint("Unable to create payment link in Razorpay").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully created payment link in Razorpay", "payment_link_id", razorpayPaymentLink["id"])
	return razorpayPaymentLink, nil
}

// VerifyWebhookSignature verifies the Razorpay webhook signature
func (c *Client) VerifyWebhookSignature(ctx context.Context, payload []byte, signature string) error {
	config, err := c.GetRazorpayConfig(ctx)
	if err != nil {
		c.logger.Errorw("failed to get Razorpay config for signature verification", "error", err)
		return ierr.NewError("failed to verify webhook signature").
			WithHint("Unable to verify Razorpay webhook signature").
			Mark(ierr.ErrInternal)
	}

	// Use webhook secret if available, otherwise fall back to API secret key
	// According to Razorpay docs, webhooks should use webhook secret
	secretForVerification := config.WebhookSecret
	if secretForVerification == "" {
		c.logger.Warnw("webhook secret not configured, using API secret key as fallback")
		secretForVerification = config.SecretKey
	}

	// Verify signature using HMAC SHA256
	// Razorpay uses HMAC SHA256 to sign the webhook body
	mac := hmac.New(sha256.New, []byte(secretForVerification))
	mac.Write(payload)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	if expectedSignature != signature {
		c.logger.Errorw("webhook signature mismatch",
			"expected_signature_length", len(expectedSignature),
			"received_signature_length", len(signature),
			"payload_length", len(payload),
			"using_webhook_secret", config.WebhookSecret != "")
		return ierr.NewError("webhook signature verification failed").
			WithHint("Invalid webhook signature").
			Mark(ierr.ErrValidation)
	}

	c.logger.Infow("webhook signature verified successfully",
		"using_webhook_secret", config.WebhookSecret != "")
	return nil
}

// CreateInvoice creates an invoice in Razorpay with inline line items
func (c *Client) CreateInvoice(ctx context.Context, invoiceData map[string]interface{}) (map[string]interface{}, error) {
	razorpayClient, _, err := c.GetRazorpaySDKClient(ctx)
	if err != nil {
		c.logger.Errorw("failed to get Razorpay client", "error", err)
		return nil, ierr.NewError("failed to initialize Razorpay client").
			WithHint("Unable to connect to Razorpay").
			Mark(ierr.ErrInternal)
	}

	razorpayInvoice, err := razorpayClient.Invoice.Create(invoiceData, nil)
	if err != nil {
		c.logger.Errorw("failed to create invoice in Razorpay", "error", err)
		return nil, ierr.NewError("failed to create invoice in Razorpay").
			WithHint("Unable to create invoice in Razorpay").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully created invoice in Razorpay",
		"invoice_id", razorpayInvoice["id"],
		"status", razorpayInvoice["status"])
	return razorpayInvoice, nil
}

// GetInvoice retrieves an invoice from Razorpay by ID
func (c *Client) GetInvoice(ctx context.Context, invoiceID string) (map[string]interface{}, error) {
	razorpayClient, _, err := c.GetRazorpaySDKClient(ctx)
	if err != nil {
		c.logger.Errorw("failed to get Razorpay client", "error", err)
		return nil, ierr.NewError("failed to initialize Razorpay client").
			WithHint("Unable to connect to Razorpay").
			Mark(ierr.ErrInternal)
	}

	razorpayInvoice, err := razorpayClient.Invoice.Fetch(invoiceID, nil, nil)
	if err != nil {
		c.logger.Errorw("failed to fetch invoice from Razorpay",
			"error", err,
			"invoice_id", invoiceID)
		return nil, ierr.NewError("failed to fetch invoice from Razorpay").
			WithHint("Unable to retrieve invoice from Razorpay").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": invoiceID,
				"error":      err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully fetched invoice from Razorpay",
		"invoice_id", invoiceID,
		"status", razorpayInvoice["status"])
	return razorpayInvoice, nil
}
