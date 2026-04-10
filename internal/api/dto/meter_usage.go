package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// MeterUsageQueryRequest is the request for POST /v1/meter-usage/query (single meter)
type MeterUsageQueryRequest struct {
	ExternalCustomerID string                `json:"external_customer_id" validate:"required" binding:"required" example:"cust_123"`
	MeterID            string                `json:"meter_id" validate:"required" binding:"required" example:"mtr_abc"`
	StartTime          time.Time             `json:"start_time" validate:"required" binding:"required" example:"2024-01-01T00:00:00Z"`
	EndTime            time.Time             `json:"end_time" validate:"required" binding:"required" example:"2024-02-01T00:00:00Z"`
	AggregationType    types.AggregationType `json:"aggregation_type" validate:"required" binding:"required" example:"SUM"`
	WindowSize         types.WindowSize      `json:"window_size,omitempty" example:"DAY"`
	BillingAnchor      *time.Time            `json:"billing_anchor,omitempty" example:"2024-01-15T00:00:00Z"`
}

func (r *MeterUsageQueryRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// ToParams converts the DTO to domain query params
func (r *MeterUsageQueryRequest) ToParams(tenantID, environmentID string) *events.MeterUsageQueryParams {
	return &events.MeterUsageQueryParams{
		TenantID:           tenantID,
		EnvironmentID:      environmentID,
		ExternalCustomerID: r.ExternalCustomerID,
		MeterID:            r.MeterID,
		StartTime:          r.StartTime,
		EndTime:            r.EndTime,
		AggregationType:    r.AggregationType,
		WindowSize:         r.WindowSize,
		BillingAnchor:      r.BillingAnchor,
		UseFinal:           true, // billing queries use FINAL
	}
}

// MeterUsageQueryResponse is the response for single-meter query
type MeterUsageQueryResponse struct {
	MeterID         string                `json:"meter_id" example:"mtr_abc"`
	AggregationType types.AggregationType `json:"aggregation_type" example:"SUM"`
	TotalValue      decimal.Decimal       `json:"total_value" swaggertype:"string" example:"1234.5678"`
	EventCount      uint64                `json:"event_count" example:"42"`
	Points          []MeterUsagePoint     `json:"points,omitempty"`
}

// MeterUsagePoint is a single time-bucketed data point
type MeterUsagePoint struct {
	Timestamp  time.Time       `json:"timestamp" example:"2024-01-01T00:00:00Z"`
	Value      decimal.Decimal `json:"value" swaggertype:"string" example:"100.0000"`
	EventCount uint64          `json:"event_count" example:"10"`
}

// MeterUsageAnalyticsRequest is the request for POST /v1/meter-usage/analytics (multi-meter)
type MeterUsageAnalyticsRequest struct {
	ExternalCustomerID string                `json:"external_customer_id" validate:"required" binding:"required" example:"cust_123"`
	MeterIDs           []string              `json:"meter_ids" validate:"required,min=1" binding:"required" example:"mtr_abc,mtr_def"`
	StartTime          time.Time             `json:"start_time" validate:"required" binding:"required" example:"2024-01-01T00:00:00Z"`
	EndTime            time.Time             `json:"end_time" validate:"required" binding:"required" example:"2024-02-01T00:00:00Z"`
	AggregationType    types.AggregationType `json:"aggregation_type" validate:"required" binding:"required" example:"SUM"`
	WindowSize         types.WindowSize      `json:"window_size,omitempty" example:"DAY"`
	BillingAnchor      *time.Time            `json:"billing_anchor,omitempty"`
}

func (r *MeterUsageAnalyticsRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// ToParams converts to domain query params
func (r *MeterUsageAnalyticsRequest) ToParams(tenantID, environmentID string) *events.MeterUsageQueryParams {
	return &events.MeterUsageQueryParams{
		TenantID:           tenantID,
		EnvironmentID:      environmentID,
		ExternalCustomerID: r.ExternalCustomerID,
		MeterIDs:           r.MeterIDs,
		StartTime:          r.StartTime,
		EndTime:            r.EndTime,
		AggregationType:    r.AggregationType,
		WindowSize:         r.WindowSize,
		BillingAnchor:      r.BillingAnchor,
		UseFinal:           true,
	}
}

// MeterUsageAnalyticsResponse wraps multi-meter results
type MeterUsageAnalyticsResponse struct {
	Items []MeterUsageQueryResponse `json:"items"`
}

// ToMeterUsageQueryResponse converts domain result to DTO response
func ToMeterUsageQueryResponse(result *events.MeterUsageAggregationResult) MeterUsageQueryResponse {
	points := make([]MeterUsagePoint, 0, len(result.Points))
	for _, p := range result.Points {
		points = append(points, MeterUsagePoint{
			Timestamp:  p.WindowStart,
			Value:      p.Value,
			EventCount: p.EventCount,
		})
	}

	return MeterUsageQueryResponse{
		MeterID:         result.MeterID,
		AggregationType: result.AggregationType,
		TotalValue:      result.TotalValue,
		EventCount:      result.EventCount,
		Points:          points,
	}
}

// ToMeterUsageAnalyticsResponse converts multiple domain results to DTO response
func ToMeterUsageAnalyticsResponse(results []*events.MeterUsageAggregationResult) *MeterUsageAnalyticsResponse {
	items := make([]MeterUsageQueryResponse, 0, len(results))
	for _, r := range results {
		items = append(items, ToMeterUsageQueryResponse(r))
	}
	return &MeterUsageAnalyticsResponse{Items: items}
}

// --- Detailed Analytics ---

// MeterUsageDetailedAnalyticsRequest is the request for POST /v1/meter-usage/detailed-analytics
type MeterUsageDetailedAnalyticsRequest struct {
	ExternalCustomerID string              `json:"external_customer_id" validate:"required" binding:"required" example:"cust_123"`
	MeterIDs           []string            `json:"meter_ids,omitempty" example:"mtr_abc,mtr_def"`
	StartTime          time.Time           `json:"start_time,omitempty" example:"2024-01-01T00:00:00Z"`
	EndTime            time.Time           `json:"end_time,omitempty" example:"2024-02-01T00:00:00Z"`
	GroupBy            []string            `json:"group_by,omitempty" example:"meter_id,source"`
	PropertyFilters    map[string][]string `json:"property_filters,omitempty"`
	Sources            []string            `json:"sources,omitempty"`
	WindowSize         types.WindowSize    `json:"window_size,omitempty" example:"DAY"`
	BillingAnchor      *time.Time          `json:"billing_anchor,omitempty"`
}

func (r *MeterUsageDetailedAnalyticsRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// ToParams converts the DTO to domain detailed analytics params
func (r *MeterUsageDetailedAnalyticsRequest) ToParams(tenantID, environmentID string) *events.MeterUsageDetailedAnalyticsParams {
	return &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           tenantID,
		EnvironmentID:      environmentID,
		ExternalCustomerID: r.ExternalCustomerID,
		MeterIDs:           r.MeterIDs,
		StartTime:          r.StartTime,
		EndTime:            r.EndTime,
		GroupBy:            r.GroupBy,
		PropertyFilters:    r.PropertyFilters,
		Sources:            r.Sources,
		WindowSize:         r.WindowSize,
		BillingAnchor:      r.BillingAnchor,
	}
}

// MeterUsageDetailedAnalyticsResponse wraps detailed analytics results
type MeterUsageDetailedAnalyticsResponse struct {
	Items []MeterUsageDetailedItem `json:"items"`
}

// MeterUsageDetailedItem represents a single group's analytics data
type MeterUsageDetailedItem struct {
	MeterID          string                       `json:"meter_id,omitempty" example:"mtr_abc"`
	Source           string                       `json:"source,omitempty" example:"api"`
	Sources          []string                     `json:"sources,omitempty"`
	Properties       map[string]string            `json:"properties,omitempty"`
	TotalUsage       decimal.Decimal              `json:"total_usage" swaggertype:"string" example:"1234.5678"`
	MaxUsage         decimal.Decimal              `json:"max_usage" swaggertype:"string" example:"100.0000"`
	LatestUsage      decimal.Decimal              `json:"latest_usage" swaggertype:"string" example:"50.0000"`
	CountUniqueUsage uint64                       `json:"count_unique_usage" example:"25"`
	EventCount       uint64                       `json:"event_count" example:"42"`
	Points           []MeterUsageDetailedPointDTO `json:"points,omitempty"`
}

// MeterUsageDetailedPointDTO is a single time-bucketed analytics point
type MeterUsageDetailedPointDTO struct {
	Timestamp        time.Time       `json:"timestamp" example:"2024-01-01T00:00:00Z"`
	TotalUsage       decimal.Decimal `json:"total_usage" swaggertype:"string" example:"100.0000"`
	MaxUsage         decimal.Decimal `json:"max_usage" swaggertype:"string" example:"50.0000"`
	LatestUsage      decimal.Decimal `json:"latest_usage" swaggertype:"string" example:"25.0000"`
	CountUniqueUsage uint64          `json:"count_unique_usage" example:"10"`
	EventCount       uint64          `json:"event_count" example:"20"`
}

// ToMeterUsageDetailedAnalyticsResponse converts domain results to DTO response
func ToMeterUsageDetailedAnalyticsResponse(results []*events.MeterUsageDetailedResult) *MeterUsageDetailedAnalyticsResponse {
	items := make([]MeterUsageDetailedItem, 0, len(results))
	for _, r := range results {
		item := MeterUsageDetailedItem{
			MeterID:          r.MeterID,
			Source:           r.Source,
			Sources:          r.Sources,
			Properties:       r.Properties,
			TotalUsage:       r.TotalUsage,
			MaxUsage:         r.MaxUsage,
			LatestUsage:      r.LatestUsage,
			CountUniqueUsage: r.CountUniqueUsage,
			EventCount:       r.EventCount,
		}

		if len(r.Points) > 0 {
			item.Points = make([]MeterUsageDetailedPointDTO, 0, len(r.Points))
			for _, p := range r.Points {
				item.Points = append(item.Points, MeterUsageDetailedPointDTO{
					Timestamp:        p.WindowStart,
					TotalUsage:       p.TotalUsage,
					MaxUsage:         p.MaxUsage,
					LatestUsage:      p.LatestUsage,
					CountUniqueUsage: p.CountUniqueUsage,
					EventCount:       p.EventCount,
				})
			}
		}

		items = append(items, item)
	}
	return &MeterUsageDetailedAnalyticsResponse{Items: items}
}
