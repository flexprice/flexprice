package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCostsheet "github.com/flexprice/flexprice/internal/domain/costsheet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// CostSheetHandler handles HTTP requests for cost sheet operations.
type CostSheetHandler struct {
	service service.CostSheetService
	log     *logger.Logger
}

// NewCostSheetHandler creates a new instance of CostSheetHandler.
func NewCostSheetHandler(service service.CostSheetService, log *logger.Logger) *CostSheetHandler {
	return &CostSheetHandler{
		service: service,
		log:     log,
	}
}

// @Summary Create a new cost sheet
// @Description Create a new cost sheet with the specified configuration
// @Tags CostSheets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param costsheet body dto.CreateCostSheetRequest true "Cost sheet configuration"
// @Success 201 {object} dto.CostSheetResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 409 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /cost [post]
func (h *CostSheetHandler) CreateCostSheet(c *gin.Context) {
	var req dto.CreateCostSheetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	// Check for existing published costsheet with same meter_id and price_id
	filter := &domainCostsheet.Filter{
		QueryFilter: types.NewDefaultQueryFilter(),
		MeterIDs:    []string{req.MeterID},
		PriceIDs:    []string{req.PriceID},
		Status:      types.CostsheetStatusPublished,
	}

	// Get tenant and environment from context
	tenantID, envID := domainCostsheet.GetTenantAndEnvFromContext(c.Request.Context())
	filter.TenantID = tenantID
	filter.EnvironmentID = envID

	existing, err := h.service.ListCostSheets(c.Request.Context(), filter)
	if err != nil {
		h.log.Error("Failed to check for existing costsheet", "error", err)
		c.Error(err)
		return
	}

	if existing != nil && len(existing.Items) > 0 {
		c.Error(ierr.NewError("costsheet already exists").
			WithHint("A published costsheet with this meter and price combination already exists").
			Mark(ierr.ErrAlreadyExists))
		return
	}

	resp, err := h.service.CreateCostSheet(c.Request.Context(), &req)
	if err != nil {
		h.log.Error("Failed to create cost sheet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get a cost sheet by ID
// @Description Get a cost sheet by ID
// @Tags CostSheets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Cost Sheet ID"
// @Success 200 {object} dto.CostSheetResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /cost/{id} [get]
func (h *CostSheetHandler) GetCostSheet(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Cost Sheet ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetCostSheet(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to get cost sheet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary List cost sheets
// @Description List cost sheets with optional filtering
// @Tags CostSheets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query domainCostsheet.Filter false "Filter"
// @Success 200 {object} dto.ListCostSheetsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /cost [get]
func (h *CostSheetHandler) ListCostSheets(c *gin.Context) {
	var filter domainCostsheet.Filter
	if err := c.ShouldBindQuery(&filter); err != nil {
		h.log.Error("Failed to bind query parameters", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	// Initialize QueryFilter if not set
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	// Get tenant and environment from context
	tenantID, envID := domainCostsheet.GetTenantAndEnvFromContext(c.Request.Context())
	filter.TenantID = tenantID
	filter.EnvironmentID = envID

	resp, err := h.service.ListCostSheets(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list cost sheets", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update a cost sheet
// @Description Update a cost sheet with the specified configuration
// @Tags CostSheets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Cost Sheet ID"
// @Param costsheet body dto.UpdateCostSheetRequest true "Cost sheet configuration"
// @Success 200 {object} dto.CostSheetResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /cost/{id} [put]
func (h *CostSheetHandler) UpdateCostSheet(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Cost Sheet ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateCostSheetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	req.ID = id // Set the ID from path parameter

	resp, err := h.service.UpdateCostSheet(c.Request.Context(), &req)
	if err != nil {
		h.log.Error("Failed to update cost sheet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete a cost sheet
// @Description Delete a cost sheet
// @Tags CostSheets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Cost Sheet ID"
// @Success 204 "No Content"
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 409 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /cost/{id} [delete]
func (h *CostSheetHandler) DeleteCostSheet(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Cost Sheet ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	// Get the costsheet first to check its status
	costsheet, err := h.service.GetCostSheet(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to get cost sheet", "error", err)
		c.Error(err)
		return
	}

	// Check if the costsheet is published and has active references
	if costsheet.Status == types.StatusPublished {
		// TODO: Add check for active calculations or reports
		// For now, we'll prevent deletion of published costsheets
		c.Error(ierr.NewError("cannot delete published costsheet").
			WithHint("Published costsheets cannot be deleted while they have active references").
			Mark(ierr.ErrAlreadyExists))
		return
	}

	if err := h.service.DeleteCostSheet(c.Request.Context(), id); err != nil {
		h.log.Error("Failed to delete cost sheet", "error", err)
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary Get cost breakdown
// @Description Get cost breakdown for a time period
// @Tags CostSheets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param subscription_id path string true "Subscription ID"
// @Param start_time query string false "Start time (RFC3339)"
// @Param end_time query string false "End time (RFC3339)"
// @Success 200 {object} dto.CostBreakdownResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /cost/breakdown/{subscription_id} [get]
func (h *CostSheetHandler) GetCostBreakDown(c *gin.Context) {
	// Get subscription_id from path
	subscriptionID := c.Param("subscription_id")
	if subscriptionID == "" {
		c.Error(ierr.NewError("subscription_id is required").
			WithHint("Please provide a subscription ID in the URL").
			Mark(ierr.ErrValidation))
		return
	}

	// Optional query parameters for custom time range
	var startTime, endTime *time.Time
	if startStr := c.Query("start_time"); startStr != "" {
		t, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			c.Error(ierr.WithError(err).
				WithHint("Invalid start_time format. Use RFC3339 format").
				Mark(ierr.ErrValidation))
			return
		}
		startTime = &t
	}
	if endStr := c.Query("end_time"); endStr != "" {
		t, err := time.Parse(time.RFC3339, endStr)
		if err != nil {
			c.Error(ierr.WithError(err).
				WithHint("Invalid end_time format. Use RFC3339 format").
				Mark(ierr.ErrValidation))
			return
		}
		endTime = &t
	}

	// Get subscription details
	sub, err := h.service.GetSubscriptionDetails(c.Request.Context(), subscriptionID)
	if err != nil {
		h.log.Error("Failed to get subscription", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Failed to get subscription details").
			Mark(ierr.ErrInternal))
		return
	}

	// Use subscription period if time range not provided
	periodStart := sub.CurrentPeriodStart
	periodEnd := sub.CurrentPeriodEnd

	if startTime != nil && endTime != nil {
		periodStart = *startTime
		periodEnd = *endTime

		// Validate time range if provided
		if periodEnd.Before(periodStart) {
			c.Error(ierr.NewError("end_time must be after start_time").
				WithHint("Please provide a valid time range").
				Mark(ierr.ErrValidation))
			return
		}
	}

	// Add time range to context
	ctx := context.WithValue(c.Request.Context(), "start_time", periodStart)
	ctx = context.WithValue(ctx, "end_time", periodEnd)

	// Get cost breakdown
	resp, err := h.service.GetInputCostForMargin(ctx, &dto.CreateCostSheetRequest{})
	if err != nil {
		h.log.Error("Failed to get cost breakdown", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Calculate ROI for cost sheet
// @Description Calculate ROI (Return on Investment) for a given cost sheet
// @Tags CostSheets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.CalculateROIRequest true "ROI calculation request"
// @Success 200 {object} dto.ROIResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /cost/roi [post]
func (h *CostSheetHandler) CalculateROI(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.CalculateROIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind request body", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request body. Required: subscription_id, meter_id, price_id, period_start, period_end").
			Mark(ierr.ErrValidation))
		return
	}

	// Calculate ROI using the service
	response, err := h.service.CalculateROI(ctx, &req)
	if err != nil {
		h.log.Error("Failed to calculate ROI", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}
