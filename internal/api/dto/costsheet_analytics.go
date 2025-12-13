package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// GetCostAnalyticsRequest represents the request to get cost analytics
type GetCostAnalyticsRequest struct {
	// Time range fields (optional - defaults to last 7 days if not provided)
	StartTime time.Time `json:"start_time,omitempty"`
	EndTime   time.Time `json:"end_time,omitempty"`

	ExternalCustomerID string `json:"external_customer_id,omitempty"` // Optional - for specific customer

	// Additional filters
	FeatureIDs []string `json:"feature_ids,omitempty"`

	// Expand options - specify which entities to expand
	Expand []string `json:"expand,omitempty"` // "meter", "price"

	// Pagination
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// Validate validates the cost analytics request and sets defaults
func (r *GetCostAnalyticsRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Set default time range to last 7 days if not provided
	if r.StartTime.IsZero() && r.EndTime.IsZero() {
		now := time.Now().UTC()
		r.EndTime = now
		r.StartTime = now.Add(-7 * 24 * time.Hour)
	} else if r.StartTime.IsZero() || r.EndTime.IsZero() {
		return ierr.NewError("both start_time and end_time must be provided if one is specified").
			WithHint("Please provide both start_time and end_time, or omit both for default 7-day range").
			Mark(ierr.ErrValidation)
	}

	// Validate expand options
	validExpandOptions := map[string]bool{
		"meter": true,
		"price": true,
	}
	for _, expand := range r.Expand {
		if !validExpandOptions[expand] {
			return ierr.NewError("invalid expand option").
				WithHint("valid expand options are: meter, price").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// CostAnalyticItem represents a single cost analytics item
type CostAnalyticItem struct {
	MeterID            string            `json:"meter_id"`
	MeterName          string            `json:"meter_name,omitempty"`
	Source             string            `json:"source,omitempty"`
	CustomerID         string            `json:"customer_id,omitempty"`
	ExternalCustomerID string            `json:"external_customer_id,omitempty"`
	Properties         map[string]string `json:"properties,omitempty"`

	// Aggregated metrics
	TotalCost     decimal.Decimal `json:"total_cost" swaggertype:"string"`
	TotalQuantity decimal.Decimal `json:"total_quantity" swaggertype:"string"`
	TotalEvents   int64           `json:"total_events"`

	// Breakdown
	CostByPeriod []CostPoint `json:"cost_by_period,omitempty"` // Time-series

	// Metadata
	Currency    string `json:"currency"`
	PriceID     string `json:"price_id,omitempty"`
	CostsheetID string `json:"costsheet_id,omitempty"`

	// Expanded data (populated when expand options are specified)
	Meter *meter.Meter `json:"meter,omitempty"`
	Price *price.Price `json:"price,omitempty"`
}

// CostPoint represents a single point in cost time-series data
type CostPoint struct {
	Timestamp  time.Time       `json:"timestamp"`
	Cost       decimal.Decimal `json:"cost" swaggertype:"string"`
	Quantity   decimal.Decimal `json:"quantity" swaggertype:"string"`
	EventCount int64           `json:"event_count"`
}

// GetCostAnalyticsResponse represents the response for cost analytics
type GetCostAnalyticsResponse struct {
	CustomerID         string    `json:"customer_id,omitempty"`
	ExternalCustomerID string    `json:"external_customer_id,omitempty"`
	CostsheetID        string    `json:"costsheet_id,omitempty"`
	StartTime          time.Time `json:"start_time"`
	EndTime            time.Time `json:"end_time"`
	Currency           string    `json:"currency"`

	// Summary
	TotalCost     decimal.Decimal `json:"total_cost" swaggertype:"string"`
	TotalQuantity decimal.Decimal `json:"total_quantity" swaggertype:"string"`
	TotalEvents   int64           `json:"total_events"`

	// Detailed breakdown
	CostAnalytics []CostAnalyticItem `json:"cost_analytics"`

	// Time-series (if requested)
	CostTimeSeries []CostPoint `json:"cost_time_series,omitempty"`

	// Pagination
	Pagination *types.PaginationResponse `json:"pagination,omitempty"`
}

// GetCombinedAnalyticsResponse represents the response for combined cost and revenue analytics
type GetDetailedCostAnalyticsResponse struct {
	// Cost analytics array (flattened from nested structure)
	CostAnalytics []CostAnalyticItem `json:"cost_analytics"`

	// Derived metrics
	TotalRevenue  decimal.Decimal `json:"total_revenue" swaggertype:"string"`
	TotalCost     decimal.Decimal `json:"total_cost" swaggertype:"string"`
	Margin        decimal.Decimal `json:"margin" swaggertype:"string"`         // Revenue - Cost
	MarginPercent decimal.Decimal `json:"margin_percent" swaggertype:"string"` // (Margin / Revenue) * 100
	ROI           decimal.Decimal `json:"roi" swaggertype:"string"`            // (Revenue - Cost) / Cost
	ROIPercent    decimal.Decimal `json:"roi_percent" swaggertype:"string"`    // ROI * 100

	Currency  string    `json:"currency"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}
