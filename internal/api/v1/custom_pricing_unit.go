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

type CustomPricingUnitHandler struct {
	service *service.CustomPricingUnitService
	log     *logger.Logger
}

func NewCustomPricingUnitHandler(service *service.CustomPricingUnitService, log *logger.Logger) *CustomPricingUnitHandler {
	return &CustomPricingUnitHandler{
		service: service,
		log:     log,
	}
}

// CreateCustomPricingUnit handles the creation of a new custom pricing unit
// @Summary Create a new custom pricing unit
// @Description Create a new custom pricing unit with the provided details
// @Tags Custom Pricing Units
// @Accept json
// @Produce json
// @Param body body dto.CreateCustomPricingUnitRequest true "Custom pricing unit details"
// @Success 201 {object} dto.CustomPricingUnitResponse
// @Failure 400 {object} errors.Error
// @Router /v1/pricing_units [post]
func (h *CustomPricingUnitHandler) CreateCustomPricingUnit(c *gin.Context) {
	var req dto.CreateCustomPricingUnitRequest
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

	unit, err := h.service.Create(c.Request.Context(), &req)
	if err != nil {
		h.log.Error("Failed to create custom pricing unit", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, unit)
}

// GetCustomPricingUnits handles listing custom pricing units with pagination and filtering
// @Summary List custom pricing units
// @Description Get a paginated list of custom pricing units with optional filtering
// @Tags Custom Pricing Units
// @Accept json
// @Produce json
// @Param status query string false "Filter by status"
// @Success 200 {object} dto.ListCustomPricingUnitsResponse
// @Failure 400 {object} errors.Error
// @Router /v1/pricing_units [get]
func (h *CustomPricingUnitHandler) GetCustomPricingUnits(c *gin.Context) {
	filter := dto.CustomPricingUnitFilter{
		Status: types.Status(c.Query("status")),
	}

	response, err := h.service.List(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list custom pricing units", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// UpdateCustomPricingUnit handles updating an existing custom pricing unit
// @Summary Update a custom pricing unit
// @Description Update an existing custom pricing unit with the provided details
// @Tags Custom Pricing Units
// @Accept json
// @Produce json
// @Param id path string true "Custom pricing unit ID"
// @Param body body dto.UpdateCustomPricingUnitRequest true "Custom pricing unit details"
// @Success 200 {object} dto.CustomPricingUnitResponse
// @Failure 400 {object} errors.Error
// @Failure 404 {object} errors.Error
// @Router /v1/pricing_units/{id} [put]
func (h *CustomPricingUnitHandler) UpdateCustomPricingUnit(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithMessage("missing id parameter").
			WithHint("Custom pricing unit ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateCustomPricingUnitRequest
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

	unit, err := h.service.Update(c.Request.Context(), id, &req)
	if err != nil {
		h.log.Error("Failed to update custom pricing unit", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, unit)
}

// DeleteCustomPricingUnit handles deleting a custom pricing unit
// @Summary Delete a custom pricing unit
// @Description Delete an existing custom pricing unit
// @Tags Custom Pricing Units
// @Accept json
// @Produce json
// @Param id path string true "Custom pricing unit ID"
// @Success 200 {object} gin.H
// @Failure 400 {object} errors.Error
// @Failure 404 {object} errors.Error
// @Router /v1/pricing_units/{id} [delete]
func (h *CustomPricingUnitHandler) DeleteCustomPricingUnit(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithMessage("missing id parameter").
			WithHint("Custom pricing unit ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.service.Delete(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to delete custom pricing unit", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Custom pricing unit deleted successfully"})
}

// GetByID handles GET /v1/pricing-units/:id
func (h *CustomPricingUnitHandler) GetByID(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	unit, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		if ierr.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "custom pricing unit not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get custom pricing unit"})
		return
	}

	c.JSON(http.StatusOK, unit)
}

// GetByCode handles GET /v1/pricing-units/code/:code
func (h *CustomPricingUnitHandler) GetByCode(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code is required"})
		return
	}

	tenantID := types.GetTenantID(c.Request.Context())
	environmentID := types.GetEnvironmentID(c.Request.Context())

	unit, err := h.service.GetByCode(c.Request.Context(), code, tenantID, environmentID)
	if err != nil {
		if ierr.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "custom pricing unit not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get custom pricing unit"})
		return
	}

	c.JSON(http.StatusOK, unit)
}
