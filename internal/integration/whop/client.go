package whop

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

// WhopClient defines the interface for Whop API operations
type WhopClient interface {
	GetWhopConfig(ctx context.Context) (*WhopConfig, error)
	GetDecryptedWhopConfig(ctx context.Context, conn *connection.Connection) (*WhopConfig, error)
	HasWhopConnection(ctx context.Context) bool
	GetConnection(ctx context.Context) (*connection.Connection, error)
	UpdateProductID(ctx context.Context, productID string) error
	GetProduct(ctx context.Context, productID string) (*ProductResponse, error)
	CreateProduct(ctx context.Context, req CreateProductRequest) (*ProductResponse, error)
	GetPlan(ctx context.Context, planID string) (*PlanResponse, error)
	GetInvoice(ctx context.Context, whopInvoiceID string) (*InvoiceResponse, error)
	CreateInvoice(ctx context.Context, req CreateInvoiceRequest) (*InvoiceResponse, error)
	MarkInvoicePaid(ctx context.Context, whopInvoiceID string) error
	GetPaymentMethods(ctx context.Context, memberID string) (*PaymentMethodsResponse, error)
	// VerifyWebhookSignature verifies a Whop webhook per the Standard Webhooks spec:
	// https://docs.whop.com/developer/guides/webhooks
	VerifyWebhookSignature(ctx context.Context, payload []byte, webhookID, timestamp, signature string) error
}

// Client handles Whop API client setup and configuration
type Client struct {
	connectionRepo    connection.Repository
	encryptionService security.EncryptionService
	httpClient        httpclient.Client
	logger            *logger.Logger
	cfg               *config.Configuration
}

// NewClient creates a new Whop client
func NewClient(
	connectionRepo connection.Repository,
	encryptionService security.EncryptionService,
	logger *logger.Logger,
	cfg *config.Configuration,
) WhopClient {
	return &Client{
		connectionRepo:    connectionRepo,
		encryptionService: encryptionService,
		httpClient:        httpclient.NewDefaultClient(),
		logger:            logger,
		cfg:               cfg,
	}
}

// GetWhopConfig retrieves and decrypts Whop configuration for the current environment
func (c *Client) GetWhopConfig(ctx context.Context) (*WhopConfig, error) {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderWhop)
	if err != nil {
		return nil, ierr.NewError("failed to get Whop connection").
			WithHint("Whop connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	if conn == nil {
		return nil, ierr.NewError("Whop connection not found").
			WithHint("Whop connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	whopConfig, err := c.GetDecryptedWhopConfig(ctx, conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Whop configuration").
			WithHint("Invalid Whop configuration").
			Mark(ierr.ErrValidation)
	}

	if whopConfig.APIKey == "" {
		return nil, ierr.NewError("missing Whop API key").
			WithHint("Configure Whop API key in the connection settings").
			Mark(ierr.ErrValidation)
	}
	if whopConfig.CompanyID == "" {
		return nil, ierr.NewError("missing Whop company ID").
			WithHint("Configure Whop company ID in the connection settings").
			Mark(ierr.ErrValidation)
	}

	return whopConfig, nil
}

