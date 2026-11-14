package openrouter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
)

type OpenRouterClient interface {
	Complete(ctx context.Context, req *Request) (*Response, error)
	DefaultModel() string
	IsConfigured() bool
}

type Client struct {
	config     config.OpenRouterConfig
	httpClient httpclient.Client
	logger     *logger.Logger
}

func NewClient(cfg *config.Configuration, logger *logger.Logger) OpenRouterClient {
	var orConfig config.OpenRouterConfig
	if cfg != nil {
		orConfig = cfg.OpenRouter
	}
	timeout := defaultTimeout
	if orConfig.TimeoutSeconds > 0 {
		timeout = time.Duration(orConfig.TimeoutSeconds) * time.Second
	}
	return newClient(orConfig, httpclient.NewProviderClientWithTimeout(timeout, logger, ProviderName), logger)
}

func newClient(cfg config.OpenRouterConfig, httpClient httpclient.Client, logger *logger.Logger) *Client {
	return &Client{config: cfg, httpClient: httpClient, logger: logger}
}

func (c *Client) Complete(ctx context.Context, req *Request) (*Response, error) {
	if req == nil || strings.TrimSpace(req.Model) == "" {
		return nil, ierr.NewError("model is required").
			WithHint("No AI model was selected").
			Mark(ierr.ErrValidation)
	}
	c.applyDefaults(req)

	var resp completionResponse
	if err := c.doRequest(ctx, http.MethodPost, chatCompletionsEndpoint, req, &resp); err != nil {
		return nil, err
	}
	return resp.toResponse()
}

func (c *Client) DefaultModel() string {
	return strings.TrimSpace(c.config.Model)
}

func (c *Client) IsConfigured() bool {
	return strings.TrimSpace(c.config.APIKey) != ""
}

func (c *Client) applyDefaults(req *Request) {
	if req.Provider == nil {
		req.Provider = c.privacyProvider(c.config.ZDR)
	}
	engine := strings.TrimSpace(c.config.PDFEngine)
	if engine != "" && len(req.Plugins) == 0 && req.hasFilePart() {
		req.Plugins = []Plugin{{ID: PluginFileParser, PDF: &PDFConfig{Engine: engine}}}
	}
}

func (c *Client) baseURL() string {
	if u := strings.TrimRight(strings.TrimSpace(c.config.BaseURL), "/"); u != "" {
		return u
	}
	return defaultBaseURL
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string, body any, out any) error {
	apiKey := strings.TrimSpace(c.config.APIKey)
	if apiKey == "" {
		return ierr.NewError("openrouter api key not configured").
			WithHint("AI contract extraction is not configured on this server").
			Mark(ierr.ErrServiceUnavailable)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Could not build the AI request").
			Mark(ierr.ErrInternal)
	}

	resp, err := c.httpClient.Send(ctx, &httpclient.Request{
		Method: method,
		URL:    c.baseURL() + endpoint,
		Headers: map[string]string{
			"Authorization": "Bearer " + apiKey,
			"Content-Type":  "application/json",
			"X-Title":       appTitle,
		},
		Body: payload,
	})
	if err != nil {
		if httpErr, ok := httpclient.IsHTTPError(err); ok {
			return c.upstreamError(method, endpoint, httpErr.StatusCode)
		}
		return ierr.WithError(err).
			WithHint("Unable to reach the AI provider").
			WithReportableDetails(map[string]interface{}{
				"method":   method,
				"endpoint": endpoint,
			}).
			Mark(ierr.ErrHTTPClient)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.upstreamError(method, endpoint, resp.StatusCode)
	}

	if out != nil && len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, out); err != nil {
			return ierr.WithError(err).
				WithHint("AI provider returned an unexpected response").
				Mark(ierr.ErrHTTPClient)
		}
	}
	return nil
}

func (c *Client) privacyProvider(zdr bool) *Provider {
	p := &Provider{DataCollection: dataCollectionDeny, RequireParameters: true}
	if zdr {
		p.ZDR = &zdr
	}
	return p
}

func PDFDataURL(pdf []byte) string {
	return DataURL("application/pdf", pdf)
}

func DataURL(mediaType string, data []byte) string {
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (c *Client) upstreamError(method, endpoint string, status int) error {
	details := map[string]interface{}{"method": method, "endpoint": endpoint, "status": status}
	b := ierr.NewError("openrouter api request failed").WithReportableDetails(details)

	switch status {
	case http.StatusTooManyRequests:
		return b.WithHint("AI provider rate limit exceeded. Wait and retry.").Mark(ierr.ErrTooManyRequests)
	case http.StatusPaymentRequired:
		return b.WithHint("AI provider credits are exhausted.").Mark(ierr.ErrServiceUnavailable)
	case http.StatusUnauthorized, http.StatusForbidden:
		return b.WithHint("AI provider rejected the server credentials.").Mark(ierr.ErrServiceUnavailable)
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		return b.WithHint("AI provider rejected the request. The document may be too large, or the model may not support PDFs or structured output.").Mark(ierr.ErrInvalidOperation)
	case http.StatusNotFound:
		return b.WithHint("No AI provider matches this model under the privacy settings. Try another model.").Mark(ierr.ErrInvalidOperation)
	default:
		return b.WithHintf("AI provider returned status %d", status).Mark(ierr.ErrHTTPClient)
	}
}
