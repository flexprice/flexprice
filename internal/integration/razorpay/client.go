package razorpay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
	razorpay "github.com/razorpay/razorpay-go"
	"github.com/samber/lo"
)

// RazorpayClient defines the interface for Razorpay API operations
type RazorpayClient interface {
	GetRazorpayConfig(ctx context.Context) (*RazorpayConfig, error)
	GetDecryptedRazorpayConfig(conn *connection.Connection) (*RazorpayConfig, error)
	GetRazorpaySDKClient(ctx context.Context) (*razorpay.Client, *RazorpayConfig, error)
	HasRazorpayConnection(ctx context.Context) bool
	GetConnection(ctx context.Context) (*connection.Connection, error)
	CreateCustomer(ctx context.Context, customerData map[string]interface{}) (map[string]interface{}, error)
	UpdateCustomer(ctx context.Context, customerID string, customerData map[string]interface{}) (map[string]interface{}, error)
	CreatePaymentLink(ctx context.Context, paymentLinkData map[string]interface{}) (map[string]interface{}, error)
	CreateInvoice(ctx context.Context, invoiceData map[string]interface{}) (map[string]interface{}, error)
	GetInvoice(ctx context.Context, invoiceID string) (map[string]interface{}, error)
	VerifyWebhookSignature(ctx context.Context, payload []byte, signature string) error
	GetCustomerTokens(ctx context.Context, razorpayCustomerID string) ([]map[string]interface{}, error)
	CreateAuthorizationLink(ctx context.Context, data map[string]interface{}) (map[string]interface{}, error)
	CreateOrder(ctx context.Context, orderData map[string]interface{}) (map[string]interface{}, error)
	CreateRecurringPayment(ctx context.Context, paymentData map[string]interface{}) (map[string]interface{}, error)
	// FetchOrdersByReceipt looks up Razorpay orders by receipt field (= FlexPrice invoiceID).
	// Returns the first matching order or ierr.ErrNotFound if none exist.
	FetchOrdersByReceipt(ctx context.Context, receipt string) (map[string]interface{}, error)
	// RefundPayment issues a full refund for a captured payment (POST /v1/payments/{id}/refund).
	// amountPaise must be in the smallest currency unit (e.g. paise for INR).
	RefundPayment(ctx context.Context, paymentID string, amountPaise int64) (map[string]interface{}, error)
	// FetchPayment retrieves a payment's current state from Razorpay (GET /v1/payments/{id}),
	// including whether it has already been refunded ("refunded" bool, "amount_refunded" int).
	FetchPayment(ctx context.Context, paymentID string) (map[string]interface{}, error)
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
		c.logger.Error(ctx, "missing Razorpay key ID",
			"error", err,
			"connection_id", conn.ID,
			"environment_id", conn.EnvironmentID)
		return nil, ierr.NewError("missing Razorpay key ID").
			WithHint("Configure Razorpay key ID in the connection settings").
			Mark(ierr.ErrValidation)
	}

	if razorpayConfig.SecretKey == "" {
		c.logger.Error(ctx, "missing Razorpay secret key",
			"error", err,
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
		c.logger.Info(context.Background(), "no razorpay metadata found in encrypted secret data", "connection_id", conn.ID)
		return types.Metadata{}, nil
	}

	// For Razorpay connections, decrypt the structured metadata
	if conn.ProviderType == types.SecretProviderRazorpay {
		if conn.EncryptedSecretData.Razorpay == nil {
			c.logger.Info(context.Background(), "no razorpay metadata found", "connection_id", conn.ID)
			return types.Metadata{}, nil
		}

		// Decrypt each field
		keyID, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.Razorpay.KeyID)
		if err != nil {
			c.logger.Error(context.Background(), "failed to decrypt key ID", "connection_id", conn.ID, "error", err)
			return nil, ierr.NewError("failed to decrypt key ID").Mark(ierr.ErrInternal)
		}

		secretKey, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.Razorpay.SecretKey)
		if err != nil {
			c.logger.Error(context.Background(), "failed to decrypt secret key", "connection_id", conn.ID, "error", err)
			return nil, ierr.NewError("failed to decrypt secret key").Mark(ierr.ErrInternal)
		}

		// Decrypt webhook secret (optional field)
		var webhookSecret string
		if conn.EncryptedSecretData.Razorpay.WebhookSecret != "" {
			webhookSecret, err = c.encryptionService.Decrypt(conn.EncryptedSecretData.Razorpay.WebhookSecret)
			if err != nil {
				c.logger.Info(context.Background(), "failed to decrypt webhook secret", "connection_id", conn.ID, "error", err)
				// Don't fail - webhook secret is optional
				webhookSecret = ""
			}
		}

		decryptedMetadata := types.Metadata{
			"key_id":         keyID,
			"secret_key":     secretKey,
			"webhook_secret": webhookSecret,
		}

		c.logger.Info(context.Background(), "successfully decrypted razorpay credentials",
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

	// Instrument the SDK's HTTP client so outbound Razorpay calls surface in
	// SigNoz External API Monitoring. We wrap the existing transport in place to
	// preserve the SDK's configured timeout.
	if razorpayClient.Request != nil && razorpayClient.Request.HTTPClient != nil {
		razorpayClient.Request.HTTPClient.Transport = httpclient.OtelTransport(razorpayClient.Request.HTTPClient.Transport)
	}

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
		c.logger.Error(ctx, "failed to get Razorpay client", "error", err)
		return nil, ierr.NewError("failed to initialize Razorpay client").
			WithHint("Unable to connect to Razorpay").
			Mark(ierr.ErrInternal)
	}

	razorpayCustomer, err := razorpayClient.Customer.Create(customerData, nil)
	if err != nil {
		c.logger.Error(ctx, "failed to create customer in Razorpay", "error", err)
		return nil, ierr.NewError("failed to create customer in Razorpay").
			WithHint("Unable to create customer in Razorpay").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Info(ctx, "successfully created customer in Razorpay", "customer_id", razorpayCustomer["id"])
	return razorpayCustomer, nil
}