// GetDecryptedWhopConfig decrypts and returns Whop configuration
func (c *Client) GetDecryptedWhopConfig(ctx context.Context, conn *connection.Connection) (*WhopConfig, error) {
	if conn.ProviderType != types.SecretProviderWhop {
		return nil, ierr.NewError("invalid provider type").
			WithHint("Connection is not a Whop connection").
			Mark(ierr.ErrValidation)
	}
	if conn.EncryptedSecretData.Whop == nil {
		c.logger.Info(ctx, "no whop metadata found", "connection_id", conn.ID)
		return &WhopConfig{}, nil
	}

	w := conn.EncryptedSecretData.Whop

	apiKey, err := c.encryptionService.Decrypt(w.APIKey)
	if err != nil {
		c.logger.Error(ctx, "failed to decrypt Whop API key", "connection_id", conn.ID, "error", err)
		return nil, ierr.NewError("failed to decrypt Whop API key").Mark(ierr.ErrInternal)
	}

	companyID, err := c.encryptionService.Decrypt(w.CompanyID)
	if err != nil {
		c.logger.Error(ctx, "failed to decrypt Whop company ID", "connection_id", conn.ID, "error", err)
		return nil, ierr.NewError("failed to decrypt Whop company ID").Mark(ierr.ErrInternal)
	}

	var webhookSecret string
	if w.WebhookSecret != "" {
		webhookSecret, err = c.encryptionService.Decrypt(w.WebhookSecret)
		if err != nil {
			c.logger.Error(ctx, "failed to decrypt Whop webhook secret", "connection_id", conn.ID, "error", err)
			// Don't fail - webhook secret is optional for non-webhook flows
			webhookSecret = ""
		}
	}

	return &WhopConfig{
		APIKey:        apiKey,
		CompanyID:     companyID,
		ProductID:     w.ProductID,
		WebhookSecret: webhookSecret,
	}, nil
}

// whopSignatureTolerance is the maximum allowed difference between the
// webhook-timestamp header and the current time, guarding against replay attacks.
// Whop's documented retry policy (initial attempt plus 3 retries at 10s/20s/40s)
// finishes in under 90s, so 5 minutes gives legitimate deliveries a wide margin
// while still bounding how long a captured request stays replayable.
const whopSignatureTolerance = 5 * time.Minute

// VerifyWebhookSignature verifies a Whop webhook per the Standard Webhooks spec
// (https://docs.whop.com/developer/guides/webhooks, https://github.com/standard-webhooks/standard-webhooks):
// the signed content is "{webhook-id}.{webhook-timestamp}.{raw_body}", HMAC-SHA256'd
// with the base64-decoded webhook secret, base64-encoded, and compared against the
// "v1,<signature>" entries in the webhook-signature header.
func (c *Client) VerifyWebhookSignature(ctx context.Context, payload []byte, webhookID, timestamp, signature string) error {
	if webhookID == "" || timestamp == "" || signature == "" {
		return ierr.NewError("missing webhook signature headers").
			WithHint("Whop webhooks must include webhook-id, webhook-timestamp and webhook-signature headers").
			Mark(ierr.ErrValidation)
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ierr.NewError("invalid webhook-timestamp header").
			WithHint("webhook-timestamp must be a unix timestamp in seconds").
			Mark(ierr.ErrValidation)
	}
	if age := time.Since(time.Unix(ts, 0)); age > whopSignatureTolerance || age < -whopSignatureTolerance {
		return ierr.NewError("webhook timestamp outside of tolerance").
			WithHint("Request may be a replay attack or the sender's clock is skewed").
			Mark(ierr.ErrValidation)
	}

	config, err := c.GetWhopConfig(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to load Whop config for webhook verification").
			Mark(ierr.ErrInternal)
	}

	if config.WebhookSecret == "" {
		return ierr.NewError("webhook_secret not configured in Whop connection").
			WithHint("Set webhook_secret in the Whop connection encrypted_secret_data").
			Mark(ierr.ErrValidation)
	}

	secretKey, err := decodeWhopWebhookSecret(config.WebhookSecret)
	if err != nil {
		return ierr.WithError(err).
			WithHint("webhook_secret must be the base64-encoded secret from the Whop dashboard").
			Mark(ierr.ErrValidation)
	}

	signedContent := fmt.Sprintf("%s.%s.%s", webhookID, timestamp, payload)
	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(signedContent))
	expectedSignature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	for _, entry := range strings.Fields(signature) {
		version, sig, ok := strings.Cut(entry, ",")
		if !ok || version != "v1" {
			continue
		}
		if hmac.Equal([]byte(sig), []byte(expectedSignature)) {
			return nil
		}
	}

	return ierr.NewError("webhook signature mismatch").
		WithHint("Request may have been tampered with or webhook_secret is incorrect").
		Mark(ierr.ErrValidation)
}

