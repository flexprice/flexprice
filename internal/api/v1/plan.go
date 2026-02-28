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

// @Summary Create plan
// @ID createPlan
// @Description Use when defining a new pricing plan (e.g. Free, Pro, Enterprise). Attach prices and entitlements; customers subscribe to plans.
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param plan body dto.CreatePlanRequest true "Plan configuration"
// @Success 201 {object} dto.PlanResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

// @Summary Get plan
// @ID getPlan
// @Description Use when you need to load a single plan (e.g. for display or to create a subscription).
// @Tags Plans
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Success 200 {object} dto.PlanResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

func (h *PlanHandler) ListPlans(c *gin.Context) {
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

// @Summary Update plan
// @ID updatePlan
// @Description Use when changing plan details (e.g. name, interval, or metadata). Partial update supported.
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Param plan body dto.UpdatePlanRequest true "Plan update"
// @Success 200 {object} dto.PlanResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

// @Summary Delete plan
// @ID deletePlan
// @Description Use when retiring a plan (e.g. end-of-life). Existing subscriptions may be affected. Returns 200 with success message.
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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
// @Description Use when checking what a plan includes (e.g. feature list or limits for display or gating).
// @Tags Entitlements
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Success 200 {object} dto.PlanResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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
// @Description Use when listing credits attached to a plan (e.g. included prepaid or promo credits).
// @Tags Credit Grants
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Plan ID"
// @Success 200 {object} dto.ListCreditGrantsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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
// @Description Use when you have changed plan prices and need to push them to all active subscriptions (e.g. global price update). Returns workflow ID.
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

// @Summary Query plans
// @ID queryPlan
// @Description Use when listing or searching plans (e.g. plan picker or admin catalog). Returns a paginated list; supports filtering and sorting.
// @Tags Plans
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.PlanFilter true "Filter"
// @Success 200 {object} dto.ListPlansResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /plans/search [post]
func (h *PlanHandler) QueryPlans(c *gin.Context) {
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
