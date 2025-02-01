package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type PlanHandler struct {
	service service.PlanService
	log     *logger.Logger
}

func NewPlanHandler(service service.PlanService, log *logger.Logger) *PlanHandler {
	return &PlanHandler{service: service, log: log}
}

// @Summary Create a new plan
// @Description Create a new plan with the specified configuration
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param plan body dto.CreatePlanRequest true "Plan configuration"
// @Success 201 {object} dto.PlanResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /plans [post]
func (h *PlanHandler) CreatePlan(c *gin.Context) {
	var req dto.CreatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.service.CreatePlan(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get a plan by ID
// @Description Get a plan by ID
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Success 200 {object} dto.PlanResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /plans/{id} [get]
func (h *PlanHandler) GetPlan(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.service.GetPlan(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get plans
// @Description Get plans with the specified filter
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.PlanFilter false "Filter"
// @Success 200 {object} dto.ListPlansResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /plans [get]
func (h *PlanHandler) GetPlans(c *gin.Context) {
	var filter types.PlanFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	resp, err := h.service.GetPlans(c.Request.Context(), &filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update a plan by ID
// @Description Update a plan by ID
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Param plan body dto.UpdatePlanRequest true "Plan configuration"
// @Success 200 {object} dto.PlanResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /plans/{id} [put]
func (h *PlanHandler) UpdatePlan(c *gin.Context) {
	id := c.Param("id")

	var req dto.UpdatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.service.UpdatePlan(c.Request.Context(), id, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete a plan by ID
// @Description Delete a plan by ID
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Success 200 {object} gin.H
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /plans/{id} [delete]
func (h *PlanHandler) DeletePlan(c *gin.Context) {
	id := c.Param("id")

	err := h.service.DeletePlan(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "price deleted successfully"})
}