// decodeWhopWebhookSecret strips the conventional "whsec_" prefix (if present) and
// base64-decodes the Whop webhook secret, per the Standard Webhooks spec.
func decodeWhopWebhookSecret(secret string) ([]byte, error) {
	secret = strings.TrimPrefix(secret, "whsec_")
	decoded, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return nil, ierr.NewError("invalid webhook secret encoding").Mark(ierr.ErrValidation)
	}
	return decoded, nil
}

// HasWhopConnection checks if the tenant has a Whop connection available
func (c *Client) HasWhopConnection(ctx context.Context) bool {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderWhop)
	return err == nil && conn != nil && conn.Status == types.StatusPublished
}

// GetConnection retrieves the Whop connection for the current context
func (c *Client) GetConnection(ctx context.Context) (*connection.Connection, error) {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderWhop)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get Whop connection").
			Mark(ierr.ErrDatabase)
	}
	if conn == nil {
		return nil, ierr.NewError("Whop connection not found").
			WithHint("Whop connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}
	return conn, nil
}

// UpdateProductID persists a newly-created Whop product ID back to the connection.
// product_id is stored in plain text (not sensitive).
func (c *Client) UpdateProductID(ctx context.Context, productID string) error {
	conn, err := c.GetConnection(ctx)
	if err != nil {
		return err
	}
	if conn.EncryptedSecretData.Whop == nil {
		return ierr.NewError("whop metadata not found on connection").Mark(ierr.ErrInternal)
	}

	conn.EncryptedSecretData.Whop.ProductID = productID
	return c.connectionRepo.Update(ctx, conn)
}

// makeRequest makes a generic HTTP request to the Whop API
func (c *Client) makeRequest(ctx context.Context, method, endpoint string, body interface{}, response interface{}) error {
	config, err := c.GetWhopConfig(ctx)
	if err != nil {
		return err
	}

	baseURL := WhopBaseURL
	if c.cfg != nil && c.cfg.Whop.BaseURL != "" {
		baseURL = c.cfg.Whop.BaseURL
	}
	fullURL := fmt.Sprintf("%s%s", baseURL, endpoint)

	var jsonBody []byte
	if body != nil {
		jsonBody, err = json.Marshal(body)
		if err != nil {
			c.logger.Error(ctx, "failed to marshal request body", "error", err)
			return ierr.NewError("failed to marshal request body").
				WithHint("Invalid request data").
				Mark(ierr.ErrInternal)
		}
	}

	httpReq := &httpclient.Request{
		Method: method,
		URL:    fullURL,
		Headers: map[string]string{
			"Authorization": fmt.Sprintf("Bearer %s", config.APIKey),
			"Content-Type":  "application/json",
			"Accept":        "application/json",
		},
		Body: jsonBody,
	}

	resp, err := c.httpClient.Send(ctx, httpReq)
	if err != nil {
		whopBody := ""
		if httpErr, ok := httpclient.IsHTTPError(err); ok {
			whopBody = string(httpErr.Response)
		}
		c.logger.Error(ctx, "whop API request failed",
			"error", err,
			"method", method,
			"endpoint", endpoint,
			"url", fullURL,
			"whop_response", whopBody)
		return ierr.NewError("failed to make request to Whop API").
			WithHint("Unable to connect to Whop").
			WithReportableDetails(map[string]interface{}{
				"method":   method,
				"endpoint": endpoint,
			}).
			Mark(ierr.ErrHTTPClient)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logger.Info(ctx, "whop API returned error",
			"status_code", resp.StatusCode,
			"method", method,
			"endpoint", endpoint,
			"whop_response", string(resp.Body))
		return ierr.NewError("Whop API returned error").
			WithHint(fmt.Sprintf("Whop API returned status %d", resp.StatusCode)).
			WithReportableDetails(map[string]interface{}{
				"status_code": resp.StatusCode,
				"method":      method,
				"endpoint":    endpoint,
			}).
			Mark(ierr.ErrHTTPClient)
	}

	if response != nil && len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, response); err != nil {
			c.logger.Error(ctx, "failed to unmarshal response", "error", err, "body", string(resp.Body))
			return ierr.NewError("failed to unmarshal response").
				WithHint("Invalid response from Whop").
				Mark(ierr.ErrInternal)
		}
	}

	return nil
}

