package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CalculateROIRequest represents the request to calculate ROI for a cost sheet
type CalculateROIRequest struct {
	// SubscriptionID is required to get subscription details
	SubscriptionID string `json:"subscription_id" validate:"required"`

	// MeterID references the meter to track usage
	MeterID string `json:"meter_id,omitempty"`

	// PriceID references the price configuration
	PriceID string `json:"price_id,omitempty"`

	// Optional time range. If not provided, uses entire subscription period
	PeriodStart *time.Time `json:"period_start,omitempty"`
	PeriodEnd   *time.Time `json:"period_end,omitempty"`
}

// CreateCostsheetRequest represents the request to create a new costsheet.
type CreateCostSheetRequest struct {
	// MeterID references the meter to track usage
	MeterID string `json:"meter_id" validate:"required"`

	// PriceID references the price configuration
	PriceID string `json:"price_id" validate:"required"`
}

// GetCostBreakdownRequest represents the request to calculate costs for a time period.
type GetCostBreakdownRequest struct {
	// SubscriptionID to get the time period from if StartTime and EndTime are not provided
	SubscriptionID string `json:"subscription_id" validate:"required"`

	// Optional time range. If not provided, uses subscription period
	StartTime *time.Time `json:"start_time,omitempty"`
	EndTime   *time.Time `json:"end_time,omitempty"`
}

// CostBreakdownResponse represents the calculated costs for a period.
type CostBreakdownResponse struct {
	// TotalCost is the sum of all meter costs
	TotalCost decimal.Decimal `json:"total_cost"`

	// Items contains the breakdown by meter
	Items []CostBreakdownItem `json:"items"`
}

// CostBreakdownItem represents the cost calculation for a single meter.
type CostBreakdownItem struct {
	// MeterID identifies the usage meter
	MeterID string `json:"meter_id"`

	// MeterName is the display name of the meter
	MeterName string `json:"meter_name"`

	// Usage is the quantity consumed
	Usage decimal.Decimal `json:"usage"`

	// Cost is the calculated cost for this meter
	Cost decimal.Decimal `json:"cost"`
}

// UpdateCostSheetRequest represents the request to update an existing costsheet.
type UpdateCostSheetRequest struct {
	// ID of the costsheet to update
	ID string `json:"id" validate:"required"`

	// Status updates the costsheet's status (optional)
	Status string `json:"status,omitempty"`
}

// CostsheetResponse represents a cost sheet in API responses
type CostSheetResponse struct {
	ID        string       `json:"id"`
	MeterID   string       `json:"meter_id"`
	PriceID   string       `json:"price_id"`
	Status    types.Status `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// ListCostSheetsResponse represents the response for listing cost sheets
type ListCostSheetsResponse struct {
	Items []CostSheetResponse `json:"items"`
	Total int                 `json:"total"`
}

// ROIResponse represents the detailed response for ROI calculations
type ROIResponse struct {
	// Cost and Revenue
	Cost    decimal.Decimal `json:"cost"`
	Revenue decimal.Decimal `json:"revenue"`

	// Net Revenue (Revenue - Cost)
	NetRevenue decimal.Decimal `json:"net_revenue"`

	// Markup (Revenue - Cost / Cost)
	Markup           decimal.Decimal `json:"markup"`
	MarkupPercentage decimal.Decimal `json:"markup_percentage"`

	// Net Margin (ROI)
	NetMargin           decimal.Decimal `json:"net_margin"`
	NetMarginPercentage decimal.Decimal `json:"net_margin_percentage"`

	// Cost breakdown by meter
	CostBreakdown []CostBreakdownItem `json:"cost_breakdown"`
}
