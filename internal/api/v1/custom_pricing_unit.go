package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
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
// @Router /v1/cpu [post]
func (h *CustomPricingUnitHandler) CreateCustomPricingUnit(c *gin.Context) {
	var req dto.CreateCustomPricingUnitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("failed to bind request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	unit, err := h.service.Create(c.Request.Context(), &req)
	if err != nil {
		h.log.Error("failed to create custom pricing unit", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
// @Param status query string false "Filter by status (published/archived/deleted)"
// @Success 200 {object} dto.ListCustomPricingUnitsResponse
// @Failure 400 {object} errors.Error
// @Router /v1/cpu [get]
func (h *CustomPricingUnitHandler) GetCustomPricingUnits(c *gin.Context) {
	filter := dto.CustomPricingUnitFilter{
		Status: types.Status(c.Query("status")),
	}

	response, err := h.service.List(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("failed to list custom pricing units", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

// UpdateCustomPricingUnit handles updating a specific custom pricing unit
// @Summary Update a custom pricing unit
// @Description Update a custom pricing unit's details
// @Tags Custom Pricing Units
// @Accept json
// @Produce json
// @Param id path string true "Custom pricing unit ID"
// @Param body body dto.UpdateCustomPricingUnitRequest true "Updated custom pricing unit details"
// @Success 200 {object} dto.CustomPricingUnitResponse
// @Failure 400,404 {object} errors.Error
// @Router /v1/cpu/{id} [put]
func (h *CustomPricingUnitHandler) UpdateCustomPricingUnit(c *gin.Context) {
	id := c.Param("id")
	var req dto.UpdateCustomPricingUnitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("failed to bind request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	unit, err := h.service.Update(c.Request.Context(), id, &req)
	if err != nil {
		h.log.Error("failed to update custom pricing unit", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, unit)
}

// DeleteCustomPricingUnit handles deleting a custom pricing unit
// @Summary Delete a custom pricing unit
// @Description Delete a custom pricing unit by its ID
// @Tags Custom Pricing Units
// @Accept json
// @Produce json
// @Param id path string true "Custom pricing unit ID"
// @Success 204 "No Content"
// @Failure 404 {object} errors.Error
// @Router /v1/cpu/{id} [delete]
func (h *CustomPricingUnitHandler) DeleteCustomPricingUnit(c *gin.Context) {
	id := c.Param("id")
	err := h.service.Delete(c.Request.Context(), id)
	if err != nil {
		h.log.Error("failed to delete custom pricing unit", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}
