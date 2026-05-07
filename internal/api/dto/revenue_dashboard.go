package dto

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// RevenueDashboardRequest represents the request for the revenue dashboard API
type RevenueDashboardRequest struct {
	PeriodStart time.Time `json:"period_start" binding:"required"`
	PeriodEnd   time.Time `json:"period_end" binding:"required"`
	CustomerIDs []string  `json:"customer_ids,omitempty"`
}

// Validate validates the revenue dashboard request
func (r *RevenueDashboardRequest) Validate() error {
	if r.PeriodStart.IsZero() {
		return ierr.NewError("period_start is required").
			WithHint("period_start must be provided").
			Mark(ierr.ErrValidation)
	}
	if r.PeriodEnd.IsZero() {
		return ierr.NewError("period_end is required").
			WithHint("period_end must be provided").
			Mark(ierr.ErrValidation)
	}
	if !r.PeriodEnd.After(r.PeriodStart) {
		return ierr.NewError("period_end must be after period_start").
			WithHint("period_end must be after period_start").
			WithReportableDetails(map[string]interface{}{
				"period_start": r.PeriodStart,
				"period_end":   r.PeriodEnd,
			}).
			Mark(ierr.ErrValidation)
	}
	const maxCustomerIDs = 1000
	if len(r.CustomerIDs) > maxCustomerIDs {
		return ierr.NewError("customer_ids exceeds maximum allowed count").
			WithHint("customer_ids must not exceed 1000 entries").
			WithReportableDetails(map[string]interface{}{
				"count": len(r.CustomerIDs),
				"max":   maxCustomerIDs,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// RevenueDashboardResponse represents the response for the revenue dashboard API
type RevenueDashboardResponse struct {
	Summary RevenueDashboardSummary    `json:"summary"`
	Items   []RevenueDashboardCustomer `json:"items"`
	// Graph is set only when custom_analytics_config includes the revenue-per-minute rule (same gate as summary cpm / voice_minutes).
	Graph *RevenueDashboardGraph `json:"graph,omitempty"`
}

// RevenueDashboardGraph holds time-bucketed series for bar charts (total revenue and voice minutes).
type RevenueDashboardGraph struct {
	TotalRevenue []types.RevenueGraphPoint `json:"total_revenue"`
	VoiceMinutes []types.RevenueGraphPoint `json:"voice_minutes,omitempty"`
}

// RevenueDashboardSummary represents aggregate revenue metrics across all customers
type RevenueDashboardSummary struct {
	TotalRevenue      decimal.Decimal  `json:"total_revenue" swaggertype:"string"`
	TotalUsageRevenue decimal.Decimal  `json:"total_usage_revenue" swaggertype:"string"`
	TotalFixedRevenue decimal.Decimal  `json:"total_fixed_revenue" swaggertype:"string"`
	CPM               *decimal.Decimal `json:"cpm,omitempty" swaggertype:"string"`
	VoiceMinutes      *decimal.Decimal `json:"voice_minutes,omitempty" swaggertype:"string"`
}

// RevenueDashboardCustomer represents per-customer revenue data
type RevenueDashboardCustomer struct {
	CustomerID         string           `json:"customer_id"`
	CustomerName       string           `json:"customer_name"`
	ExternalCustomerID string           `json:"external_customer_id"`
	TotalRevenue       decimal.Decimal  `json:"total_revenue" swaggertype:"string"`
	TotalUsageRevenue  decimal.Decimal  `json:"total_usage_revenue" swaggertype:"string"`
	TotalFixedRevenue  decimal.Decimal  `json:"total_fixed_revenue" swaggertype:"string"`
	CPM                *decimal.Decimal `json:"cpm,omitempty" swaggertype:"string"`
	VoiceMinutes       *decimal.Decimal `json:"voice_minutes,omitempty" swaggertype:"string"`
}
