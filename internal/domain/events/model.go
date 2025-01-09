package events

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
)

// Event represents the base event structure
type Event struct {
	// Unique identifier for the event
	ID string `json:"id" ch:"id" validate:"required"`

	// Tenant identifier
	TenantID string `json:"tenant_id" ch:"tenant_id" validate:"required"`

	// Event name is an identifier for the event and will be used for filtering and aggregation
	EventName string `json:"event_name" ch:"event_name" validate:"required"`

	// Additional properties
	Properties map[string]interface{} `json:"properties" ch:"properties"`

	// Source of the event
	Source string `json:"source" ch:"source"`

	// Timestamps
	Timestamp time.Time `json:"timestamp" ch:"timestamp,timezone('UTC')" validate:"required"`

	// IngestedAt is the time the event was ingested into the database and it is automatically set to the current time
	// at the clickhouse server level and is not required to be set by the caller
	IngestedAt time.Time `json:"ingested_at" ch:"ingested_at,timezone('UTC')"`

	// Subject identifiers - at least one is required
	// CustomerID is the identifier of the customer in Flexprice's system
	CustomerID string `json:"customer_id" ch:"customer_id"`

	// ExternalCustomerID is the identifier of the customer in the external system ex Customer DB or Stripe
	ExternalCustomerID string `json:"external_customer_id" ch:"external_customer_id"`
}

// NewEvent creates a new event with defaults
func NewEvent(
	eventName, tenantID, externalCustomerID string, // primary keys
	properties map[string]interface{},
	timestamp time.Time,
	eventID, customerID, source string,
) *Event {
	if eventID == "" {
		eventID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_EVENT)
	}

	now := time.Now().UTC()

	if timestamp.IsZero() {
		timestamp = now
	} else {
		timestamp = timestamp.UTC()
	}

	return &Event{
		ID:                 eventID,
		TenantID:           tenantID,
		CustomerID:         customerID,
		ExternalCustomerID: externalCustomerID,
		Source:             source,
		EventName:          eventName,
		Timestamp:          timestamp,
		Properties:         properties,
	}
}

// Validate validates the event
func (e *Event) Validate() error {
	if e.CustomerID == "" && e.ExternalCustomerID == "" {
		return fmt.Errorf("customer_id or external_customer_id is required")
	}

	return validator.New().Struct(e)
}
