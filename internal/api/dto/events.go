package dto

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

type IngestEventRequest struct {
	EventName          string                 `json:"event_name" validate:"required" binding:"required" example:"api_request" csv:"event_name"`
	EventID            string                 `json:"event_id" example:"event123" csv:"event_id"`
	CustomerID         string                 `json:"customer_id" example:"customer456" csv:"customer_id"`
	ExternalCustomerID string                 `json:"external_customer_id" validate:"required" binding:"required" example:"customer456" csv:"external_customer_id"`
	Timestamp          time.Time              `json:"timestamp" example:"2024-03-20T15:04:05Z" csv:"-"` // Handled separately due to parsing
	TimestampStr       string                 `json:"-" csv:"timestamp"`                                // Used for CSV parsing
	Source             string                 `json:"source" example:"api" csv:"source"`
	Properties         map[string]interface{} `json:"properties" swaggertype:"object,string,number" example:"{\"request_size\":100,\"response_status\":200}" csv:"-"` // Handled separately for dynamic columns
}

func (r *IngestEventRequest) Validate() error {
	return validator.ValidateRequest(r)
}

type BulkIngestEventRequest struct {
	Events []*IngestEventRequest `json:"events" validate:"required,min=1,max=1000"`
}

func (r *BulkIngestEventRequest) Validate() error {
	return validator.ValidateRequest(r)
}

func (r *IngestEventRequest) ToEvent(ctx context.Context) *events.Event {
	return events.NewEvent(
		r.EventName,
		types.GetTenantID(ctx),
		r.ExternalCustomerID,
		r.Properties,
		r.Timestamp,
		r.EventID,
		r.CustomerID,
		r.Source,
		types.GetEnvironmentID(ctx),
	)
}

type GetUsageRequest struct {
	ExternalCustomerID string                `form:"external_customer_id" json:"external_customer_id" example:"customer456"`
	CustomerID         string                `form:"customer_id" json:"customer_id" example:"customer456"`
	EventName          string                `form:"event_name" json:"event_name" binding:"required" required:"true" example:"api_request"`
	PropertyName       string                `form:"property_name" json:"property_name" example:"request_size"` // will be empty/ignored in case of COUNT
	AggregationType    types.AggregationType `form:"aggregation_type" json:"aggregation_type" binding:"required"`
	StartTime          time.Time             `form:"start_time" json:"start_time" example:"2024-03-13T00:00:00Z"`
	EndTime            time.Time             `form:"end_time" json:"end_time" example:"2024-03-20T00:00:00Z"`
	WindowSize         types.WindowSize      `form:"window_size" json:"window_size"`
	BucketSize         types.WindowSize      `form:"bucket_size" json:"bucket_size,omitempty" example:"HOUR"` // Optional, only used for MAX aggregation with windowing
	Filters            map[string][]string   `form:"filters,omitempty" json:"filters,omitempty"`
	PriceID            string                `form:"-" json:"-"` // this is just for internal use to store the price id
	MeterID            string                `form:"-" json:"-"` // this is just for internal use to store the meter id
	Multiplier         *decimal.Decimal      `form:"multiplier" json:"multiplier,omitempty" swaggertype:"string"`
	// BillingAnchor enables custom monthly billing periods for usage aggregation.
	//
	// When to use:
	// - WindowSize = "MONTH" AND you need custom monthly periods (not calendar months)
	// - Subscription billing that doesn't align with calendar months
	// - Example: Customer signed up on 15th, so billing periods are 15th to 15th
	//
	// When NOT to use:
	// - WindowSize != "MONTH" (ignored for DAY, HOUR, WEEK, etc.)
	// - Standard calendar-based billing (1st to 1st of each month)
	//
	// Example values:
	// - "2024-03-05T14:30:45.123456789Z" (5th of each month at 2:30:45 PM)
	// - "2024-01-15T00:00:00Z" (15th of each month at midnight)
	// - "2024-02-29T12:00:00Z" (29th of each month at noon - handles leap years)
	BillingAnchor *time.Time `form:"billing_anchor" json:"billing_anchor,omitempty" example:"2024-03-05T14:30:45.123456789Z"`
}

