package tabs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

// defaultBaseURL is the Tabs production integrators API base URL.
const defaultBaseURL = "https://integrators.prod.api.tabsplatform.com"

// TabsClient defines the interface for Tabs API operations.
type TabsClient interface {
	CreateCustomer(ctx context.Context, req CreateCustomerRequest) (*CreateCustomerResponse, error)
	CreateProduct(ctx context.Context, req CreateProductRequest) (*CreateProductResponse, error)
	CreateContract(ctx context.Context, req CreateContractRequest) (*CreateContractResponse, error)
	ListInvoicesByContract(ctx context.Context, contractID, issueDate string) (*ListInvoicesResponse, error)
	CreateObligation(ctx context.Context, contractID string, req CreateObligationRequest) (*CreateObligationResponse, error)
	DeleteObligation(ctx context.Context, contractID, obligationID string) error
	MarkContractProcessed(ctx context.Context, contractID string) error
	GetJob(ctx context.Context, jobID string) (*JobResponse, error)
	WaitForJob(ctx context.Context, jobID string) (*JobPayload, error)
	HasTabsConnection(ctx context.Context) bool
}

type Client struct {
	connectionRepo    connection.Repository
	encryptionService security.EncryptionService
	httpClient        *http.Client
	logger            *logger.Logger
}

// NewClient builds a Tabs API client. The API key is read (decrypted) per request from the
// tenant's published Tabs connection, resolved via the connection repository.
func NewClient(
	connectionRepo connection.Repository,
	encryptionService security.EncryptionService,
	logger *logger.Logger,
) TabsClient {
	return &Client{
		connectionRepo:    connectionRepo,
		encryptionService: encryptionService,
		httpClient:        &http.Client{Timeout: 30 * time.Second, Transport: httpclient.OtelTransport(nil)},
		logger:            logger,
	}
}

// HasTabsConnection reports whether the current environment has a published Tabs connection.
func (c *Client) HasTabsConnection(ctx context.Context) bool {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderTabs)
	return err == nil && conn != nil && conn.Status == types.StatusPublished
}

// apiKey resolves the current environment's Tabs connection and returns its decrypted API key.
func (c *Client) apiKey(ctx context.Context) (string, error) {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderTabs)
	if err != nil {
		return "", err
	}
	if conn == nil || conn.EncryptedSecretData.Tabs == nil || conn.EncryptedSecretData.Tabs.APIKey == "" {
		return "", ierr.NewError("tabs api key not configured").
			WithHint("Tabs connection is missing an api_key").
			Mark(ierr.ErrValidation)
	}
	return c.encryptionService.Decrypt(conn.EncryptedSecretData.Tabs.APIKey)
}

// doRequest performs an authenticated JSON request against the Tabs API.
func (c *Client) doRequest(ctx context.Context, method, path string, body any, out any) error {
	apiKey, err := c.apiKey(ctx)
	if err != nil {
		return err
	}

	var reqBody io.Reader
	if body != nil {
		b, mErr := json.Marshal(body)
		if mErr != nil {
			return mErr
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, defaultBaseURL+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ierr.WithError(err).WithHint("Tabs API request failed").Mark(ierr.ErrHTTPClient)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ierr.NewError("tabs api request failed").
			WithHintf("Tabs API returned status %d", resp.StatusCode).
			WithReportableDetails(map[string]interface{}{
				"status": resp.StatusCode,
				"path":   path,
				"body":   string(respBytes),
			}).
			Mark(ierr.ErrHTTPClient)
	}

	if out != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, out); err != nil {
			return err
		}
	}
	return nil
}

// CreateCustomer starts an async customer-creation job and returns its job id.
func (c *Client) CreateCustomer(ctx context.Context, req CreateCustomerRequest) (*CreateCustomerResponse, error) {
	var resp CreateCustomerResponse
	if err := c.doRequest(ctx, http.MethodPost, "/v3/customers", req, &resp); err != nil {
		return nil, err
	}
	if resp.Payload.JobID == "" {
		return nil, ierr.NewError("tabs did not return a job id").
			WithHint("Tabs create-customer response had no jobId").
			Mark(ierr.ErrSystem)
	}
	return &resp, nil
}

// CreateProduct creates a product in Tabs and returns its id. Product creation is synchronous,
// so the id is available directly in the response payload.
func (c *Client) CreateProduct(ctx context.Context, req CreateProductRequest) (*CreateProductResponse, error) {
	var resp CreateProductResponse
	if err := c.doRequest(ctx, http.MethodPost, "/v3/products", req, &resp); err != nil {
		return nil, err
	}
	if resp.Payload.ID == "" {
		return nil, ierr.NewError("tabs did not return a product id").
			WithHint("Tabs create-product response had no id").
			Mark(ierr.ErrSystem)
	}
	return &resp, nil
}

