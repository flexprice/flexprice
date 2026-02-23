package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type PlanHandler struct {
	service            service.PlanService
	entitlementService service.EntitlementService
	creditGrantService service.CreditGrantService
	temporalService    temporalservice.TemporalService
	log                *logger.Logger
}

func NewPlanHandler(
	service service.PlanService,
	entitlementService service.EntitlementService,
	creditGrantService service.CreditGrantService,
	temporalService temporalservice.TemporalService,
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
// @ID createPlan
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
// @ID getPlan
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
// @ID getPlans
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
// @ID updatePlan
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
// @ID deletePlan
// @Description Delete a plan by ID
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Success 200 {object} dto.SuccessResponse
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
// @ID getPlanEntitlements
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
// @ID getPlanCreditGrants
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

func priceSyncLockKey(planID string) string {
	return cache.PrefixPriceSyncLock + planID
}

// @Summary Synchronize plan prices
// @ID syncPlanPrices
// @Description Synchronize current plan prices with all existing active subscriptions
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Success 200 {object} models.TemporalWorkflowResult
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
	// Verify that the plan exists
	_, err := h.service.GetPlan(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}
	// Acquire plan-level lock (Redis SetNX, 2h TTL)
	redisCache := cache.GetRedisCache()
	if redisCache == nil {
		c.Error(ierr.NewError("price sync lock unavailable").
			WithHint("Redis cache is not available. Try again later.").
			Mark(ierr.ErrServiceUnavailable))
		return
	}
	lockKey := priceSyncLockKey(id)
	acquired, err := redisCache.TrySetNX(c.Request.Context(), lockKey, "1", cache.ExpiryPriceSyncLock)
	if err != nil {
		h.log.Errorw("price_sync_lock_acquire_failed", "plan_id", id, "lock_key", lockKey, "error", err)
		c.Error(ierr.NewError("failed to acquire price sync lock").
			WithHint("Try again later.").
			Mark(ierr.ErrInternal))
		return
	}
	if !acquired {
		h.log.Infow("price_sync_lock_rejected", "plan_id", id, "lock_key", lockKey, "reason", "already_held")
		c.Error(ierr.NewError("price sync already in progress for this plan").
			WithHint("Try again later or wait up to 2 hours for the current sync to complete.").
			Mark(ierr.ErrAlreadyExists))
		return
	}
	h.log.Infow("price_sync_lock_acquired", "plan_id", id, "lock_key", lockKey)
	// Start the price sync workflow (activity will release lock when done)
	workflowRun, err := h.temporalService.ExecuteWorkflow(c.Request.Context(), types.TemporalPriceSyncWorkflow, id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, models.TemporalWorkflowResult{
		Message:    "price sync workflow started successfully",
		WorkflowID: workflowRun.GetID(),
		RunID:      workflowRun.GetRunID(),
	})
}

// @Summary List plans by filter
// @ID listPlansByFilter
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

// @Summary Clone a plan
// @Description Clone an existing plan, copying its active prices, published entitlements, and published credit grants
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Source Plan ID"
// @Param request body dto.ClonePlanRequest true "Clone configuration"
// @Success 201 {object} dto.PlanResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 409 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /plans/{id}/clone [post]
func (h *PlanHandler) ClonePlan(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.ClonePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.ClonePlan(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

func (h *PlanHandler) SyncPlanPricesV2(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation))
		return
	}
	// Acquire plan-level lock (Redis SetNX, 2h TTL)
	redisCache := cache.GetRedisCache()
	if redisCache == nil {
		c.Error(ierr.NewError("price sync lock unavailable").
			WithHint("Redis cache is not available. Try again later.").
			Mark(ierr.ErrServiceUnavailable))
		return
	}
	lockKey := priceSyncLockKey(id)
	acquired, err := redisCache.TrySetNX(c.Request.Context(), lockKey, "1", cache.ExpiryPriceSyncLock)
	if err != nil {
		h.log.Errorw("price_sync_lock_acquire_failed", "plan_id", id, "lock_key", lockKey, "error", err)
		c.Error(ierr.NewError("failed to acquire price sync lock").
			WithHint("Try again later.").
			Mark(ierr.ErrInternal))
		return
	}
	if !acquired {
		h.log.Infow("price_sync_lock_rejected", "plan_id", id, "lock_key", lockKey, "reason", "already_held")
		c.Error(ierr.NewError("price sync already in progress for this plan").
			WithHint("Try again later or wait up to 2 hours for the current sync to complete.").
			Mark(ierr.ErrAlreadyExists))
		return
	}
	h.log.Infow("price_sync_lock_acquired", "plan_id", id, "lock_key", lockKey)
	defer redisCache.Delete(c.Request.Context(), lockKey)

	resp, err := h.service.SyncPlanPrices(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