type GetUsageByMeterRequest struct {
	MeterID            string              `form:"meter_id" json:"meter_id" binding:"required" example:"123"`
	PriceID            string              `form:"-" json:"-"` // this is just for internal use to store the price id
	Meter              *meter.Meter        `form:"-" json:"-"` // caller can set this in case already fetched from db to avoid extra db call
	ExternalCustomerID string              `form:"external_customer_id" json:"external_customer_id" example:"user_5"`
	CustomerID         string              `form:"customer_id" json:"customer_id" example:"customer456"`
	StartTime          time.Time           `form:"start_time" json:"start_time" example:"2024-11-09T00:00:00Z"`
	EndTime            time.Time           `form:"end_time" json:"end_time" example:"2024-12-09T00:00:00Z"`
	WindowSize         types.WindowSize    `form:"window_size" json:"window_size"`
	BucketSize         types.WindowSize    `form:"bucket_size" json:"bucket_size,omitempty" example:"HOUR"` // Optional, only used for MAX aggregation with windowing
	Filters            map[string][]string `form:"filters,omitempty" json:"filters,omitempty"`
	// BillingAnchor enables custom monthly billing periods for meter usage aggregation.
	//
	// Usage guidelines:
	// - Only effective when WindowSize = "MONTH"
	// - For other window sizes (DAY, HOUR, WEEK), this field is ignored
	// - When nil, uses standard calendar months (1st to 1st)
	// - When provided, creates custom monthly periods (e.g., 5th to 5th)
	//
	// Common use cases:
	// - Subscription billing periods that don't align with calendar months
	// - Customer-specific billing cycles (e.g., signed up on 15th)
	// - Multi-tenant systems with different billing anchor dates
	//
	// Example: If BillingAnchor = "2024-03-05T14:30:45Z" and WindowSize = "MONTH":
	//   - March period: 2024-03-05 14:30:45 to 2024-04-05 14:30:45
	//   - April period: 2024-04-05 14:30:45 to 2024-05-05 14:30:45
	BillingAnchor *time.Time `form:"billing_anchor" json:"billing_anchor,omitempty" example:"2024-03-05T14:30:45Z"`
}

type GetEventsRequest struct {
	// Customer ID in your system that was sent with the event
	ExternalCustomerID string `json:"external_customer_id"`
	// Event name / Unique identifier for the event in your system
	EventName string `json:"event_name"`
	// Event ID is the idempotency key for the event
	EventID string `json:"event_id"`
	// Start time of the events to be fetched in ISO 8601 format
	// Defaults to last 7 days from now if not provided
	StartTime time.Time `json:"start_time" example:"2024-11-09T00:00:00Z"`
	// End time of the events to be fetched in ISO 8601 format
	// Defaults to now if not provided
	EndTime time.Time `json:"end_time" example:"2024-12-09T00:00:00Z"`
	// First key to iterate over the events
	IterFirstKey string `json:"iter_first_key"`
	// Last key to iterate over the events
	IterLastKey string `json:"iter_last_key"`
	// Property filters to filter the events by the keys in `properties` field of the event
	PropertyFilters map[string][]string `json:"property_filters,omitempty"`
	// Page size to fetch the events and is set to 50 by default
	PageSize int `json:"page_size"`
	// Offset to fetch the events and is set to 0 by default
	Offset int `json:"offset"`
	// Source to filter the events by the source
	Source string `json:"source"`
	// Sort by the field. Allowed values (case sensitive): timestamp, event_name (default: timestamp)
	Sort *string `json:"sort,omitempty" form:"sort" example:"timestamp"`
	// Order by condition. Allowed values (case sensitive): asc, desc (default: desc)
	Order *string `json:"order,omitempty" form:"order" example:"desc"`
	// Count of total number of events
	CountTotal bool `json:"-"`
}

type GetEventsResponse struct {
	Events       []Event `json:"events"`
	HasMore      bool    `json:"has_more"`
	IterFirstKey string  `json:"iter_first_key,omitempty"`
	IterLastKey  string  `json:"iter_last_key,omitempty"`
	TotalCount   uint64  `json:"total_count,omitempty"`
	Offset       int     `json:"offset,omitempty"`
}

