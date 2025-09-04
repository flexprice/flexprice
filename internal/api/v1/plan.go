package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type PlanHandler struct {
	service            service.PlanService
	entitlementService service.EntitlementService
	creditGrantService service.CreditGrantService
	temporalService    *temporal.Service
	log                *logger.Logger
}

func NewPlanHandler(
	service service.PlanService,
	entitlementService service.EntitlementService,
	creditGrantService service.CreditGrantService,
	temporalService *temporal.Service,
	log *logger.Logger,
) *PlanHandler {
	return &PlanHandler{
		service:            service,
		entitlementService: entitlementService,
		creditGrantService: creditGrantService,
		temporalService:    temporalService,
		log:                log,
	}
}

// @Summary Create a new plan
// @Description Create a new plan with the specified configuration
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param plan body dto.CreatePlanRequest true "Plan configuration"
// @Success 201 {object} dto.PlanResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /plans [post]
func (h *PlanHandler) CreatePlan(c *gin.Context) {
	var req dto.CreatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreatePlan(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get a plan
// @Description Get a plan by ID
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Success 200 {object} dto.PlanResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /plans/{id} [get]
func (h *PlanHandler) GetPlan(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetPlan(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get plans
// @Description Get plans with optional filtering
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.PlanFilter false "Filter"
// @Success 200 {object} dto.ListPlansResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /plans [get]
func (h *PlanHandler) GetPlans(c *gin.Context) {
	var filter types.PlanFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	resp, err := h.service.GetPlans(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update a plan
// @Description Update a plan by ID
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Param plan body dto.UpdatePlanRequest true "Plan update"
// @Success 200 {object} dto.PlanResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /plans/{id} [put]
func (h *PlanHandler) UpdatePlan(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdatePlan(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete a plan
// @Description Delete a plan by ID
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Success 200 {object} gin.H
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /plans/{id} [delete]
func (h *PlanHandler) DeletePlan(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.service.DeletePlan(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "price deleted successfully"})
}

// @Summary Get plan entitlements
// @Description Get all entitlements for a plan
// @Tags Entitlements
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Success 200 {object} dto.PlanResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /plans/{id}/entitlements [get]
func (h *PlanHandler) GetPlanEntitlements(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.entitlementService.GetPlanEntitlements(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get plan credit grants
// @Description Get all credit grants for a plan
// @Tags CreditGrants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Success 200 {object} dto.ListCreditGrantsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /plans/{id}/creditgrants [get]
func (h *PlanHandler) GetPlanCreditGrants(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.creditGrantService.GetCreditGrantsByPlan(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Synchronize plan prices
// @Description Synchronize current plan prices with all existing active subscriptions
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Success 200 {object} dto.SyncPlanPricesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 422 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /plans/{id}/sync/subscriptions [post]
func (h *PlanHandler) SyncPlanPrices(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	// Use temporal workflow instead of direct service call
	result, err := h.temporalService.StartPlanPriceSync(c.Request.Context(), id)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Failed to sync plan prices").
			Mark(ierr.ErrInternal))
		return
	}

	// Call the service directly without Temporal (commented out)
	// result, err := h.service.SyncPlanPrices(c.Request.Context(), id)
	// if err != nil {
	// 	c.Error(ierr.WithError(err).
	// 		WithHint("Failed to sync plan prices").
	// 		Mark(ierr.ErrInternal))
	// 	return
	// }

	c.JSON(http.StatusOK, result)
}

// @Summary List plans by filter
// @Description List plans by filter
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.PlanFilter true "Filter"
// @Success 200 {object} dto.ListPlansResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /plans/search [post]
func (h *PlanHandler) ListPlansByFilter(c *gin.Context) {
	var filter types.PlanFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}
	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}
	resp, err := h.service.GetPlans(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
