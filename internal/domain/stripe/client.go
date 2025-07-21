package stripe

import (
	"context"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// Client defines the interface for Stripe API operations
type Client interface {
	// Customer operations
	CreateCustomer(ctx context.Context, req *CreateCustomerRequest) (*StripeCustomer, error)
	GetCustomer(ctx context.Context, customerID string) (*StripeCustomer, error)
	UpdateCustomer(ctx context.Context, customerID string, req *UpdateCustomerRequest) (*StripeCustomer, error)

	// Meter operations
	CreateMeter(ctx context.Context, req *CreateMeterRequest) (*StripeMeter, error)
	GetMeter(ctx context.Context, meterID string) (*StripeMeter, error)
	UpdateMeter(ctx context.Context, meterID string, req *UpdateMeterRequest) (*StripeMeter, error)
	ListMeters(ctx context.Context, req *ListMetersRequest) ([]*StripeMeter, error)

	// Usage/Event operations
	CreateMeterEvent(ctx context.Context, req *CreateMeterEventRequest) (*StripeAPIResponse, error)
	CreateMeterEventBatch(ctx context.Context, req *CreateMeterEventBatchRequest) (*StripeAPIResponse, error)
	GetUsageRecordSummary(ctx context.Context, req *GetUsageRecordSummaryRequest) (*StripeUsageRecordSummary, error)

	// Webhook operations
	ValidateWebhookSignature(ctx context.Context, payload []byte, signature string, secret string) error
	ParseWebhookPayload(ctx context.Context, payload []byte, eventType string) (interface{}, error)
}

// Request/Response types for Stripe API operations

// CreateCustomerRequest represents a request to create a Stripe customer
type CreateCustomerRequest struct {
	Email       string            `json:"email,omitempty"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// UpdateCustomerRequest represents a request to update a Stripe customer
type UpdateCustomerRequest struct {
	Email       *string           `json:"email,omitempty"`
	Name        *string           `json:"name,omitempty"`
	Description *string           `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// CreateMeterRequest represents a request to create a Stripe billing meter
type CreateMeterRequest struct {
	DisplayName        string                  `json:"display_name"`
	EventName          string                  `json:"event_name"`
	CustomerMapping    map[string]string       `json:"customer_mapping,omitempty"`
	ValueSettings      MeterValueSettings      `json:"value_settings"`
	DefaultAggregation MeterDefaultAggregation `json:"default_aggregation"`
	Metadata           map[string]string       `json:"metadata,omitempty"`
}

// MeterValueSettings represents meter value configuration
type MeterValueSettings struct {
	EventPayloadKey string `json:"event_payload_key"`
}

// MeterDefaultAggregation represents meter aggregation configuration
type MeterDefaultAggregation struct {
	Formula string `json:"formula"`
}

// UpdateMeterRequest represents a request to update a Stripe billing meter
type UpdateMeterRequest struct {
	DisplayName *string           `json:"display_name,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ListMetersRequest represents a request to list Stripe billing meters
type ListMetersRequest struct {
	Limit         *int    `json:"limit,omitempty"`
	StartingAfter *string `json:"starting_after,omitempty"`
	EndingBefore  *string `json:"ending_before,omitempty"`
	Status        *string `json:"status,omitempty"`
}

// CreateMeterEventRequest represents a request to create a single meter event
type CreateMeterEventRequest struct {
	EventName  string                 `json:"event_name"`
	Payload    map[string]interface{} `json:"payload"`
	Timestamp  time.Time              `json:"timestamp"`
	Identifier string                 `json:"identifier"`
}

// CreateMeterEventBatchRequest represents a request to create multiple meter events
type CreateMeterEventBatchRequest struct {
	Events []CreateMeterEventRequest `json:"events"`
}

// GetUsageRecordSummaryRequest represents a request to get usage record summary
type GetUsageRecordSummaryRequest struct {
	SubscriptionItem string     `json:"subscription_item"`
	StartTime        *time.Time `json:"start_time,omitempty"`
	EndTime          *time.Time `json:"end_time,omitempty"`
}

// Validation methods for request types

// Validate validates the create customer request
func (r *CreateCustomerRequest) Validate() error {
	if r.Email != "" && len(r.Email) > 320 { // RFC 5321 limit
		return ierr.NewError("email too long").
			WithHint("Email must be less than 320 characters").
			Mark(ierr.ErrValidation)
	}

	if r.Name != "" && len(r.Name) > 255 {
		return ierr.NewError("name too long").
			WithHint("Name must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	if r.Description != "" && len(r.Description) > 350 {
		return ierr.NewError("description too long").
			WithHint("Description must be less than 350 characters").
			Mark(ierr.ErrValidation)
	}

	// Validate metadata
	if len(r.Metadata) > 50 {
		return ierr.NewError("too many metadata fields").
			WithHint("Maximum 50 metadata fields allowed").
			Mark(ierr.ErrValidation)
	}

	for key, value := range r.Metadata {
		if len(key) > 40 {
			return ierr.NewError("metadata key too long").
				WithHint("Metadata keys must be less than 40 characters").
				Mark(ierr.ErrValidation)
		}
		if len(value) > 500 {
			return ierr.NewError("metadata value too long").
				WithHint("Metadata values must be less than 500 characters").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// Validate validates the create meter request
func (r *CreateMeterRequest) Validate() error {
	if r.DisplayName == "" {
		return ierr.NewError("display_name is required").
			WithHint("Meter display name must not be empty").
			Mark(ierr.ErrValidation)
	}

	if r.EventName == "" {
		return ierr.NewError("event_name is required").
			WithHint("Meter event name must not be empty").
			Mark(ierr.ErrValidation)
	}

	if len(r.DisplayName) > 255 {
		return ierr.NewError("display_name too long").
			WithHint("Display name must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	if len(r.EventName) > 255 {
		return ierr.NewError("event_name too long").
			WithHint("Event name must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	if r.ValueSettings.EventPayloadKey == "" {
		return ierr.NewError("event_payload_key is required").
			WithHint("Value settings event payload key must not be empty").
			Mark(ierr.ErrValidation)
	}

	if r.DefaultAggregation.Formula == "" {
		return ierr.NewError("aggregation formula is required").
			WithHint("Default aggregation formula must not be empty").
			Mark(ierr.ErrValidation)
	}

	// Validate allowed aggregation formulas
	validFormulas := []string{"sum", "count", "max", "last"}
	isValidFormula := false
	for _, formula := range validFormulas {
		if r.DefaultAggregation.Formula == formula {
			isValidFormula = true
			break
		}
	}
	if !isValidFormula {
		return ierr.NewError("invalid aggregation formula").
			WithHint("Formula must be one of: sum, count, max, last").
			Mark(ierr.ErrValidation)
	}

	// Validate metadata
	if len(r.Metadata) > 50 {
		return ierr.NewError("too many metadata fields").
			WithHint("Maximum 50 metadata fields allowed").
			Mark(ierr.ErrValidation)
	}

	for key, value := range r.Metadata {
		if len(key) > 40 {
			return ierr.NewError("metadata key too long").
				WithHint("Metadata keys must be less than 40 characters").
				Mark(ierr.ErrValidation)
		}
		if len(value) > 500 {
			return ierr.NewError("metadata value too long").
				WithHint("Metadata values must be less than 500 characters").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// Validate validates the create meter event request
func (r *CreateMeterEventRequest) Validate() error {
	if r.EventName == "" {
		return ierr.NewError("event_name is required").
			WithHint("Event name must not be empty").
			Mark(ierr.ErrValidation)
	}

	if r.Payload == nil {
		return ierr.NewError("payload is required").
			WithHint("Event payload must not be nil").
			Mark(ierr.ErrValidation)
	}

	if r.Identifier == "" {
		return ierr.NewError("identifier is required").
			WithHint("Event identifier must not be empty").
			Mark(ierr.ErrValidation)
	}

	if r.Timestamp.IsZero() {
		return ierr.NewError("timestamp is required").
			WithHint("Event timestamp must not be zero").
			Mark(ierr.ErrValidation)
	}

	// Timestamp should not be in the future (with some tolerance)
	now := time.Now().UTC()
	if r.Timestamp.After(now.Add(5 * time.Minute)) {
		return ierr.NewError("timestamp cannot be in the future").
			WithHint("Event timestamp must not be more than 5 minutes in the future").
			Mark(ierr.ErrValidation)
	}

	// Timestamp should not be too old (Stripe has limits)
	if r.Timestamp.Before(now.Add(-48 * time.Hour)) {
		return ierr.NewError("timestamp too old").
			WithHint("Event timestamp must not be more than 48 hours old").
			Mark(ierr.ErrValidation)
	}

	if len(r.EventName) > 255 {
		return ierr.NewError("event_name too long").
			WithHint("Event name must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	if len(r.Identifier) > 255 {
		return ierr.NewError("identifier too long").
			WithHint("Identifier must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Validate validates the create meter event batch request
func (r *CreateMeterEventBatchRequest) Validate() error {
	if len(r.Events) == 0 {
		return ierr.NewError("events list is empty").
			WithHint("At least one event must be provided").
			Mark(ierr.ErrValidation)
	}

	if len(r.Events) > 100 { // Stripe batch limit
		return ierr.NewError("too many events in batch").
			WithHint("Maximum 100 events allowed per batch").
			Mark(ierr.ErrValidation)
	}

	// Validate each event in the batch
	for i, event := range r.Events {
		if err := event.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint("Invalid event in batch").
				WithReportableDetails(map[string]interface{}{
					"event_index": i,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// Validate validates the list meters request
func (r *ListMetersRequest) Validate() error {
	if r.Limit != nil && (*r.Limit < 1 || *r.Limit > 100) {
		return ierr.NewError("invalid limit").
			WithHint("Limit must be between 1 and 100").
			Mark(ierr.ErrValidation)
	}

	if r.Status != nil && *r.Status != "active" && *r.Status != "inactive" {
		return ierr.NewError("invalid status").
			WithHint("Status must be 'active' or 'inactive'").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Validate validates the get usage record summary request
func (r *GetUsageRecordSummaryRequest) Validate() error {
	if r.SubscriptionItem == "" {
		return ierr.NewError("subscription_item is required").
			WithHint("Subscription item ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if r.StartTime != nil && r.EndTime != nil && r.StartTime.After(*r.EndTime) {
		return ierr.NewError("invalid time range").
			WithHint("Start time must be before end time").
			Mark(ierr.ErrValidation)
	}

	return nil
}
