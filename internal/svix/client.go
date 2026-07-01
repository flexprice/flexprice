package svix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/tracing"
	svix "github.com/svix/svix-webhooks/go"
	"github.com/svix/svix-webhooks/go/models"
)

// Client wraps the Svix SDK client
type Client struct {
	client  *svix.Svix
	baseURL string
	enabled bool
	sentry  *tracing.Service
}

// NewClient creates a new Svix client
func NewClient(config *config.Configuration, sentry *tracing.Service) (*Client, error) {
	if !config.Webhook.Svix.Enabled {
		return &Client{
			enabled: false,
			sentry:  sentry,
		}, nil
	}

	serverURL, err := url.Parse(config.Webhook.Svix.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	svixClient, err := svix.New(config.Webhook.Svix.AuthToken, &svix.SvixOptions{
		ServerUrl: serverURL,
		// Instrument outbound Svix API calls for SigNoz External API Monitoring.
		HTTPClient: httpclient.NewOtelHTTPClient(0),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create svix client: %w", err)
	}

	return &Client{
		client:  svixClient,
		baseURL: config.Webhook.Svix.BaseURL,
		enabled: true,
		sentry:  sentry,
	}, nil
}

// GetOrCreateApplication gets or creates a Svix application for the given tenant and environment
func (c *Client) GetOrCreateApplication(ctx context.Context, tenantID, environmentID string) (string, error) {
	if !c.enabled || c.client == nil {
		return "", nil
	}

	span, ctx := c.startSpan(ctx, "get_or_create_application", map[string]interface{}{
		"tenant_id":      tenantID,
		"environment_id": environmentID,
	})
	if span != nil {
		defer span.Finish()
	}

	appID := fmt.Sprintf("%s_%s", tenantID, environmentID)

	// Try to get existing application
	_, err := c.client.Application.Get(ctx, appID)
	if err == nil {
		if span != nil {
			span.Span.SetTag("svix.app_action", "get")
		}
		return appID, nil
	}

	// Create new application if it doesn't exist
	if span != nil {
		span.Span.SetTag("svix.app_action", "create")
	}
	app, err := c.client.Application.Create(ctx, models.ApplicationIn{
		Name: appID,
		Uid:  &appID,
	}, &svix.ApplicationCreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create application: %w", err)
	}

	return app.Id, nil
}

// GetDashboardURL returns the Svix app-portal access url and token for the
// given application. Hosted Svix returns a real usable portal `url`; Svix OSS
// returns a docs stub url plus the app-scoped JWT `token`, consumed by
// svix-react in the browser. Callers should branch on the url to decide
// which to use.
func (c *Client) GetDashboardURL(ctx context.Context, applicationID string) (url string, token string, err error) {
	if !c.enabled || c.client == nil {
		return "", "", nil
	}

	span, ctx := c.startSpan(ctx, "get_dashboard_url", map[string]interface{}{
		"application_id": applicationID,
	})
	if span != nil {
		defer span.Finish()
	}

	dashboard, err := c.client.Authentication.AppPortalAccess(ctx, applicationID, models.AppPortalAccessIn{}, &svix.AuthenticationAppPortalAccessOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to get dashboard access: %w", err)
	}

	return dashboard.Url, dashboard.Token, nil
}

// SendMessage sends a webhook message to the given application.
// Returns the Svix message id on success (empty string when Svix is disabled or the app doesn't exist).
func (c *Client) SendMessage(ctx context.Context, applicationID string, eventType string, payload interface{}) (string, error) {
	if !c.enabled || c.client == nil {
		return "", nil
	}

	span, ctx := c.startSpan(ctx, "send_message", map[string]interface{}{
		"application_id": applicationID,
		"event_type":     eventType,
	})
	if span != nil {
		defer span.Finish()
	}

	var payloadMap map[string]interface{}

	// Handle different payload types
	switch p := payload.(type) {
	case map[string]interface{}:
		payloadMap = p
	case []byte:
		if err := json.Unmarshal(p, &payloadMap); err != nil {
			return "", fmt.Errorf("failed to unmarshal payload: %w", err)
		}
	case json.RawMessage:
		if err := json.Unmarshal(p, &payloadMap); err != nil {
			return "", fmt.Errorf("failed to unmarshal payload: %w", err)
		}
	default:
		data, err := json.Marshal(p)
		if err != nil {
			return "", fmt.Errorf("failed to marshal payload: %w", err)
		}
		if err := json.Unmarshal(data, &payloadMap); err != nil {
			return "", fmt.Errorf("failed to unmarshal payload: %w", err)
		}
	}

	payloadMap["event_type"] = eventType
	out, err := c.client.Message.Create(ctx, applicationID, models.MessageIn{
		EventType: eventType,
		Payload:   payloadMap,
	}, &svix.MessageCreateOptions{})
	if err != nil {
		if err.Error() == "application not found" {
			return "", nil
		}
		c.captureException(ctx, err)
		return "", fmt.Errorf("failed to send message: %w", err)
	}
	if out == nil {
		return "", nil
	}
	return out.Id, nil
}

func (c *Client) startSpan(ctx context.Context, operation string, params map[string]interface{}) (*tracing.SpanFinisher, context.Context) {
	if c.sentry == nil {
		return nil, ctx
	}
	span, ctx := c.sentry.StartSvixSpan(ctx, operation, params)
	if span == nil {
		return nil, ctx
	}
	return &tracing.SpanFinisher{Span: span}, ctx
}

func (c *Client) captureException(ctx context.Context, err error) {
	if c.sentry != nil {
		c.sentry.CaptureException(ctx, err)
	}
}
