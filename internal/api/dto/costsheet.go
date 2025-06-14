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

	// PeriodStart and PeriodEnd define the time range for ROI calculation
	PeriodStart time.Time `json:"period_start" validate:"required"`
	PeriodEnd   time.Time `json:"period_end" validate:"required"`

	// MeterID references the meter to track usage
	MeterID string `json:"meter_id" validate:"required"`

	// PriceID references the price configuration
	PriceID string `json:"price_id" validate:"required"`
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
	// StartTime defines the beginning of the period
	StartTime time.Time `json:"start_time" validate:"required"`

	// EndTime defines the end of the period
	EndTime time.Time `json:"end_time" validate:"required"`
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
	// Total revenue generated from the subscription
	Revenue decimal.Decimal `json:"revenue"`

	// Total cost breakdown
	Costs struct {
		Total decimal.Decimal     `json:"total"`
		Items []CostBreakdownItem `json:"items"`
	} `json:"costs"`

	// Calculated metrics
	NetRevenue decimal.Decimal `json:"net_revenue"` // Revenue - Total Cost
	ROI        decimal.Decimal `json:"roi"`         // (Revenue - Cost) / Cost
	Margin     decimal.Decimal `json:"margin"`      // (Revenue - Cost) / Revenue
}
