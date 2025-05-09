package events

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// Event represents the base event structure
type Event struct {
	// Unique identifier for the event
	ID string `json:"id" ch:"id" validate:"required"`

	// Tenant identifier
	TenantID string `json:"tenant_id" ch:"tenant_id" validate:"required"`

	// Environment identifier
	EnvironmentID string `json:"environment_id" ch:"environment_id"`

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

// ProcessedEvent represents an event that has been processed for billing
type ProcessedEvent struct {
	// Original event fields
	Event
	// Processing fields
	SubscriptionID string `json:"subscription_id" ch:"subscription_id"`
	SubLineItemID  string `json:"sub_line_item_id" ch:"sub_line_item_id"`
	PriceID        string `json:"price_id" ch:"price_id"`
	FeatureID      string `json:"feature_id" ch:"feature_id"`
	MeterID        string `json:"meter_id" ch:"meter_id"`
	PeriodID       uint64 `json:"period_id" ch:"period_id"`
	Currency       string `json:"currency" ch:"currency"`

	// Deduplication and metrics
	UniqueHash     string          `json:"unique_hash" ch:"unique_hash"`
	QtyTotal       decimal.Decimal `json:"qty_total" ch:"qty_total"`
	QtyBillable    decimal.Decimal `json:"qty_billable" ch:"qty_billable"`
	QtyFreeApplied decimal.Decimal `json:"qty_free_applied" ch:"qty_free_applied"`
	TierSnapshot   decimal.Decimal `json:"tier_snapshot" ch:"tier_snapshot"`
	UnitCost       decimal.Decimal `json:"unit_cost" ch:"unit_cost"`
	Cost           decimal.Decimal `json:"cost" ch:"cost"`

	// Audit fields
	Version uint64 `json:"version" ch:"version"`
	Sign    int8   `json:"sign" ch:"sign"`

	// Processing metadata
	ProcessedAt time.Time `json:"processed_at" ch:"processed_at,timezone('UTC')"`
	FinalLagMs  uint32    `json:"final_lag_ms" ch:"final_lag_ms"`
}

// NewEvent creates a new event with defaults
func NewEvent(
	eventName, tenantID, externalCustomerID string, // primary keys
	properties map[string]interface{},
	timestamp time.Time,
	eventID, customerID, source string,
	environmentID string, // Add environmentID parameter
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
		EnvironmentID:      environmentID,
	}
}

// Validate validates the event
func (e *Event) Validate() error {
	if e.CustomerID == "" && e.ExternalCustomerID == "" {
		return ierr.NewError("customer_id or external_customer_id is required").
			WithHint("Customer ID or external customer ID is required").
			Mark(ierr.ErrValidation)
	}

	return validator.ValidateRequest(e)
}

// ToProcessedEvent creates a new ProcessedEvent from this Event with pending status
func (e *Event) ToProcessedEvent() *ProcessedEvent {
	return &ProcessedEvent{
		Event:          *e,
		QtyTotal:       decimal.Zero,
		QtyBillable:    decimal.Zero,
		QtyFreeApplied: decimal.Zero,
		TierSnapshot:   decimal.Zero,
		UnitCost:       decimal.Zero,
		Cost:           decimal.Zero,
	}
}
