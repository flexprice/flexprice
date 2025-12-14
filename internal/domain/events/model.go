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
	QtyTotal       decimal.Decimal `json:"qty_total" ch:"qty_total" swaggertype:"string"`
	QtyBillable    decimal.Decimal `json:"qty_billable" ch:"qty_billable" swaggertype:"string"`
	QtyFreeApplied decimal.Decimal `json:"qty_free_applied" ch:"qty_free_applied" swaggertype:"string"`
	TierSnapshot   decimal.Decimal `json:"tier_snapshot" ch:"tier_snapshot" swaggertype:"string"`
	UnitCost       decimal.Decimal `json:"unit_cost" ch:"unit_cost" swaggertype:"string"`
	Cost           decimal.Decimal `json:"cost" ch:"cost" swaggertype:"string"`

	// Audit fields
	Version uint64 `json:"version" ch:"version"`
	Sign    int8   `json:"sign" ch:"sign"`

	// Processing metadata
	ProcessedAt time.Time `json:"processed_at" ch:"processed_at,timezone('UTC')"`
	FinalLagMs  uint32    `json:"final_lag_ms" ch:"final_lag_ms"`
}

// FindUnprocessedEventsParams contains parameters for finding events that haven't been processed
type FindUnprocessedEventsParams struct {
	ExternalCustomerID string    // Optional filter by external customer ID
	EventName          string    // Optional filter by event name
	StartTime          time.Time // Optional filter by start time
	EndTime            time.Time // Optional filter by end time
	BatchSize          int       // Number of events to return per batch
	LastID             string    // Last event ID for keyset pagination (more efficient than offset)
	LastTimestamp      time.Time // Last event timestamp for keyset pagination
}

// ReprocessEventsParams contains parameters for event reprocessing
type ReprocessEventsParams struct {
	ExternalCustomerID string    // Filter by external customer ID (optional)
	EventName          string    // Filter by event name (optional)
	StartTime          time.Time // Filter by start time (optional)
	EndTime            time.Time // Filter by end time (optional)
	BatchSize          int       // Number of events to process per batch (default 100)
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

// Feature Usage Model
type FeatureUsageEvent struct {
	Event
	FeatureID string `json:"feature_id" ch:"feature_id"`
}

// Feature Usage Processed Event
type FeatureUsage struct {
	// Original event fields
	Event
	// Processing fields
	SubscriptionID string `json:"subscription_id" ch:"subscription_id"`
	SubLineItemID  string `json:"sub_line_item_id" ch:"sub_line_item_id"`
	PriceID        string `json:"price_id" ch:"price_id"`
	FeatureID      string `json:"feature_id" ch:"feature_id"`
	MeterID        string `json:"meter_id" ch:"meter_id"`
	PeriodID       uint64 `json:"period_id" ch:"period_id"`
	// Deduplication and metrics
	UniqueHash string          `json:"unique_hash" ch:"unique_hash"`
	QtyTotal   decimal.Decimal `json:"qty_total" ch:"qty_total" swaggertype:"string"`

	// Audit fields
	Version uint64 `json:"version" ch:"version"`
	Sign    int8   `json:"sign" ch:"sign"`

	// Processing metadata
	ProcessedAt     time.Time `json:"processed_at" ch:"processed_at,timezone('UTC')"`
	ProcessingLagMs uint32    `json:"processing_lag_ms" ch:"processing_lag_ms"`
}

type CostUsage struct {
	// Original event fields
	Event
	// Processing fields
	CostSheetID string `json:"costsheet_id" ch:"costsheet_id"`
	PriceID     string `json:"price_id" ch:"price_id"`
	FeatureID   string `json:"feature_id" ch:"feature_id"`
	MeterID     string `json:"meter_id" ch:"meter_id"`

	// Deduplication and metrics
	UniqueHash string          `json:"unique_hash" ch:"unique_hash"`
	QtyTotal   decimal.Decimal `json:"qty_total" ch:"qty_total"`

	// Audit fields
	Version uint64 `json:"version" ch:"version"`
	Sign    int8   `json:"sign" ch:"sign"`

	// Processing metadata
	ProcessedAt     time.Time `json:"processed_at" ch:"processed_at,timezone('UTC')"`
	ProcessingLagMs uint32    `json:"processing_lag_ms" ch:"processing_lag_ms"`
}
