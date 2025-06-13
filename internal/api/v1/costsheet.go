package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCostsheet "github.com/flexprice/flexprice/internal/domain/costsheet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type CostSheetHandler struct {
	service service.CostSheetService
	log     *logger.Logger
}

func NewCostSheetHandler(service service.CostSheetService, log *logger.Logger) *CostSheetHandler {
	return &CostSheetHandler{service: service, log: log}
}

// @Summary Create a new cost sheet
// @Description Create a new cost sheet with the specified configuration
// @Tags CostSheets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param costsheet body dto.CreateCostsheetRequest true "Cost sheet configuration"
// @Success 201 {object} dto.CostsheetResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /cost_sheets [post]
func (h *CostSheetHandler) CreateCostsheet(c *gin.Context) {
	var req dto.CreateCostsheetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateCostsheet(c.Request.Context(), &req)
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
// @Success 200 {object} dto.CostsheetResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /cost_sheets/{id} [get]
func (h *CostSheetHandler) GetCostsheet(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Cost Sheet ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetCostsheet(c.Request.Context(), id)
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
// @Success 200 {object} dto.ListCostsheetsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /cost_sheets [get]
func (h *CostSheetHandler) ListCostsheets(c *gin.Context) {
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

	resp, err := h.service.ListCostsheets(c.Request.Context(), &filter)
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
// @Param costsheet body dto.UpdateCostsheetRequest true "Cost sheet configuration"
// @Success 200 {object} dto.CostsheetResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /cost_sheets/{id} [put]
func (h *CostSheetHandler) UpdateCostsheet(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Cost Sheet ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateCostsheetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	req.ID = id // Set the ID from path parameter

	resp, err := h.service.UpdateCostsheet(c.Request.Context(), &req)
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
// @Failure 500 {object} ierr.ErrorResponse
// @Router /cost_sheets/{id} [delete]
func (h *CostSheetHandler) DeleteCostsheet(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Cost Sheet ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	if err := h.service.DeleteCostsheet(c.Request.Context(), id); err != nil {
		h.log.Error("Failed to delete cost sheet", "error", err)
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary Calculate cost sheet
// @Description Calculate cost sheet for given meter and price
// @Tags CostSheets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.CreateCostsheetRequest true "Calculate request"
// @Success 200 {object} dto.CostBreakdownResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /cost_sheets/calculate [post]
func (h *CostSheetHandler) CalculateCostSheet(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.CreateCostsheetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	costbreakdown, err := h.service.GetInputCostForMargin(ctx, &req)
	if err != nil {
		h.log.Error("Failed to calculate cost sheet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, costbreakdown)
}
