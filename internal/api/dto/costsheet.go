package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/costsheet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// CreateCostsheetRequest represents the request to create a new costsheet
type CreateCostsheetRequest struct {
	Name        string            `json:"name" validate:"required,min=1,max=255"`
	LookupKey   string            `json:"lookup_key,omitempty" validate:"omitempty,min=1,max=255"`
	Description string            `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Validate validates the create costsheet request
func (r *CreateCostsheetRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// ToCostsheet converts the request to a domain model
func (r *CreateCostsheetRequest) ToCostsheet(ctx context.Context) *costsheet.Costsheet {
	baseModel := types.GetDefaultBaseModel(ctx)
	return &costsheet.Costsheet{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COSTSHEET),
		Name:          r.Name,
		LookupKey:     r.LookupKey,
		Description:   r.Description,
		Metadata:      r.Metadata,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     baseModel,
	}
}

// UpdateCostsheetRequest represents the request to update an existing costsheet
type UpdateCostsheetRequest struct {
	Name        string            `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	LookupKey   string            `json:"lookup_key,omitempty" validate:"omitempty,min=1,max=255"`
	Description string            `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Validate validates the update costsheet request
func (r *UpdateCostsheetRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// UpdateCostsheet updates the costsheet with the provided data
func (r *UpdateCostsheetRequest) UpdateCostsheet(costsheet *costsheet.Costsheet, ctx context.Context) {
	if r.Name != "" {
		costsheet.Name = r.Name
	}
	if r.LookupKey != "" {
		costsheet.LookupKey = r.LookupKey
	}
	if r.Description != "" {
		costsheet.Description = r.Description
	}
	if r.Metadata != nil {
		costsheet.Metadata = r.Metadata
	}
}

// CostsheetResponse represents a costsheet in API responses
type CostsheetResponse struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	LookupKey     string            `json:"lookup_key,omitempty"`
	Description   string            `json:"description,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	TenantID      string            `json:"tenant_id"`
	EnvironmentID string            `json:"environment_id"`
	Status        types.Status      `json:"status"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	CreatedBy     string            `json:"created_by,omitempty"`
	UpdatedBy     string            `json:"updated_by,omitempty"`
	Prices        []*PriceResponse  `json:"prices,omitempty"` // Associated prices
}

// ToCostsheetResponse converts a domain model to response DTO
func ToCostsheetResponse(costsheet *costsheet.Costsheet) *CostsheetResponse {
	return &CostsheetResponse{
		ID:            costsheet.ID,
		Name:          costsheet.Name,
		LookupKey:     costsheet.LookupKey,
		Description:   costsheet.Description,
		Metadata:      costsheet.Metadata,
		TenantID:      costsheet.TenantID,
		EnvironmentID: costsheet.EnvironmentID,
		Status:        costsheet.Status,
		CreatedAt:     costsheet.CreatedAt,
		UpdatedAt:     costsheet.UpdatedAt,
		CreatedBy:     costsheet.CreatedBy,
		UpdatedBy:     costsheet.UpdatedBy,
		Prices:        nil, // Prices will be populated by the service layer when expanded
	}
}

// ToCostsheetResponseWithPrices converts a domain model to response DTO with prices
func ToCostsheetResponseWithPrices(costsheet *costsheet.Costsheet, prices []*PriceResponse) *CostsheetResponse {
	resp := ToCostsheetResponse(costsheet)
	resp.Prices = prices
	return resp
}

// ToCostsheet converts response DTO to domain model
func (r *CostsheetResponse) ToCostsheet() *costsheet.Costsheet {
	return &costsheet.Costsheet{
		ID:            r.ID,
		Name:          r.Name,
		LookupKey:     r.LookupKey,
		Description:   r.Description,
		Metadata:      r.Metadata,
		EnvironmentID: r.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  r.TenantID,
			Status:    r.Status,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			CreatedBy: r.CreatedBy,
			UpdatedBy: r.UpdatedBy,
		},
	}
}

// CreateCostsheetResponse represents the response for creating a costsheet
type CreateCostsheetResponse struct {
	Costsheet *CostsheetResponse `json:"costsheet"`
}

// ListCostsheetResponse represents the response for listing costsheet records
type ListCostsheetResponse struct {
	Items      []*CostsheetResponse      `json:"items"`
	Pagination *types.PaginationResponse `json:"pagination"`
}

// GetCostsheetResponse represents the response for getting a single costsheet
type GetCostsheetResponse struct {
	Costsheet *CostsheetResponse `json:"costsheet"`
}

// UpdateCostsheetResponse represents the response for updating a costsheet
type UpdateCostsheetResponse struct {
	Costsheet *CostsheetResponse `json:"costsheet"`
}

// DeleteCostsheetResponse represents the response for deleting a costsheet
type DeleteCostsheetResponse struct {
	Message string `json:"message"`
	ID      string `json:"id"`
}

// Legacy DTOs for backward compatibility

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

// CreateCostSheetRequest represents the legacy request to create a costsheet (deprecated)
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
	TotalCost decimal.Decimal `json:"total_cost" swaggertype:"string"`

	// Items contains the breakdown by meter
	Items []CostBreakdownItem `json:"items"`

	// Period shows the time range for this calculation
	Period CostPeriod `json:"period"`
}

// CostBreakdownItem represents the cost for a single meter.
type CostBreakdownItem struct {
	// MeterID identifies the meter
	MeterID string `json:"meter_id"`

	// MeterName is the display name of the meter
	MeterName string `json:"meter_name"`

	// Cost is the calculated cost for this meter
	Cost decimal.Decimal `json:"cost" swaggertype:"string"`

	// Usage is the total usage for this meter in the period
	Usage decimal.Decimal `json:"usage" swaggertype:"string"`

	// Unit is the unit of measurement
	Unit string `json:"unit"`
}

// CostPeriod represents a time period for cost calculations.
type CostPeriod struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

// ROIResponse represents the calculated ROI metrics.
type ROIResponse struct {
	// ROI is the return on investment percentage
	ROI decimal.Decimal `json:"roi" swaggertype:"string"`

	// TotalRevenue is the total revenue for the period
	TotalRevenue decimal.Decimal `json:"total_revenue" swaggertype:"string"`

	// TotalCost is the total cost for the period
	TotalCost decimal.Decimal `json:"total_cost" swaggertype:"string"`

	// Profit is the difference between revenue and cost
	Profit decimal.Decimal `json:"profit" swaggertype:"string"`

	// Period shows the time range for this calculation
	Period CostPeriod `json:"period"`
}

// CostSheetResponse represents a legacy costsheet response (deprecated)
type CostSheetResponse struct {
	ID       string `json:"id"`
	MeterID  string `json:"meter_id"`
	PriceID  string `json:"price_id"`
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`
}

// ListCostSheetsResponse represents the legacy response for listing costsheets (deprecated)
type ListCostSheetsResponse struct {
	Items      []*CostSheetResponse      `json:"items"`
	Pagination *types.PaginationResponse `json:"pagination"`
}

// UpdateCostSheetRequest represents the legacy request to update a costsheet (deprecated)
type UpdateCostSheetRequest struct {
	MeterID string `json:"meter_id,omitempty"`
	PriceID string `json:"price_id,omitempty"`
}
