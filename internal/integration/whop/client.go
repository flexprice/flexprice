package whop

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

// WhopClient defines the interface for Whop API operations
type WhopClient interface {
	GetWhopConfig(ctx context.Context) (*WhopConfig, error)
	GetDecryptedWhopConfig(conn *connection.Connection) (*WhopConfig, error)
	HasWhopConnection(ctx context.Context) bool
	GetConnection(ctx context.Context) (*connection.Connection, error)
	UpdateProductID(ctx context.Context, productID string) error
	GetProduct(ctx context.Context, productID string) (*ProductResponse, error)
	CreateProduct(ctx context.Context, req CreateProductRequest) (*ProductResponse, error)
	GetPlan(ctx context.Context, planID string) (*PlanResponse, error)
	GetInvoice(ctx context.Context, whopInvoiceID string) (*InvoiceResponse, error)
	CreateInvoice(ctx context.Context, req CreateInvoiceRequest) (*InvoiceResponse, error)
	MarkInvoicePaid(ctx context.Context, whopInvoiceID string) error
}

// Client handles Whop API client setup and configuration
type Client struct {
	connectionRepo    connection.Repository
	encryptionService security.EncryptionService
	httpClient        httpclient.Client
	logger            *logger.Logger
}

// NewClient creates a new Whop client
func NewClient(
	connectionRepo connection.Repository,
	encryptionService security.EncryptionService,
	logger *logger.Logger,
) WhopClient {
	return &Client{
		connectionRepo:    connectionRepo,
		encryptionService: encryptionService,
		httpClient:        httpclient.NewDefaultClient(),
		logger:            logger,
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

	whopConfig, err := c.GetDecryptedWhopConfig(conn)
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
func (c *Client) GetDecryptedWhopConfig(conn *connection.Connection) (*WhopConfig, error) {
	if conn.ProviderType != types.SecretProviderWhop {
		return nil, ierr.NewError("invalid provider type").
			WithHint("Connection is not a Whop connection").
			Mark(ierr.ErrValidation)
	}
	if conn.EncryptedSecretData.Whop == nil {
		c.logger.Warnw("no whop metadata found", "connection_id", conn.ID)
		return &WhopConfig{}, nil
	}

	w := conn.EncryptedSecretData.Whop

	apiKey, err := c.encryptionService.Decrypt(w.APIKey)
	if err != nil {
		c.logger.Errorw("failed to decrypt Whop API key", "connection_id", conn.ID, "error", err)
		return nil, ierr.NewError("failed to decrypt Whop API key").Mark(ierr.ErrInternal)
	}

	companyID, err := c.encryptionService.Decrypt(w.CompanyID)
	if err != nil {
		c.logger.Errorw("failed to decrypt Whop company ID", "connection_id", conn.ID, "error", err)
		return nil, ierr.NewError("failed to decrypt Whop company ID").Mark(ierr.ErrInternal)
	}

	return &WhopConfig{
		APIKey:    apiKey,
		CompanyID: companyID,
		ProductID: w.ProductID,
	}, nil
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

	fullURL := fmt.Sprintf("%s%s", WhopBaseURL, endpoint)

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
		c.logger.Errorw("whop API request failed",
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
		c.logger.Errorw("whop API returned error",
			"status_code", resp.StatusCode,
			"method", method,
			"endpoint", endpoint)
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
			c.logger.Errorw("failed to unmarshal response", "error", err, "body", string(resp.Body))
			return ierr.NewError("failed to unmarshal response").
				WithHint("Invalid response from Whop").
				Mark(ierr.ErrInternal)
		}
	}

	return nil
}

// GetProduct fetches a product by ID from Whop
func (c *Client) GetProduct(ctx context.Context, productID string) (*ProductResponse, error) {
	c.logger.Infow("fetching product from Whop", "product_id", productID)

	var response ProductResponse
	if err := c.makeRequest(ctx, http.MethodGet, fmt.Sprintf("/v1/products/%s", productID), nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CreateProduct creates a new product in Whop
func (c *Client) CreateProduct(ctx context.Context, req CreateProductRequest) (*ProductResponse, error) {
	c.logger.Infow("creating product in Whop", "company_id", req.CompanyID, "title", req.Title)

	var response ProductResponse
	if err := c.makeRequest(ctx, http.MethodPost, "/v1/products", req, &response); err != nil {
		return nil, err
	}

	c.logger.Infow("successfully created product in Whop", "product_id", response.ID)
	return &response, nil
}

// GetPlan fetches a plan by ID from Whop
func (c *Client) GetPlan(ctx context.Context, planID string) (*PlanResponse, error) {
	c.logger.Infow("fetching plan from Whop", "plan_id", planID)

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

// CreateInvoice creates a one-time invoice in Whop with send_invoice collection method
func (c *Client) CreateInvoice(ctx context.Context, req CreateInvoiceRequest) (*InvoiceResponse, error) {
	c.logger.Infow("creating invoice in Whop",
		"company_id", req.CompanyID,
		"product_id", req.ProductID,
		"initial_price", req.Plan.InitialPrice)

	var response InvoiceResponse
	if err := c.makeRequest(ctx, http.MethodPost, "/v1/invoices", req, &response); err != nil {
		return nil, err
	}

	c.logger.Infow("successfully created invoice in Whop", "invoice_id", response.ID, "status", response.Status)
	return &response, nil
}

// MarkInvoicePaid marks a Whop invoice as paid via POST /v1/invoices/:id/mark_paid
func (c *Client) MarkInvoicePaid(ctx context.Context, whopInvoiceID string) error {
	c.logger.Infow("marking invoice as paid in Whop", "whop_invoice_id", whopInvoiceID)

	inv, err := c.GetInvoice(ctx, whopInvoiceID)
	if err != nil {
		return err
	}
	if inv.Status == "paid" {
		c.logger.Infow("Whop invoice already paid, skipping mark_paid", "whop_invoice_id", whopInvoiceID)
		return nil
	}

	if err := c.makeRequest(ctx, http.MethodPost, fmt.Sprintf("/v1/invoices/%s/mark_paid", whopInvoiceID), nil, nil); err != nil {
		return err
	}

	c.logger.Infow("successfully marked invoice as paid in Whop", "whop_invoice_id", whopInvoiceID)
	return nil
}