type Event struct {
	ID                 string                 `json:"id"`
	ExternalCustomerID string                 `json:"external_customer_id"`
	CustomerID         string                 `json:"customer_id"`
	EventName          string                 `json:"event_name"`
	Timestamp          time.Time              `json:"timestamp"`
	Properties         map[string]interface{} `json:"properties"`
	Source             string                 `json:"source"`
	EnvironmentID      string                 `json:"environment_id"`
}

type GetUsageResponse struct {
	Results   []UsageResult         `json:"results,omitempty"`
	Value     float64               `json:"value,omitempty"`
	EventName string                `json:"event_name"`
	Type      types.AggregationType `json:"type"`
}

type UsageResult struct {
	WindowSize time.Time `json:"window_size"`
	Value      float64   `json:"value"`
}

func FromAggregationResult(result *events.AggregationResult) *GetUsageResponse {
	if result == nil {
		return nil
	}

	response := &GetUsageResponse{
		Results:   make([]UsageResult, len(result.Results)),
		Value:     result.Value.InexactFloat64(),
		EventName: result.EventName,
		Type:      result.Type,
	}

	if len(result.Results) > 0 {
		for i, r := range result.Results {
			response.Results[i] = UsageResult{
				WindowSize: r.WindowSize,
				Value:      r.Value.InexactFloat64(),
			}
		}
	}

	return response
}

func (r *GetUsageRequest) Validate() error {
	return validator.ValidateRequest(r)
}

func (r *GetUsageRequest) ToUsageParams() *events.UsageParams {
	if r.AggregationType == "" || r.PropertyName == "" {
		r.AggregationType = types.AggregationCount
	}

	return &events.UsageParams{
		ExternalCustomerID: r.ExternalCustomerID,
		CustomerID:         r.CustomerID,
		EventName:          r.EventName,
		PropertyName:       r.PropertyName,
		AggregationType:    types.AggregationType(strings.ToUpper(string(r.AggregationType))),
		StartTime:          r.StartTime,
		EndTime:            r.EndTime,
		WindowSize:         r.WindowSize,
		BucketSize:         r.BucketSize,
		Filters:            r.Filters,
		Multiplier:         r.Multiplier,
		BillingAnchor:      r.BillingAnchor,
	}
}

func (r *GetUsageByMeterRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	if err := r.WindowSize.Validate(); err != nil {
		return err
	}

	return nil
}

