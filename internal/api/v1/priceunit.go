package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type PriceUnitHandler struct {
	service service.PriceUnitService
	log     *logger.Logger
}

func NewPriceUnitHandler(service service.PriceUnitService, log *logger.Logger) *PriceUnitHandler {
	return &PriceUnitHandler{
		service: service,
		log:     log,
	}
}

// CreatePriceUnit handles the creation of a new price unit
// @Summary Create a new price unit
// @Description Create a new price unit with the provided details
// @Tags Price Units
// @Accept json
// @Security ApiKeyAuth
// @Produce json
// @Param body body dto.CreatePriceUnitRequest true "Price unit details"
// @Success 201 {object} dto.CreatePriceUnitResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Router /prices/units [post]
func (h *PriceUnitHandler) CreatePriceUnit(c *gin.Context) {
	var req dto.CreatePriceUnitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.NewError("invalid request body").
			WithMessage("failed to parse request").
			WithHint("The request body is invalid").
			WithReportableDetails(map[string]interface{}{
				"parsing_error": err.Error(),
			}).
			Mark(ierr.ErrValidation))
		return
	}

	unit, err := h.service.CreatePriceUnit(c.Request.Context(), req)
	if err != nil {
		h.log.Error("Failed to create price unit", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, unit)
}

// GetPriceUnit handles getting a price unit by ID
// @Summary Get a price unit by ID
// @Description Get a price unit by ID
// @Tags Price Units
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Price unit ID"
// @Success 200 {object} dto.PriceUnitResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Router /prices/units/{id} [get]
func (h *PriceUnitHandler) GetPriceUnit(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithMessage("missing id parameter").
			WithHint("Price unit ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	unit, err := h.service.GetPriceUnit(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to get price unit", "error", err, "id", id)

		if ierr.IsNotFound(err) {
			c.Error(ierr.NewError("price unit not found").
				WithMessage("price unit not found").
				WithHint("The specified price unit ID does not exist").
				WithReportableDetails(map[string]interface{}{
					"id": id,
				}).
				Mark(ierr.ErrNotFound))
			return
		}

		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, unit)
}

// GetPriceUnitByCode handles getting a price unit by code
// @Summary Get a price unit by code
// @Description Get a price unit by code
// @Tags Price Units
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param code path string true "Price unit code"
// @Success 200 {object} dto.PriceUnitResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Router /prices/units/code/{code} [get]
func (h *PriceUnitHandler) GetPriceUnitByCode(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.Error(ierr.NewError("code is required").
			WithMessage("missing code parameter").
			WithHint("Price unit code is required").
			Mark(ierr.ErrValidation))
		return
	}

	unit, err := h.service.GetPriceUnitByCode(c.Request.Context(), code)
	if err != nil {
		h.log.Error("Failed to get price unit by code", "error", err, "code", code)

		if ierr.IsNotFound(err) {
			c.Error(ierr.NewError("price unit not found").
				WithMessage("price unit not found").
				WithHint("The specified price unit code does not exist").
				WithReportableDetails(map[string]interface{}{
					"code": code,
				}).
				Mark(ierr.ErrNotFound))
			return
		}

		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, unit)
}

// ListPriceUnits handles listing price units with pagination and filtering
// @Summary List price units
// @Description Get a paginated list of price units with optional filtering
// @Tags Price Units
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param status query string false "Filter by status"
// @Param limit query int false "Limit number of results"
// @Param offset query int false "Offset for pagination"
// @Param sort query string false "Sort field"
// @Param order query string false "Sort order (asc/desc)"
// @Success 200 {object} dto.ListPriceUnitsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Router /prices/units [get]
func (h *PriceUnitHandler) ListPriceUnits(c *gin.Context) {
	var filter types.PriceUnitFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	// Debug logging to verify pagination
	h.log.Info("PriceUnit filter applied",
		"limit", filter.GetLimit(),
		"offset", filter.GetOffset(),
		"sort", filter.GetSort(),
		"order", filter.GetOrder(),
		"status", filter.GetStatus())

	response, err := h.service.ListPriceUnits(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list price units", "error", err)
		c.Error(err)
		return
	}

	// Debug logging for response
	h.log.Info("PriceUnit response",
		"items_count", len(response.Items),
		"total", response.Pagination.Total,
		"limit", response.Pagination.Limit,
		"offset", response.Pagination.Offset)

	c.JSON(http.StatusOK, response)
}

// UpdatePriceUnit handles updating an existing price unit
// @Summary Update a price unit
// @Description Update an existing price unit with the provided details. Only name and metadata can be updated.
// @Tags Price Units
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Price unit ID"
// @Param body body dto.UpdatePriceUnitRequest true "Price unit details to update"
// @Success 200 {object} dto.PriceUnitResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Router /prices/units/{id} [put]
func (h *PriceUnitHandler) UpdatePriceUnit(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithMessage("missing id parameter").
			WithHint("Price unit ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdatePriceUnitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.NewError("invalid request body").
			WithMessage("failed to parse request").
			WithHint("The request body is invalid").
			WithReportableDetails(map[string]interface{}{
				"parsing_error": err.Error(),
			}).
			Mark(ierr.ErrValidation))
		return
	}

	unit, err := h.service.UpdatePriceUnit(c.Request.Context(), id, req)
	if err != nil {
		h.log.Error("Failed to update price unit", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, unit)
}

// DeletePriceUnit handles deleting a price unit
// @Summary Delete a price unit
// @Description Delete an existing price unit. The unit will be marked as deleted and cannot be used in new prices.
// @Tags Price Units
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Price unit ID"
// @Success 200 {object} gin.H
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Router /prices/units/{id} [delete]
func (h *PriceUnitHandler) DeletePriceUnit(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithMessage("missing id parameter").
			WithHint("Price unit ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.service.DeletePriceUnit(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to delete price unit", "error", err, "id", id)

		// Handle specific error types
		if ierr.IsNotFound(err) {
			c.Error(ierr.NewError("price unit not found").
				WithMessage("price unit not found").
				WithHint("The specified price unit ID does not exist").
				WithReportableDetails(map[string]interface{}{
					"id": id,
				}).
				Mark(ierr.ErrNotFound))
			return
		}

		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Price unit deleted successfully"})
}
