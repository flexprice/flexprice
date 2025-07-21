package stripe

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// StripeCustomer represents a Stripe customer object
type StripeCustomer struct {
	ID       string            `json:"id"`
	Email    string            `json:"email"`
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata"`
	Created  time.Time         `json:"created"`
}

// StripeMeter represents a Stripe billing meter object
type StripeMeter struct {
	ID              string            `json:"id"`
	DisplayName     string            `json:"display_name"`
	EventName       string            `json:"event_name"`
	CustomerMapping map[string]string `json:"customer_mapping"`
	ValueSettings   struct {
		EventPayloadKey string `json:"event_payload_key"`
	} `json:"value_settings"`
	DefaultAggregation struct {
		Formula string `json:"formula"`
	} `json:"default_aggregation"`
	Status   string            `json:"status"`
	Metadata map[string]string `json:"metadata"`
	Created  time.Time         `json:"created"`
}

// StripeUsageRecord represents a usage record sent to Stripe
type StripeUsageRecord struct {
	ID               string    `json:"id"`
	Object           string    `json:"object"`
	Quantity         float64   `json:"quantity"`
	Timestamp        time.Time `json:"timestamp"`
	SubscriptionItem string    `json:"subscription_item,omitempty"`
}

// StripeUsageRecordSummary represents a usage record summary from Stripe
type StripeUsageRecordSummary struct {
	ID               string            `json:"id"`
	Object           string            `json:"object"`
	Invoice          string            `json:"invoice"`
	LiveMode         bool              `json:"livemode"`
	Period           StripeUsagePeriod `json:"period"`
	SubscriptionItem string            `json:"subscription_item"`
	TotalUsage       int64             `json:"total_usage"`
}

// StripeUsagePeriod represents a billing period
type StripeUsagePeriod struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// StripeEvent represents a Stripe billing meter event
type StripeEvent struct {
	EventName  string                 `json:"event_name"`
	Payload    map[string]interface{} `json:"payload"`
	Timestamp  time.Time              `json:"timestamp"`
	Identifier string                 `json:"identifier"`
}

// StripeEventBatch represents a batch of events to send to Stripe
type StripeEventBatch struct {
	Events []StripeEvent `json:"events"`
}

// StripeAPIResponse represents a generic Stripe API response
type StripeAPIResponse struct {
	ID     string       `json:"id"`
	Object string       `json:"object"`
	Error  *StripeError `json:"error,omitempty"`
}

// StripeError represents a Stripe API error
type StripeError struct {
	Type          string `json:"type"`
	Code          string `json:"code"`
	Message       string `json:"message"`
	Param         string `json:"param,omitempty"`
	DeclineCode   string `json:"decline_code,omitempty"`
	ChargeID      string `json:"charge,omitempty"`
	PaymentIntent string `json:"payment_intent,omitempty"`
	PaymentMethod string `json:"payment_method,omitempty"`
	SetupIntent   string `json:"setup_intent,omitempty"`
	Source        string `json:"source,omitempty"`
}

// Validation methods

// Validate validates the Stripe customer fields
func (c *StripeCustomer) Validate() error {
	if c.ID == "" {
		return ierr.NewError("stripe customer id is required").
			WithHint("Stripe customer ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if len(c.ID) < 8 || c.ID[:4] != "cus_" {
		return ierr.NewError("invalid stripe customer id format").
			WithHint("Stripe customer ID must start with 'cus_' and be at least 8 characters").
			Mark(ierr.ErrValidation)
	}

	if c.Email != "" && len(c.Email) > 320 { // RFC 5321 limit
		return ierr.NewError("email too long").
			WithHint("Email must be less than 320 characters").
			Mark(ierr.ErrValidation)
	}

	if c.Name != "" && len(c.Name) > 255 {
		return ierr.NewError("name too long").
			WithHint("Name must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Validate validates the Stripe meter fields
func (m *StripeMeter) Validate() error {
	if m.ID == "" {
		return ierr.NewError("stripe meter id is required").
			WithHint("Stripe meter ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if m.DisplayName == "" {
		return ierr.NewError("display_name is required").
			WithHint("Meter display name must not be empty").
			Mark(ierr.ErrValidation)
	}

	if m.EventName == "" {
		return ierr.NewError("event_name is required").
			WithHint("Meter event name must not be empty").
			Mark(ierr.ErrValidation)
	}

	if len(m.DisplayName) > 255 {
		return ierr.NewError("display_name too long").
			WithHint("Display name must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	if len(m.EventName) > 255 {
		return ierr.NewError("event_name too long").
			WithHint("Event name must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	if m.Status != "" && m.Status != "active" && m.Status != "inactive" {
		return ierr.NewError("invalid status").
			WithHint("Status must be 'active' or 'inactive'").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Validate validates the Stripe usage record fields
func (u *StripeUsageRecord) Validate() error {
	if u.Quantity < 0 {
		return ierr.NewError("quantity cannot be negative").
			WithHint("Usage quantity must be >= 0").
			Mark(ierr.ErrValidation)
	}

	if u.Timestamp.IsZero() {
		return ierr.NewError("timestamp is required").
			WithHint("Usage record timestamp must not be zero").
			Mark(ierr.ErrValidation)
	}

	// Timestamp should not be in the future (with some tolerance)
	now := time.Now().UTC()
	if u.Timestamp.After(now.Add(5 * time.Minute)) {
		return ierr.NewError("timestamp cannot be in the future").
			WithHint("Usage record timestamp must not be more than 5 minutes in the future").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Validate validates the Stripe event fields
func (e *StripeEvent) Validate() error {
	if e.EventName == "" {
		return ierr.NewError("event_name is required").
			WithHint("Event name must not be empty").
			Mark(ierr.ErrValidation)
	}

	if e.Payload == nil {
		return ierr.NewError("payload is required").
			WithHint("Event payload must not be nil").
			Mark(ierr.ErrValidation)
	}

	if e.Timestamp.IsZero() {
		return ierr.NewError("timestamp is required").
			WithHint("Event timestamp must not be zero").
			Mark(ierr.ErrValidation)
	}

	if e.Identifier == "" {
		return ierr.NewError("identifier is required").
			WithHint("Event identifier must not be empty").
			Mark(ierr.ErrValidation)
	}

	if len(e.EventName) > 255 {
		return ierr.NewError("event_name too long").
			WithHint("Event name must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	if len(e.Identifier) > 255 {
		return ierr.NewError("identifier too long").
			WithHint("Event identifier must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Validate validates the Stripe event batch
func (b *StripeEventBatch) Validate() error {
	if len(b.Events) == 0 {
		return ierr.NewError("events is required").
			WithHint("Event batch must contain at least one event").
			Mark(ierr.ErrValidation)
	}

	if len(b.Events) > 1000 { // Stripe's batch limit
		return ierr.NewError("too many events in batch").
			WithHint("Event batch must contain at most 1000 events").
			Mark(ierr.ErrValidation)
	}

	for _, event := range b.Events {
		if err := event.Validate(); err != nil {
			return ierr.NewError("invalid event in batch").
				WithHint("Event validation failed").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// IsClientError returns true if the error is a client error (4xx)
func (e *StripeError) IsClientError() bool {
	return e.Type == "card_error" || e.Type == "invalid_request_error"
}

// IsServerError returns true if the error is a server error (5xx)
func (e *StripeError) IsServerError() bool {
	return e.Type == "api_error"
}

// IsRetryable returns true if the error is retryable
func (e *StripeError) IsRetryable() bool {
	if e.IsServerError() {
		return true
	}
	// Some rate limit errors are retryable
	return e.Type == "rate_limit_error"
}
