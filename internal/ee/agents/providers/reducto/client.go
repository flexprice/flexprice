package reducto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
)

type ReductoClient interface {
	Upload(ctx context.Context, fileName, mediaType string, data []byte) (*UploadResponse, error)
	Extract(ctx context.Context, req *ExtractRequest) (*ExtractResponse, error)
	IsConfigured() bool
}

type Client struct {
	config     config.ReductoConfig
	httpClient httpclient.Client
	logger     *logger.Logger
}

func NewClient(cfg *config.Configuration, logger *logger.Logger) ReductoClient {
	var reductoConfig config.ReductoConfig
	if cfg != nil {
		reductoConfig = cfg.Reducto
	}
	timeout := defaultTimeout
	if reductoConfig.TimeoutSeconds > 0 {
		timeout = time.Duration(reductoConfig.TimeoutSeconds) * time.Second
	}
	return newClient(reductoConfig, httpclient.NewProviderClientWithTimeout(timeout, logger, ProviderName), logger)
}

func newClient(cfg config.ReductoConfig, httpClient httpclient.Client, logger *logger.Logger) *Client {
	return &Client{config: cfg, httpClient: httpClient, logger: logger}
}

func (c *Client) IsConfigured() bool {
	return strings.TrimSpace(c.config.APIKey) != ""
}

func (c *Client) Upload(ctx context.Context, fileName, mediaType string, data []byte) (*UploadResponse, error) {
	if len(data) == 0 {
		return nil, ierr.NewError("file is required").
			WithHint("Upload a document").
			Mark(ierr.ErrValidation)
	}
	body, contentType, err := c.multipartBody(fileName, mediaType, data)
	if err != nil {
		return nil, err
	}

	var resp UploadResponse
	if err := c.doRequest(ctx, http.MethodPost, uploadEndpoint, contentType, body, &resp); err != nil {
		return nil, err
	}
	if resp.FileID == "" {
		return nil, ierr.NewError("reducto did not return a file id").
			WithHint("The document service returned an unexpected response").
			Mark(ierr.ErrHTTPClient)
	}
	return &resp, nil
}

func (c *Client) Extract(ctx context.Context, req *ExtractRequest) (*ExtractResponse, error) {
	if req == nil || strings.TrimSpace(req.Input) == "" {
		return nil, ierr.NewError("input is required").
			WithHint("No document was given to extract from").
			Mark(ierr.ErrValidation)
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Could not build the extraction request").
			Mark(ierr.ErrInternal)
	}

	var resp ExtractResponse
	if err := c.doRequest(ctx, http.MethodPost, extractEndpoint, "application/json", payload, &resp); err != nil {
		return nil, err
	}
	if len(resp.Result) == 0 {
		return nil, ierr.NewError("reducto returned no result").
			WithHint("The document service returned an empty result").
			Mark(ierr.ErrHTTPClient)
	}
	return &resp, nil
}

func (c *Client) multipartBody(fileName, mediaType string, data []byte) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="`+uploadFormField+`"; filename="`+c.quoteFileName(fileName)+`"`)
	header.Set("Content-Type", mediaType)
	part, err := w.CreatePart(header)
	if err == nil {
		_, err = part.Write(data)
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		return nil, "", ierr.WithError(err).
			WithHint("Could not prepare the document upload").
			Mark(ierr.ErrInternal)
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

func (c *Client) quoteFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "document"
	}
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", "", "\n", "").Replace(name)
}

func (c *Client) baseURL() string {
	if u := strings.TrimRight(strings.TrimSpace(c.config.BaseURL), "/"); u != "" {
		return u
	}
	return defaultBaseURL
}

func (c *Client) doRequest(ctx context.Context, method, endpoint, contentType string, body []byte, out any) error {
	apiKey := strings.TrimSpace(c.config.APIKey)
	if apiKey == "" {
		return ierr.NewError("reducto api key not configured").
			WithHint("AI contract extraction is not configured on this server").
			Mark(ierr.ErrServiceUnavailable)
	}

	resp, err := c.httpClient.Send(ctx, &httpclient.Request{
		Method: method,
		URL:    c.baseURL() + endpoint,
		Headers: map[string]string{
			"Authorization": "Bearer " + apiKey,
			"Content-Type":  contentType,
			"Accept":        "application/json",
		},
		Body: body,
	})
	if err != nil {
		if httpErr, ok := httpclient.IsHTTPError(err); ok {
			return c.upstreamError(method, endpoint, httpErr.StatusCode, httpErr.Response)
		}
		return c.transportError(method, endpoint, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.upstreamError(method, endpoint, resp.StatusCode, resp.Body)
	}

	if out != nil && len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, out); err != nil {
			return ierr.WithError(err).
				WithHint("The document service returned an unexpected response").
				Mark(ierr.ErrHTTPClient)
		}
	}
	return nil
}

func (c *Client) upstreamError(method, endpoint string, status int, body []byte) error {
	details := map[string]interface{}{"method": method, "endpoint": endpoint, "status": status}
	var er errorResponse
	if json.Unmarshal(body, &er) == nil && len(er.Detail) > 0 {
		details["detail"] = string(er.Detail)
	}
	b := ierr.NewError("reducto api request failed").WithReportableDetails(details)

	switch status {
	case http.StatusTooManyRequests:
		return b.WithHint("The document service is rate limiting requests. Wait and retry.").Mark(ierr.ErrTooManyRequests)
	case http.StatusPaymentRequired:
		return b.WithHint("The document service has no credits left.").Mark(ierr.ErrServiceUnavailable)
	case http.StatusUnauthorized, http.StatusForbidden:
		return b.WithHint("The document service rejected the server credentials.").Mark(ierr.ErrServiceUnavailable)
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return b.WithHint("The document service could not process this document.").Mark(ierr.ErrInvalidOperation)
	default:
		return b.WithHintf("The document service returned status %d", status).Mark(ierr.ErrHTTPClient)
	}
}

func (c *Client) transportError(method, endpoint string, err error) error {
	details := map[string]interface{}{"method": method, "endpoint": endpoint, "cause": err.Error()}
	if c.isTimeout(err) {
		return ierr.NewError("reducto request timed out").
			WithHint("The document service took too long to respond. Try again.").
			WithReportableDetails(details).
			Mark(ierr.ErrHTTPClient)
	}
	return ierr.NewError("reducto request failed").
		WithHint("Unable to reach the document service").
		WithReportableDetails(details).
		Mark(ierr.ErrHTTPClient)
}

func (c *Client) isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