// GetProduct fetches a product by ID from Whop
func (c *Client) GetProduct(ctx context.Context, productID string) (*ProductResponse, error) {
	c.logger.Info(ctx, "fetching product from Whop", "product_id", productID)

	var response ProductResponse
	if err := c.makeRequest(ctx, http.MethodGet, fmt.Sprintf("/v1/products/%s", productID), nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CreateProduct creates a new product in Whop
func (c *Client) CreateProduct(ctx context.Context, req CreateProductRequest) (*ProductResponse, error) {
	c.logger.Info(ctx, "creating product in Whop", "company_id", req.CompanyID, "title", req.Title)

	var response ProductResponse
	if err := c.makeRequest(ctx, http.MethodPost, "/v1/products", req, &response); err != nil {
		return nil, err
	}

	c.logger.Info(ctx, "successfully created product in Whop", "product_id", response.ID)
	return &response, nil
}

// GetPlan fetches a plan by ID from Whop
func (c *Client) GetPlan(ctx context.Context, planID string) (*PlanResponse, error) {
	c.logger.Info(ctx, "fetching plan from Whop", "plan_id", planID)

	var response PlanResponse
	if err := c.makeRequest(ctx, http.MethodGet, fmt.Sprintf("/v1/plans/%s", planID), nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// GetInvoice fetches a Whop invoice by ID
func (c *Client) GetInvoice(ctx context.Context, whopInvoiceID string) (*InvoiceResponse, error) {
	var response InvoiceResponse
	if err := c.makeRequest(ctx, http.MethodGet, fmt.Sprintf("/v1/invoices/%s", whopInvoiceID), nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CreateInvoice creates a one-time invoice in Whop
func (c *Client) CreateInvoice(ctx context.Context, req CreateInvoiceRequest) (*InvoiceResponse, error) {
	c.logger.Info(ctx, "creating invoice in Whop",
		"company_id", req.CompanyID,
		"product_id", req.ProductID,
		"initial_price", req.Plan.InitialPrice)

	var response InvoiceResponse
	if err := c.makeRequest(ctx, http.MethodPost, "/v1/invoices", req, &response); err != nil {
		return nil, err
	}

	c.logger.Info(ctx, "successfully created invoice in Whop", "invoice_id", response.ID, "status", response.Status)
	return &response, nil
}

// GetPaymentMethods fetches saved payment methods for a Whop member via GET /v1/payment_methods?member_id=xxx
func (c *Client) GetPaymentMethods(ctx context.Context, memberID string) (*PaymentMethodsResponse, error) {
	c.logger.Debug(ctx, "fetching payment methods from Whop")

	var response PaymentMethodsResponse
	if err := c.makeRequest(ctx, http.MethodGet, fmt.Sprintf("/v1/payment_methods?member_id=%s", memberID), nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// MarkInvoicePaid marks a Whop invoice as paid via POST /v1/invoices/:id/mark_paid
func (c *Client) MarkInvoicePaid(ctx context.Context, whopInvoiceID string) error {
	c.logger.Info(ctx, "marking invoice as paid in Whop", "whop_invoice_id", whopInvoiceID)

	inv, err := c.GetInvoice(ctx, whopInvoiceID)
	if err != nil {
		return err
	}
	if inv.Status == WhopInvoiceStatusPaid {
		c.logger.Info(ctx, "Whop invoice already paid, skipping mark_paid", "whop_invoice_id", whopInvoiceID)
		return nil
	}

	if err := c.makeRequest(ctx, http.MethodPost, fmt.Sprintf("/v1/invoices/%s/mark_paid", whopInvoiceID), nil, nil); err != nil {
		return err
	}

	c.logger.Info(ctx, "successfully marked invoice as paid in Whop", "whop_invoice_id", whopInvoiceID)
	return nil
}