// CreateContract creates a contract in Tabs and returns its id. Contract creation is
// synchronous (shouldProcess=true), so the id is available in the response.
func (c *Client) CreateContract(ctx context.Context, req CreateContractRequest) (*CreateContractResponse, error) {
	var resp CreateContractResponse
	if err := c.doRequest(ctx, http.MethodPost, "/v3/contracts", req, &resp); err != nil {
		return nil, err
	}
	if resp.Payload.ID == "" {
		return nil, ierr.NewError("tabs did not return a contract id").
			WithHint("Tabs create-contract response had no id").
			Mark(ierr.ErrSystem)
	}
	return &resp, nil
}

// ListInvoicesByContract fetches Tabs invoices for a contract issued on a specific date.
// issueDate must be YYYY-MM-DD. Mirrors:
//
//	GET /v3/invoices?page=1&limit=50&filter=contractId:eq:"<id>",issueDate:eq:"<date>"
func (c *Client) ListInvoicesByContract(ctx context.Context, contractID, issueDate string) (*ListInvoicesResponse, error) {
	filter := fmt.Sprintf(`contractId:eq:"%s",issueDate:eq:"%s"`, contractID, issueDate)
	q := url.Values{}
	q.Set("page", "1")
	q.Set("limit", "50")
	q.Set("filter", filter)

	var resp ListInvoicesResponse
	if err := c.doRequest(ctx, http.MethodGet, "/v3/invoices?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateObligation creates an obligation (a charge) on a Tabs contract and returns its id.
func (c *Client) CreateObligation(ctx context.Context, contractID string, req CreateObligationRequest) (*CreateObligationResponse, error) {
	var resp CreateObligationResponse
	if err := c.doRequest(ctx, http.MethodPost, "/v3/contracts/"+contractID+"/obligations", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteObligation deletes an obligation on a Tabs contract. Tabs has no update endpoint for
// obligations, so changing one (e.g. after a draft invoice recomputes) means delete + recreate.
// The response body is a plain success message, so only the HTTP status matters.
func (c *Client) DeleteObligation(ctx context.Context, contractID, obligationID string) error {
	return c.doRequest(ctx, http.MethodDelete, "/v3/contracts/"+contractID+"/obligations/"+obligationID, nil, nil)
}

// MarkContractProcessed transitions a Tabs contract from NEW to PROCESSED. The response body is
// a plain success message, so only the HTTP status matters.
func (c *Client) MarkContractProcessed(ctx context.Context, contractID string) error {
	return c.doRequest(ctx, http.MethodPost, "/v3/contracts/"+contractID+"/actions",
		ContractActionRequest{Action: ContractActionMarkAsProcessed}, nil)
}

// GetJob returns the current state of a Tabs async job.
func (c *Client) GetJob(ctx context.Context, jobID string) (*JobResponse, error) {
	var resp JobResponse
	if err := c.doRequest(ctx, http.MethodGet, "/v3/jobs/"+jobID, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// jobPollAttempts / jobPollInterval bound how long WaitForJob polls a Tabs job.
// The activity's StartToCloseTimeout (5m) is the outer bound.
var (
	jobPollAttempts = 3
	jobPollInterval = 2 * time.Second
)

// WaitForJob polls a Tabs job until it succeeds, fails, or times out.
func (c *Client) WaitForJob(ctx context.Context, jobID string) (*JobPayload, error) {
	for attempt := 0; attempt < jobPollAttempts; attempt++ {
		job, err := c.GetJob(ctx, jobID)
		if err != nil {
			return nil, err
		}
		payload := job.Payload
		switch strings.ToUpper(payload.Status) {
		case "SUCCESS":
			return &payload, nil
		case "FAILED", "ERROR":
			return nil, ierr.NewError("tabs job failed").
				WithHintf("Tabs job %s reported status %s", jobID, payload.Status).
				WithReportableDetails(map[string]interface{}{"job_id": jobID, "status": payload.Status}).
				Mark(ierr.ErrSystem)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(jobPollInterval):
		}
	}
	return nil, ierr.NewError("tabs job timed out").
		WithHintf("Tabs job %s did not complete within the polling window", jobID).
		WithReportableDetails(map[string]interface{}{"job_id": jobID}).
		Mark(ierr.ErrSystem)
}