// UpdateCustomer updates a customer in Razorpay.
func (c *Client) UpdateCustomer(ctx context.Context, customerID string, customerData map[string]interface{}) (map[string]interface{}, error) {
	razorpayClient, _, err := c.GetRazorpaySDKClient(ctx)
	if err != nil {
		return nil, ierr.NewError("failed to initialize Razorpay client").
			WithHint("Unable to connect to Razorpay").
			Mark(ierr.ErrInternal)
	}
	updatedCustomer, err := razorpayClient.Customer.Edit(customerID, customerData, nil)
	if err != nil {
		return nil, ierr.NewError("failed to update customer in Razorpay").
			WithHint("Unable to update customer in Razorpay").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
				"error":       err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}
	return updatedCustomer, nil
}

// CreatePaymentLink creates a payment link in Razorpay
func (c *Client) CreatePaymentLink(ctx context.Context, paymentLinkData map[string]interface{}) (map[string]interface{}, error) {
	razorpayClient, _, err := c.GetRazorpaySDKClient(ctx)
	if err != nil {
		c.logger.Error(ctx, "failed to get Razorpay client", "error", err)
		return nil, ierr.NewError("failed to initialize Razorpay client").
			WithHint("Unable to connect to Razorpay").
			Mark(ierr.ErrInternal)
	}

	razorpayPaymentLink, err := razorpayClient.PaymentLink.Create(paymentLinkData, nil)
	// todo
	// lets make a struct from here
	if err != nil {
		c.logger.Error(ctx, "failed to create payment link in Razorpay", "error", err)
		return nil, ierr.NewError("failed to create payment link in Razorpay").
			WithHint("Unable to create payment link in Razorpay").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Info(ctx, "successfully created payment link in Razorpay", "payment_link_id", razorpayPaymentLink["id"])
	return razorpayPaymentLink, nil
}

// VerifyWebhookSignature verifies the Razorpay webhook signature
func (c *Client) VerifyWebhookSignature(ctx context.Context, payload []byte, signature string) error {
	config, err := c.GetRazorpayConfig(ctx)
	if err != nil {
		c.logger.Error(ctx, "failed to get Razorpay config for signature verification", "error", err)
		return ierr.NewError("failed to verify webhook signature").
			WithHint("Unable to verify Razorpay webhook signature").
			Mark(ierr.ErrInternal)
	}

	// Use webhook secret if available, otherwise fall back to API secret key
	// According to Razorpay docs, webhooks should use webhook secret
	secretForVerification := config.WebhookSecret
	if secretForVerification == "" {
		c.logger.Info(ctx, "webhook secret not configured, using API secret key as fallback")
		secretForVerification = config.SecretKey
	}

	// Verify signature using HMAC SHA256
	// Razorpay uses HMAC SHA256 to sign the webhook body
	mac := hmac.New(sha256.New, []byte(secretForVerification))
	mac.Write(payload)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	if expectedSignature != signature {
		c.logger.Info(ctx, "webhook signature mismatch",
			"expected_signature_length", len(expectedSignature),
			"received_signature_length", len(signature),
			"payload_length", len(payload),
			"using_webhook_secret", config.WebhookSecret != "")
		return ierr.NewError("webhook signature verification failed").
			WithHint("Invalid webhook signature").
			Mark(ierr.ErrValidation)
	}

	c.logger.Info(ctx, "webhook signature verified successfully",
		"using_webhook_secret", config.WebhookSecret != "")
	return nil
}

// CreateInvoice creates an invoice in Razorpay with inline line items
func (c *Client) CreateInvoice(ctx context.Context, invoiceData map[string]interface{}) (map[string]interface{}, error) {
	razorpayClient, _, err := c.GetRazorpaySDKClient(ctx)
	if err != nil {
		c.logger.Error(ctx, "failed to get Razorpay client", "error", err)
		return nil, ierr.NewError("failed to initialize Razorpay client").
			WithHint("Unable to connect to Razorpay").
			Mark(ierr.ErrInternal)
	}

	razorpayInvoice, err := razorpayClient.Invoice.Create(invoiceData, nil)
	if err != nil {
		c.logger.Error(ctx, "failed to create invoice in Razorpay", "error", err)
		return nil, ierr.NewError("failed to create invoice in Razorpay").
			WithHint("Unable to create invoice in Razorpay").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Info(ctx, "successfully created invoice in Razorpay",
		"invoice_id", razorpayInvoice["id"],
		"status", razorpayInvoice["status"])
	return razorpayInvoice, nil
}

// GetInvoice retrieves an invoice from Razorpay by ID
func (c *Client) GetInvoice(ctx context.Context, invoiceID string) (map[string]interface{}, error) {
	razorpayClient, _, err := c.GetRazorpaySDKClient(ctx)
	if err != nil {
		c.logger.Error(ctx, "failed to get Razorpay client", "error", err)
		return nil, ierr.NewError("failed to initialize Razorpay client").
			WithHint("Unable to connect to Razorpay").
			Mark(ierr.ErrInternal)
	}

	razorpayInvoice, err := razorpayClient.Invoice.Fetch(invoiceID, nil, nil)
	if err != nil {
		c.logger.Error(ctx, "failed to fetch invoice from Razorpay",
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

	c.logger.Info(ctx, "successfully fetched invoice from Razorpay",
		"invoice_id", invoiceID,
		"status", razorpayInvoice["status"])
	return razorpayInvoice, nil
}

// sdkClient is a convenience wrapper that initialises the Razorpay SDK client
// and returns a ready-to-use *razorpay.Client or a pre-formatted error.
func (c *Client) sdkClient(ctx context.Context) (*razorpay.Client, error) {
	rc, _, err := c.GetRazorpaySDKClient(ctx)
	if err != nil {
		return nil, ierr.NewError("failed to initialize Razorpay client").
			WithHint("Unable to connect to Razorpay").
			Mark(ierr.ErrInternal)
	}
	return rc, nil
}

// GetCustomerTokens fetches all tokens registered against a Razorpay customer.
// GET /v1/customers/{id}/tokens — SDK: Token.All.
func (c *Client) GetCustomerTokens(ctx context.Context, razorpayCustomerID string) ([]map[string]interface{}, error) {
	rc, err := c.sdkClient(ctx)
	if err != nil {
		return nil, err
	}

	result, err := rc.Token.All(razorpayCustomerID, nil, nil)
	if err != nil {
		c.logger.Error(ctx, "failed to list Razorpay customer tokens", "error", err, "customer_id", razorpayCustomerID)
		return nil, ierr.NewError("failed to list Razorpay customer tokens").
			WithHint("Unable to list tokens from Razorpay").
			WithReportableDetails(map[string]interface{}{"customer_id": razorpayCustomerID, "error": err.Error()}).
			Mark(ierr.ErrInternal)
	}

	items, _ := result["items"].([]interface{})
	return lo.FilterMap(items, func(item interface{}, _ int) (map[string]interface{}, bool) {
		m, ok := item.(map[string]interface{})
		return m, ok
	}), nil
}

// CreateAuthorizationLink registers a UPI Autopay mandate combined with the
// first invoice payment. POST /v1/subscription_registration/auth_links — no
// dedicated SDK helper exists for this endpoint (confirmed against
// razorpay-go@v1.4.0 source), so this goes through the embedded raw request client.
func (c *Client) CreateAuthorizationLink(ctx context.Context, data map[string]interface{}) (map[string]interface{}, error) {
	rc, err := c.sdkClient(ctx)
	if err != nil {
		return nil, err
	}

	result, err := rc.Post("/v1/subscription_registration/auth_links", data, nil)
	if err != nil {
		c.logger.Error(ctx, "failed to create Razorpay authorization link", "error", err)
		return nil, ierr.NewError("failed to create Razorpay authorization link").
			WithHint("Unable to create authorization link in Razorpay").
			WithReportableDetails(map[string]interface{}{"error": err.Error()}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Info(ctx, "successfully created Razorpay authorization link", "id", result["id"])
	return result, nil
}

// CreateOrder creates a Razorpay Order for a subsequent recurring charge. POST /v1/orders.
func (c *Client) CreateOrder(ctx context.Context, orderData map[string]interface{}) (map[string]interface{}, error) {
	rc, err := c.sdkClient(ctx)
	if err != nil {
		return nil, err
	}

	result, err := rc.Order.Create(orderData, nil)
	if err != nil {
		c.logger.Error(ctx, "failed to create Razorpay order", "error", err)
		return nil, ierr.NewError("failed to create Razorpay order").
			WithHint("Unable to create order in Razorpay").
			WithReportableDetails(map[string]interface{}{"error": err.Error()}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Info(ctx, "successfully created Razorpay order", "order_id", result["id"])
	return result, nil
}

// CreateRecurringPayment charges a stored token against an Order. POST
// /v1/payments/create/recurring — SDK: Payment.CreateRecurringPayment (confirmed
// present in razorpay-go@v1.4.0's resources/payment.go).
func (c *Client) CreateRecurringPayment(ctx context.Context, paymentData map[string]interface{}) (map[string]interface{}, error) {
	rc, err := c.sdkClient(ctx)
	if err != nil {
		return nil, err
	}

	result, err := rc.Payment.CreateRecurringPayment(paymentData, nil)
	if err != nil {
		c.logger.Error(ctx, "failed to create Razorpay recurring payment", "error", err)
		return nil, ierr.NewError("failed to create Razorpay recurring payment").
			WithHint("Unable to charge the stored token in Razorpay").
			WithReportableDetails(map[string]interface{}{"error": err.Error()}).
			Mark(ierr.ErrInternal)
	}

	return result, nil
}

// FetchOrdersByReceipt fetches the first Razorpay order whose receipt matches the given string.
func (c *Client) FetchOrdersByReceipt(ctx context.Context, receipt string) (map[string]interface{}, error) {
	rc, err := c.sdkClient(ctx)
	if err != nil {
		return nil, err
	}

	queryParams := map[string]interface{}{
		"receipt": receipt,
		"count":   1,
	}
	result, err := rc.Order.All(queryParams, nil)
	if err != nil {
		c.logger.Error(ctx, "failed to fetch Razorpay orders by receipt",
			"error", err,
			"receipt", receipt)
		return nil, ierr.NewError("failed to fetch orders by receipt").
			WithHint("Unable to query Razorpay orders").
			WithReportableDetails(map[string]interface{}{"receipt": receipt, "error": err.Error()}).
			Mark(ierr.ErrInternal)
	}

	items, _ := result["items"].([]interface{})
	if len(items) == 0 {
		return nil, ierr.NewError("no order found with receipt").
			WithHint("No Razorpay order matches this receipt").
			WithReportableDetails(map[string]interface{}{"receipt": receipt}).
			Mark(ierr.ErrNotFound)
	}

	order, ok := items[0].(map[string]interface{})
	if !ok {
		return nil, ierr.NewError("invalid order response from Razorpay").
			Mark(ierr.ErrInternal)
	}

	return order, nil
}

// RefundPayment issues a refund for a captured Razorpay payment. POST
// /v1/payments/{id}/refund — SDK: Payment.Refund(paymentID, amount, data, headers).
func (c *Client) RefundPayment(ctx context.Context, paymentID string, amountPaise int64) (map[string]interface{}, error) {
	rc, err := c.sdkClient(ctx)
	if err != nil {
		return nil, err
	}

	result, err := rc.Payment.Refund(paymentID, int(amountPaise), nil, nil)
	if err != nil {
		c.logger.Error(ctx, "failed to refund Razorpay payment",
			"error", err, "payment_id", paymentID, "amount_paise", amountPaise)
		return nil, ierr.NewError("failed to refund Razorpay payment").
			WithHint("Unable to refund the payment in Razorpay").
			WithReportableDetails(map[string]interface{}{
				"payment_id":   paymentID,
				"amount_paise": amountPaise,
				"error":        err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	c.logger.Info(ctx, "successfully refunded Razorpay payment",
		"payment_id", paymentID, "refund_id", result["id"], "amount_paise", amountPaise)
	return result, nil
}

// FetchPayment retrieves a payment's current state from Razorpay. SDK: Payment.Fetch.
func (c *Client) FetchPayment(ctx context.Context, paymentID string) (map[string]interface{}, error) {
	rc, err := c.sdkClient(ctx)
	if err != nil {
		return nil, err
	}

	result, err := rc.Payment.Fetch(paymentID, nil, nil)
	if err != nil {
		c.logger.Error(ctx, "failed to fetch Razorpay payment", "error", err, "payment_id", paymentID)
		return nil, ierr.NewError("failed to fetch Razorpay payment").
			WithHint("Unable to retrieve the payment from Razorpay").
			WithReportableDetails(map[string]interface{}{
				"payment_id": paymentID,
				"error":      err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	return result, nil
}
