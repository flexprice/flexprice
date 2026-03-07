package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// PortalAnalyticsRequest represents a request for usage analytics from the customer portal
// The ExternalCustomerID is implicitly derived from the authentication context
type PortalAnalyticsRequest struct {
	FeatureIDs      []string            `json:"feature_ids,omitempty" example:"feat_123,feat_456"`
	Sources         []string            `json:"sources,omitempty" example:"api,web"`
	StartTime       time.Time           `json:"start_time,omitempty" example:"2024-01-01T00:00:00Z"`
	EndTime         time.Time           `json:"end_time,omitempty" example:"2024-01-31T23:59:59Z"`
	GroupBy         []string            `json:"group_by,omitempty" example:"source,feature_id"`
	WindowSize      types.WindowSize    `json:"window_size,omitempty" example:"DAY"`
	Expand          []string            `json:"expand,omitempty" example:"price,meter,feature"`
	PropertyFilters map[string][]string `json:"property_filters,omitempty"`
}

// ToInternalRequest converts the portal analytics request to an internal GetUsageAnalyticsRequest
// with the customer ID injected from the authentication context
func (r *PortalAnalyticsRequest) ToInternalRequest(externalCustomerID string) *GetUsageAnalyticsRequest {
	return &GetUsageAnalyticsRequest{
		ExternalCustomerID: externalCustomerID,
		FeatureIDs:         r.FeatureIDs,
		Sources:            r.Sources,
		StartTime:          r.StartTime,
		EndTime:            r.EndTime,
		GroupBy:            r.GroupBy,
		WindowSize:         r.WindowSize,
		Expand:             r.Expand,
		PropertyFilters:    r.PropertyFilters,
	}
}

// PortalCostAnalyticsRequest represents a request for cost analytics from the customer portal
// The ExternalCustomerID is implicitly derived from the authentication context
type PortalCostAnalyticsRequest struct {
	FeatureIDs []string  `json:"feature_ids,omitempty" example:"feat_123,feat_456"`
	StartTime  time.Time `json:"start_time" binding:"required" example:"2024-01-01T00:00:00Z"`
	EndTime    time.Time `json:"end_time" binding:"required" example:"2024-01-31T23:59:59Z"`
	// Expand specifies which related entities to include. Supported values: "meter", "price"
	Expand []string `json:"expand,omitempty" example:"meter,price"`
}

// ToInternalRequest converts the portal cost analytics request to an internal GetCostAnalyticsRequest
// with the customer ID injected from the authentication context
func (r *PortalCostAnalyticsRequest) ToInternalRequest(externalCustomerID string) *GetCostAnalyticsRequest {
	return &GetCostAnalyticsRequest{
		ExternalCustomerID: externalCustomerID,
		FeatureIDs:         r.FeatureIDs,
		StartTime:          r.StartTime,
		EndTime:            r.EndTime,
		Expand:             r.Expand,
	}
}

// PortalPaginatedRequest represents a paginated request from the customer portal
type PortalPaginatedRequest struct {
	Page  int `form:"page" json:"page" example:"1"`
	Limit int `form:"limit" json:"limit" example:"20"`
}
