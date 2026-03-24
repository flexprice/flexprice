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
	"github.com/samber/lo"
)

// CostsheetHandler handles HTTP requests for costsheet operations.
type CostsheetHandler struct {
	service service.CostsheetService
	log     *logger.Logger
}

// NewCostsheetHandler creates a new instance of CostsheetHandler.
func NewCostsheetHandler(service service.CostsheetService, log *logger.Logger) *CostsheetHandler {
	return &CostsheetHandler{
		service: service,
		log:     log,
	}
}

// @Summary Create costsheet
// @ID createCostsheet
// @Description Use when setting up a new pricing configuration (e.g. a new product or region). Costsheets group prices and define the default for the environment.
// @Tags Costs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param costsheet body dto.CreateCostsheetRequest true "Costsheet configuration"
// @Success 201 {object} dto.CreateCostsheetResponse "Created costsheet"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 409 {object} ierr.ErrorResponse "Conflict"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /costs [post]
func (h *CostsheetHandler) CreateCostsheet(c *gin.Context) {
	var req dto.CreateCostsheetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateCostsheet(c.Request.Context(), req)
	if err != nil {
		h.log.Error("Failed to create costsheet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get costsheet
// @ID getCostsheet
// @Description Use when you need to load a single costsheet (e.g. for editing or display). Supports optional expand for related prices.
// @Tags Costs
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Costsheet ID"
// @Param expand query string false "Comma-separated list of fields to expand (e.g., 'prices')"
// @Success 200 {object} dto.GetCostsheetResponse "Costsheet details"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Costsheet not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /costs/{id} [get]
func (h *CostsheetHandler) GetCostsheet(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Costsheet ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetCostsheet(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to get costsheet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update costsheet
// @ID updateCostsheet
// @Description Use when changing costsheet name or metadata.
// @Tags Costs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Costsheet ID"
// @Param costsheet body dto.UpdateCostsheetRequest true "Costsheet configuration"
// @Success 200 {object} dto.UpdateCostsheetResponse "Updated costsheet"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Costsheet not found"
// @Failure 409 {object} ierr.ErrorResponse "Conflict"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /costs/{id} [put]
func (h *CostsheetHandler) UpdateCostsheet(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Costsheet ID is required").
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

	resp, err := h.service.UpdateCostsheet(c.Request.Context(), id, req)
	if err != nil {
		h.log.Error("Failed to update costsheet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete costsheet
// @ID deleteCostsheet
// @Description Use when retiring a costsheet (e.g. end-of-life product). Soft-deletes; status set to deleted.
// @Tags Costs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Costsheet ID"
// @Success 200 {object} dto.DeleteCostsheetResponse "Costsheet deleted"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Costsheet not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /costs/{id} [delete]
func (h *CostsheetHandler) DeleteCostsheet(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Costsheet ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.DeleteCostsheet(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to delete costsheet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Query costsheets
// @ID queryCostsheet
// @Description Use when listing or searching costsheets (e.g. admin catalog). Returns a paginated list; supports filtering and sorting.
// @Tags Costs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body domainCostsheet.Filter true "Filter"
// @Success 200 {object} dto.ListCostsheetResponse "Paginated costsheets"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /costs/search [post]
func (h *CostsheetHandler) QueryCostsheets(c *gin.Context) {
	var filter domainCostsheet.Filter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	// Initialize QueryFilter if not set and set default limit if not provided
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	// Set default limit if not provided
	if filter.GetLimit() == 0 {
		filter.QueryFilter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	resp, err := h.service.GetCostsheets(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get active costsheet
// @ID getActiveCostsheet
// @Description Use when you need the tenant's default pricing configuration (e.g. for checkout or plan display). Returns the active costsheet for the environment.
// @Tags Costs
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.CostsheetResponse "Active costsheet"
// @Failure 404 {object} ierr.ErrorResponse "No active costsheet"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /costs/active [get]
func (h *CostsheetHandler) GetActiveCostsheetForTenant(c *gin.Context) {
	resp, err := h.service.GetActiveCostsheetForTenant(c.Request.Context())
	if err != nil {
		h.log.Error("Failed to get active costsheet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
