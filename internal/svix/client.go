package svix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/flexprice/flexprice/internal/config"
	svix "github.com/svix/svix-webhooks/go"
	"github.com/svix/svix-webhooks/go/models"
)

// Client wraps the Svix SDK client
type Client struct {
	client  *svix.Svix
	baseURL string
	enabled bool
}

// NewClient creates a new Svix client
func NewClient(config *config.Configuration) (*Client, error) {
	if !config.Webhook.Svix.Enabled {
		return &Client{
			enabled: false,
		}, nil
	}

	serverURL, err := url.Parse(config.Webhook.Svix.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	svixClient, err := svix.New(config.Webhook.Svix.AuthToken, &svix.SvixOptions{
		ServerUrl: serverURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create svix client: %w", err)
	}

	return &Client{
		client:  svixClient,
		baseURL: config.Webhook.Svix.BaseURL,
		enabled: true,
	}, nil
}

// GetOrCreateApplication gets or creates a Svix application for the given tenant and environment
func (c *Client) GetOrCreateApplication(ctx context.Context, tenantID, environmentID string) (string, error) {
	if !c.enabled || c.client == nil {
		return "", nil // Return nil if Svix is not enabled
	}

	appID := fmt.Sprintf("%s_%s", tenantID, environmentID)

	// Try to get existing application
	_, err := c.client.Application.Get(ctx, appID)
	if err == nil {
		return appID, nil
	}

	// Create new application if it doesn't exist
	app, err := c.client.Application.Create(ctx, models.ApplicationIn{
		Name: appID,
		Uid:  &appID,
	}, &svix.ApplicationCreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create application: %w", err)
	}

	return app.Id, nil
}

// GetDashboardURL gets the dashboard URL for the given application
func (c *Client) GetDashboardURL(ctx context.Context, applicationID string) (string, error) {
	if !c.enabled || c.client == nil {
		return "", nil // Return nil if Svix is not enabled
	}

	// Get dashboard access URL
	dashboard, err := c.client.Authentication.AppPortalAccess(ctx, applicationID, models.AppPortalAccessIn{}, &svix.AuthenticationAppPortalAccessOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get dashboard access: %w", err)
	}

	return dashboard.Url, nil
}

// SendMessage sends a webhook message to the given application
func (c *Client) SendMessage(ctx context.Context, applicationID string, eventType string, payload interface{}) error {
	if !c.enabled || c.client == nil {
		return nil // Return nil if Svix is not enabled
	}

	var payloadMap map[string]interface{}

	// Handle different payload types
	switch p := payload.(type) {
	case map[string]interface{}:
		// If it's already a map, use it directly
		payloadMap = p
	case []byte:
		// If it's a byte slice (like json.RawMessage), unmarshal it
		if err := json.Unmarshal(p, &payloadMap); err != nil {
			return fmt.Errorf("failed to unmarshal payload: %w", err)
		}
	case json.RawMessage:
		// If it's a json.RawMessage, unmarshal it
		if err := json.Unmarshal(p, &payloadMap); err != nil {
			return fmt.Errorf("failed to unmarshal payload: %w", err)
		}
	default:
		// For any other type, try to marshal and then unmarshal it
		data, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		if err := json.Unmarshal(data, &payloadMap); err != nil {
			return fmt.Errorf("failed to unmarshal payload: %w", err)
		}
	}

	_, err := c.client.Message.Create(ctx, applicationID, models.MessageIn{
		EventType: eventType,
		Payload:   payloadMap,
	}, &svix.MessageCreateOptions{})
	if err != nil {
		// Check if application not found error
		if err.Error() == "application not found" {
			return nil // Ignore application not found errors as per requirement
		}
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}