func (r *GetEventsRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	allowedSortFields := []string{"timestamp", "event_name"}
	if r.Sort != nil && !slices.Contains(allowedSortFields, *r.Sort) {
		return ierr.NewErrorf("invalid sort field: %s", *r.Sort).
			WithHint("Request validation failed due to invalid sort field").
			WithReportableDetails(map[string]any{
				"sort":           *r.Sort,
				"allowed_values": allowedSortFields,
			}).
			Mark(ierr.ErrValidation)
	}

	allowedOrderValues := []string{types.OrderAsc, types.OrderDesc}
	if r.Order != nil && !slices.Contains(allowedOrderValues, *r.Order) {
		return ierr.NewErrorf("invalid order: %s", *r.Order).
			WithHint("Request validation failed due to invalid order by value").
			WithReportableDetails(map[string]any{
				"order":          *r.Order,
				"allowed_values": allowedOrderValues,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

type GetUsageAnalyticsRequest struct {
	ExternalCustomerID string           `json:"external_customer_id" binding:"required"`
	FeatureIDs         []string         `json:"feature_ids,omitempty"`
	Sources            []string         `json:"sources,omitempty"`
	StartTime          time.Time        `json:"start_time,omitempty"`
	EndTime            time.Time        `json:"end_time,omitempty"`
	GroupBy            []string         `json:"group_by,omitempty"` // allowed values: "source", "feature_id", "properties.<field_name>"
	WindowSize         types.WindowSize `json:"window_size,omitempty"`
	Expand             []string         `json:"expand,omitempty"` // allowed values: "price", "meter", "feature", "subscription_line_item","plan","addon"
	// Property filters to filter the events by the keys in `properties` field of the event
	PropertyFilters map[string][]string `json:"property_filters,omitempty"`
}

// GetUsageAnalyticsResponse represents the response for the usage analytics API
type GetUsageAnalyticsResponse struct {
	TotalCost decimal.Decimal     `json:"total_cost" swaggertype:"string"`
	Currency  string              `json:"currency"`
	Items     []UsageAnalyticItem `json:"items"`
}

// UsageAnalyticItem represents a single analytic item in the response
type UsageAnalyticItem struct {
	FeatureID            string                             `json:"feature_id"`
	PriceID              string                             `json:"price_id,omitempty"`               // Price ID used for this usage
	MeterID              string                             `json:"meter_id,omitempty"`               // Meter ID
	SubLineItemID        string                             `json:"sub_line_item_id,omitempty"`       // Subscription line item ID
	SubscriptionID       string                             `json:"subscription_id,omitempty"`        // Subscription ID
	Price                *PriceResponse                     `json:"price,omitempty"`                  // Full price object (only if expand includes "price")
	Meter                *meter.Meter                       `json:"meter,omitempty"`                  // Full meter object (only if expand includes "meter")
	Feature              *feature.Feature                   `json:"feature,omitempty"`                // Full feature object (only if expand includes "feature")
	SubscriptionLineItem *subscription.SubscriptionLineItem `json:"subscription_line_item,omitempty"` // Full line item (only if expand includes "subscription_line_item")
	Plan                 *plan.Plan                         `json:"plan,omitempty"`                   // Full plan object (only if expand includes "plan")
	Addon                *addon.Addon                       `json:"addon,omitempty"`                  // Full addon object (only if expand includes "addon")
	FeatureName          string                             `json:"name,omitempty"`
	EventName            string                             `json:"event_name,omitempty"`
	Source               string                             `json:"source,omitempty"`
	Sources              []string                           `json:"sources,omitempty"` // List of sources when not grouping by source
	Unit                 string                             `json:"unit,omitempty"`
	UnitPlural           string                             `json:"unit_plural,omitempty"`
	AggregationType      types.AggregationType              `json:"aggregation_type,omitempty"`
	TotalUsage           decimal.Decimal                    `json:"total_usage" swaggertype:"string"`
	TotalCost            decimal.Decimal                    `json:"total_cost" swaggertype:"string"`
	Currency             string                             `json:"currency,omitempty"`
	EventCount           uint64                             `json:"event_count"`          // Number of events that contributed to this aggregation
	Properties           map[string]string                  `json:"properties,omitempty"` // Stores property values for flexible grouping (e.g., org_id -> "org123")
	CommitmentInfo       *types.CommitmentInfo              `json:"commitment_info,omitempty"`
	Points               []UsageAnalyticPoint               `json:"points,omitempty"`
	AddOnID              string                             `json:"add_on_id,omitempty"`
	PlanID               string                             `json:"plan_id,omitempty"`
	WindowSize           types.WindowSize                   `json:"window_size,omitempty"` // Window size for bucketed meters (only set if meter is bucketed)
}

// UsageAnalyticPoint represents a point in the time series data
type UsageAnalyticPoint struct {
	Timestamp  time.Time       `json:"timestamp"`
	Usage      decimal.Decimal `json:"usage" swaggertype:"string"`
	Cost       decimal.Decimal `json:"cost" swaggertype:"string"`
	EventCount uint64          `json:"event_count"` // Number of events in this time window

	// Commitment breakdown (only populated for windowed commitments)
	ComputedCommitmentUtilizedAmount decimal.Decimal `json:"computed_commitment_utilized_amount,omitempty" swaggertype:"string"`
	ComputedOverageAmount            decimal.Decimal `json:"computed_overage_amount,omitempty" swaggertype:"string"`
	ComputedTrueUpAmount             decimal.Decimal `json:"computed_true_up_amount,omitempty" swaggertype:"string"`
}

type GetMonitoringDataRequest struct {
	StartTime  time.Time        `json:"start_time,omitempty" form:"start_time"`
	EndTime    time.Time        `json:"end_time,omitempty" form:"end_time"`
	WindowSize types.WindowSize `json:"window_size,omitempty" form:"window_size"`
}

func (r *GetMonitoringDataRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Default to last 24 hours if start_time and end_time are not provided
	if r.StartTime.IsZero() && r.EndTime.IsZero() {
		r.EndTime = time.Now().UTC()
		r.StartTime = r.EndTime.Add(-24 * time.Hour)
	} else if r.StartTime.IsZero() || r.EndTime.IsZero() {
		return ierr.NewError("both start_time and end_time must be provided, or neither").
			WithHint("Please provide both start_time and end_time, or leave both empty for default 24 hour window").
			Mark(ierr.ErrValidation)
	}

	return nil
}

type EventCountPoint struct {
	Timestamp  time.Time `json:"timestamp"`
	EventCount uint64    `json:"event_count"`
}

type GetMonitoringDataResponse struct {
	TotalCount        uint64            `json:"total_count"`
	ConsumptionLag    int64             `json:"consumption_lag"`
	PostProcessingLag int64             `json:"post_processing_lag"`
	Points            []EventCountPoint `json:"points,omitempty"`
}

type GetHuggingFaceBillingDataRequest struct {
	EventIDs []string `json:"requestIds" binding:"required,min=1"`
}

type EventCostInfo struct {
	EventID       string          `json:"requestId"`
	CostInNanoUSD decimal.Decimal `json:"costNanoUsd" swaggertype:"string"`
}

type GetHuggingFaceBillingDataResponse struct {
	Data []EventCostInfo `json:"requests"`
}

// BenchmarkResult represents the result of running a benchmark on event processing
// This is used for comparing V1 vs V2 event processing performance
type BenchmarkResult struct {
	// Version of the prepare function used (v1 or v2)
	Version string `json:"version"`
	// Time taken to process the event in milliseconds
	DurationMs float64 `json:"duration_ms"`
	// Number of feature usage records generated
	FeatureUsageCount int `json:"feature_usage_count"`
	// Event ID that was processed
	EventID string `json:"event_id"`
	// Customer ID resolved for the event
	CustomerID string `json:"customer_id,omitempty"`
	// External customer ID from the event
	ExternalCustomerID string `json:"external_customer_id"`
	// Error message if processing failed
	Error string `json:"error,omitempty"`

	Events []*events.FeatureUsage `json:"events,omitempty"`
}

// BenchmarkRequest represents a request to benchmark event processing
type BenchmarkRequest struct {
	EventName          string                 `json:"event_name" validate:"required" binding:"required" example:"api_request"`
	EventID            string                 `json:"event_id" example:"event123"`
	CustomerID         string                 `json:"customer_id" example:"customer456"`
	ExternalCustomerID string                 `json:"external_customer_id" validate:"required" binding:"required" example:"customer456"`
	Timestamp          time.Time              `json:"timestamp" example:"2024-03-20T15:04:05Z"`
	Source             string                 `json:"source" example:"api"`
	Properties         map[string]interface{} `json:"properties" swaggertype:"object,string,number"`
}

func (r *BenchmarkRequest) Validate() error {
	return validator.ValidateRequest(r)
}

func (r *BenchmarkRequest) ToEvent(ctx context.Context) *events.Event {
	return events.NewEvent(
		r.EventName,
		types.GetTenantID(ctx),
		r.ExternalCustomerID,
		r.Properties,
		r.Timestamp,
		r.EventID,
		r.CustomerID,
		r.Source,
		types.GetEnvironmentID(ctx),
	)
}

type GetEventByIDResponse struct {
	Event           *Event                          `json:"event"`
	Status          types.EventProcessingStatusType `json:"status"`
	ProcessedEvents []*FeatureUsageInfo             `json:"processed_events,omitempty"`
	DebugTracker    *DebugTracker                   `json:"debug_tracker,omitempty"`
}

type FeatureUsageInfo struct {
	CustomerID     string    `json:"customer_id"`
	SubscriptionID string    `json:"subscription_id"`
	SubLineItemID  string    `json:"sub_line_item_id"`
	PriceID        string    `json:"price_id"`
	MeterID        string    `json:"meter_id"`
	FeatureID      string    `json:"feature_id"`
	QtyTotal       string    `json:"qty_total"`
	ProcessedAt    time.Time `json:"processed_at"`
}

type DebugTracker struct {
	CustomerLookup             *CustomerLookupResult             `json:"customer_lookup"`
	MeterMatching              *MeterMatchingResult              `json:"meter_matching"`
	PriceLookup                *PriceLookupResult                `json:"price_lookup"`
	SubscriptionLineItemLookup *SubscriptionLineItemLookupResult `json:"subscription_line_item_lookup"`
	FailurePoint               *types.FailurePoint               `json:"failure_point"`
}

type CustomerLookupResult struct {
	Status   types.DebugTrackerStatus `json:"status"`
	Customer *customer.Customer       `json:"customer,omitempty"`
	Error    *ierr.ErrorResponse      `json:"error,omitempty"`
}

type MeterMatchingResult struct {
	Status        types.DebugTrackerStatus `json:"status"`
	MatchedMeters []MatchedMeter           `json:"matched_meters,omitempty"`
	Error         *ierr.ErrorResponse      `json:"error,omitempty"`
}

type MatchedMeter struct {
	MeterID   string       `json:"meter_id"`
	EventName string       `json:"event_name"`
	Meter     *meter.Meter `json:"meter"`
}

type PriceLookupResult struct {
	Status        types.DebugTrackerStatus `json:"status"`
	MatchedPrices []MatchedPrice           `json:"matched_prices,omitempty"`
	Error         *ierr.ErrorResponse      `json:"error,omitempty"`
}

type MatchedPrice struct {
	PriceID string       `json:"price_id"`
	MeterID string       `json:"meter_id"`
	Status  string       `json:"status"`
	Price   *price.Price `json:"price"`
}

type SubscriptionLineItemLookupResult struct {
	Status           types.DebugTrackerStatus      `json:"status"`
	MatchedLineItems []MatchedSubscriptionLineItem `json:"matched_line_items,omitempty"`
	Error            *ierr.ErrorResponse           `json:"error,omitempty"`
}

type MatchedSubscriptionLineItem struct {
	SubLineItemID        string                             `json:"sub_line_item_id"`
	SubscriptionID       string                             `json:"subscription_id"`
	PriceID              string                             `json:"price_id"`
	StartDate            time.Time                          `json:"start_date"`
	EndDate              time.Time                          `json:"end_date"`
	IsActiveForEvent     bool                               `json:"is_active_for_event"`
	TimestampWithinRange bool                               `json:"timestamp_within_range"`
	SubscriptionLineItem *subscription.SubscriptionLineItem `json:"subscription_line_item,omitempty"`
}

// ReprocessEventsRequest represents the request to reprocess events
type ReprocessEventsRequest struct {
	ExternalCustomerID string `json:"external_customer_id" validate:"required" binding:"required" example:"customer456"`
	EventName          string `json:"event_name" example:"api_request"`
	StartDate          string `json:"start_date" validate:"required" binding:"required" example:"2024-01-01T00:00:00Z"`
	EndDate            string `json:"end_date" validate:"required" binding:"required" example:"2024-01-31T23:59:59Z"`
	BatchSize          int    `json:"batch_size" example:"100"`
}

// Validate validates the reprocess events request
func (r *ReprocessEventsRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Validate date format (RFC3339)
	parsedStartDate, err := time.Parse(time.RFC3339, r.StartDate)
	if err != nil {
		return ierr.NewError("invalid start_date format").
			WithHint("Start date must be in RFC3339 format (e.g., 2006-01-02T15:04:05Z07:00)").
			Mark(ierr.ErrValidation)
	}

	parsedEndDate, err := time.Parse(time.RFC3339, r.EndDate)
	if err != nil {
		return ierr.NewError("invalid end_date format").
			WithHint("End date must be in RFC3339 format (e.g., 2006-01-02T15:04:05Z07:00)").
			Mark(ierr.ErrValidation)
	}

	// Parse dates to validate start_date < end_date
	startDate := parsedStartDate
	endDate := parsedEndDate

	if startDate.After(endDate) {
		return ierr.NewError("start_date must be before end_date").
			WithHint("Start date must be before end date").
			Mark(ierr.ErrValidation)
	}

	// Validate batch size (default to 100 if not provided or invalid)
	if r.BatchSize <= 0 {
		r.BatchSize = 100 // Default batch size
	}

	return nil
}
