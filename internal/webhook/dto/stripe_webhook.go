package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/validator"
)

// StripeWebhookEvent represents the top-level Stripe webhook event
type StripeWebhookEvent struct {
	ID              string              `json:"id" validate:"required"`
	Object          string              `json:"object" validate:"required,eq=event"`
	APIVersion      string              `json:"api_version"`
	Created         int64               `json:"created" validate:"required"`
	Data            StripeEventData     `json:"data" validate:"required"`
	Livemode        bool                `json:"livemode"`
	PendingWebhooks int                 `json:"pending_webhooks"`
	Request         *StripeEventRequest `json:"request"`
	Type            string              `json:"type" validate:"required"`
}

// StripeEventData contains the data object from the Stripe webhook
type StripeEventData struct {
	Object             map[string]interface{} `json:"object" validate:"required"`
	PreviousAttributes map[string]interface{} `json:"previous_attributes,omitempty"`
}

// StripeEventRequest contains information about the API request that triggered the event
type StripeEventRequest struct {
	ID             string `json:"id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// StripeCustomerData represents the customer object from Stripe webhook
type StripeCustomerData struct {
	ID            string                 `json:"id" validate:"required"`
	Object        string                 `json:"object" validate:"required,eq=customer"`
	Created       int64                  `json:"created"`
	DefaultSource string                 `json:"default_source"`
	Deleted       bool                   `json:"deleted"`
	Description   string                 `json:"description"`
	Email         string                 `json:"email"`
	Livemode      bool                   `json:"livemode"`
	Metadata      map[string]string      `json:"metadata"`
	Name          string                 `json:"name"`
	Phone         string                 `json:"phone"`
	Shipping      map[string]interface{} `json:"shipping"`
	TaxExempt     string                 `json:"tax_exempt"`
	TestClock     string                 `json:"test_clock"`
}

// StripeWebhookRequest represents the incoming webhook request
type StripeWebhookRequest struct {
	// Raw webhook payload will be in the body, this struct is for processed data
	Event      *StripeWebhookEvent `json:"event"`
	Signature  string              `json:"-"` // From Stripe-Signature header
	Timestamp  time.Time           `json:"-"` // Parsed from signature
	RawPayload []byte              `json:"-"` // Original payload for signature verification
}

// StripeWebhookResponse represents the response to Stripe webhook
type StripeWebhookResponse struct {
	Received bool   `json:"received"`
	Message  string `json:"message,omitempty"`
}

// ProcessedStripeCustomer represents the processed customer data for internal use
type ProcessedStripeCustomer struct {
	StripeCustomerID string            `json:"stripe_customer_id" validate:"required"`
	ExternalID       string            `json:"external_id"`
	Email            string            `json:"email"`
	Name             string            `json:"name"`
	Metadata         map[string]string `json:"metadata"`
	TenantID         string            `json:"tenant_id" validate:"omitempty"`
	EnvironmentID    string            `json:"environment_id" validate:"omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
}

// Validate validates the StripeWebhookEvent using the validator package
func (e *StripeWebhookEvent) Validate() error {
	return validator.ValidateRequest(e)
}

// IsCustomerCreated checks if the webhook event is a customer.created event
func (e *StripeWebhookEvent) IsCustomerCreated() bool {
	return e.Type == "customer.created"
}

// IsCustomerUpdated checks if the webhook event is a customer.updated event
func (e *StripeWebhookEvent) IsCustomerUpdated() bool {
	return e.Type == "customer.updated"
}

// IsCustomerDeleted checks if the webhook event is a customer.deleted event
func (e *StripeWebhookEvent) IsCustomerDeleted() bool {
	return e.Type == "customer.deleted"
}

// GetCustomerData extracts customer data from the event data object
func (e *StripeWebhookEvent) GetCustomerData() (*StripeCustomerData, error) {
	customer := &StripeCustomerData{}

	// Extract fields from the object map
	if id, ok := e.Data.Object["id"].(string); ok {
		customer.ID = id
	}

	if obj, ok := e.Data.Object["object"].(string); ok {
		customer.Object = obj
	}

	if created, ok := e.Data.Object["created"].(float64); ok {
		customer.Created = int64(created)
	}

	if email, ok := e.Data.Object["email"].(string); ok {
		customer.Email = email
	}

	if name, ok := e.Data.Object["name"].(string); ok {
		customer.Name = name
	}

	if description, ok := e.Data.Object["description"].(string); ok {
		customer.Description = description
	}

	if phone, ok := e.Data.Object["phone"].(string); ok {
		customer.Phone = phone
	}

	if livemode, ok := e.Data.Object["livemode"].(bool); ok {
		customer.Livemode = livemode
	}

	if deleted, ok := e.Data.Object["deleted"].(bool); ok {
		customer.Deleted = deleted
	}

	// Handle metadata
	if metadata, ok := e.Data.Object["metadata"].(map[string]interface{}); ok {
		customer.Metadata = make(map[string]string)
		for k, v := range metadata {
			if strVal, ok := v.(string); ok {
				customer.Metadata[k] = strVal
			}
		}
	}

	return customer, nil
}

// GetExternalID extracts the external_id from customer metadata
func (c *StripeCustomerData) GetExternalID() string {
	if c.Metadata == nil {
		return ""
	}
	return c.Metadata["external_id"]
}

// GetTenantID extracts the tenant_id from customer metadata
func (c *StripeCustomerData) GetTenantID() string {
	if c.Metadata == nil {
		return ""
	}
	return c.Metadata["tenant_id"]
}

// GetEnvironmentID extracts the environment_id from customer metadata
func (c *StripeCustomerData) GetEnvironmentID() string {
	if c.Metadata == nil {
		return ""
	}
	return c.Metadata["environment_id"]
}

// ToProcessedCustomer converts StripeCustomerData to ProcessedStripeCustomer
func (c *StripeCustomerData) ToProcessedCustomer() *ProcessedStripeCustomer {
	return &ProcessedStripeCustomer{
		StripeCustomerID: c.ID,
		ExternalID:       c.GetExternalID(),
		Email:            c.Email,
		Name:             c.Name,
		Metadata:         c.Metadata,
		TenantID:         c.GetTenantID(),
		EnvironmentID:    c.GetEnvironmentID(),
		CreatedAt:        time.Unix(c.Created, 0),
	}
}

// Validate validates the ProcessedStripeCustomer
func (p *ProcessedStripeCustomer) Validate() error {
	return validator.ValidateRequest(p)
}

// NewStripeWebhookResponse creates a successful webhook response
func NewStripeWebhookResponse() *StripeWebhookResponse {
	return &StripeWebhookResponse{
		Received: true,
		Message:  "Webhook processed successfully",
	}
}

// NewStripeWebhookErrorResponse creates an error webhook response
func NewStripeWebhookErrorResponse(message string) *StripeWebhookResponse {
	return &StripeWebhookResponse{
		Received: false,
		Message:  message,
	}
}
