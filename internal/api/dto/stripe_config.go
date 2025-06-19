package dto

import (
	"fmt"
	"regexp"
)

// CreateOrUpdateStripeConfigRequest represents a request to create or update Stripe configuration
type CreateOrUpdateStripeConfigRequest struct {
	APIKey                   string                  `json:"api_key" validate:"required"`
	SyncEnabled              bool                    `json:"sync_enabled"`
	AggregationWindowMinutes int                     `json:"aggregation_window_minutes" validate:"min=5,max=60"`
	WebhookConfig            *StripeWebhookConfigDTO `json:"webhook_config,omitempty"`
	Metadata                 map[string]interface{}  `json:"metadata,omitempty"`
}

// StripeWebhookConfigDTO represents webhook configuration in DTOs
type StripeWebhookConfigDTO struct {
	EndpointURL string `json:"endpoint_url" validate:"required,url"`
	Secret      string `json:"secret,omitempty"`
	Enabled     bool   `json:"enabled"`
}

// StripeConfigResponse represents the response for Stripe configuration
type StripeConfigResponse struct {
	TenantID                 string                  `json:"tenant_id"`
	EnvironmentID            string                  `json:"environment_id"`
	SyncEnabled              bool                    `json:"sync_enabled"`
	AggregationWindowMinutes int                     `json:"aggregation_window_minutes"`
	WebhookConfig            *StripeWebhookConfigDTO `json:"webhook_config,omitempty"`
	Metadata                 map[string]interface{}  `json:"metadata,omitempty"`
	CreatedAt                string                  `json:"created_at"`
	UpdatedAt                string                  `json:"updated_at"`
	// Note: API key is never returned for security reasons
}

// StripeConnectionTestResponse represents the response for testing Stripe connection
type StripeConnectionTestResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	LatencyMs int64  `json:"latency_ms"`
	TestedAt  string `json:"tested_at"`
}

// StripeConfigStatusResponse represents the response for configuration status
type StripeConfigStatusResponse struct {
	Configured       bool                          `json:"configured"`
	SyncEnabled      bool                          `json:"sync_enabled"`
	ConfigurationOK  bool                          `json:"configuration_ok"`
	LastTestedAt     *string                       `json:"last_tested_at"`
	ConnectionStatus *StripeConnectionTestResponse `json:"connection_status,omitempty"`
	Issues           []string                      `json:"issues,omitempty"`
}

// StripeConfigHistoryEntry represents a single configuration change entry
type StripeConfigHistoryEntry struct {
	ID          string                 `json:"id"`
	ChangedAt   string                 `json:"changed_at"`
	ChangedBy   string                 `json:"changed_by"`
	ChangeType  string                 `json:"change_type"` // "created", "updated", "deleted"
	Changes     map[string]interface{} `json:"changes"`
	Description string                 `json:"description"`
}

// StripeConfigHistoryResponse represents the response for configuration history
type StripeConfigHistoryResponse struct {
	Items   []StripeConfigHistoryEntry `json:"items"`
	Total   int                        `json:"total"`
	Limit   int                        `json:"limit"`
	Offset  int                        `json:"offset"`
	HasMore bool                       `json:"has_more"`
}

// Validate validates the CreateOrUpdateStripeConfigRequest
func (r *CreateOrUpdateStripeConfigRequest) Validate() error {
	// Additional custom validation for Stripe API key format
	if !isValidStripeAPIKey(r.APIKey) {
		return fmt.Errorf("invalid Stripe API key format. Must start with sk_test_ or sk_live_ followed by at least 24 characters")
	}

	// Validate aggregation window
	if r.AggregationWindowMinutes < 5 || r.AggregationWindowMinutes > 60 {
		return fmt.Errorf("aggregation_window_minutes must be between 5 and 60")
	}

	// Validate webhook configuration if provided
	if r.WebhookConfig != nil {
		if err := r.WebhookConfig.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// Validate validates the StripeWebhookConfigDTO
func (w *StripeWebhookConfigDTO) Validate() error {
	if w.EndpointURL == "" {
		return fmt.Errorf("webhook endpoint URL is required")
	}

	// Ensure webhook URLs are HTTPS
	if w.EndpointURL != "" && !isHTTPS(w.EndpointURL) {
		return fmt.Errorf("webhook URLs must use HTTPS")
	}

	return nil
}

// Helper functions for service layer conversions (to be used by handlers)

// NewStripeConfigHistoryResponse creates a new StripeConfigHistoryResponse
func NewStripeConfigHistoryResponse(
	entries []StripeConfigHistoryEntry,
	total int,
	limit int,
	offset int,
) *StripeConfigHistoryResponse {
	return &StripeConfigHistoryResponse{
		Items:   entries,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: offset+len(entries) < total,
	}
}

// Helper validation functions

// isValidStripeAPIKey validates if the provided string is a valid Stripe API key format
func isValidStripeAPIKey(apiKey string) bool {
	// Stripe API keys start with sk_test_ or sk_live_ followed by 24+ characters
	pattern := `^sk_(test|live)_[A-Za-z0-9]{24,}$`
	matched, err := regexp.MatchString(pattern, apiKey)
	return err == nil && matched
}

// isHTTPS checks if the URL uses HTTPS
func isHTTPS(url string) bool {
	return len(url) >= 8 && url[:8] == "https://"
}
