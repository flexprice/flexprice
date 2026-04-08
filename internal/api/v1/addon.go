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
)

type AddonHandler struct {
	service            service.AddonService
	entitlementService service.EntitlementService
	temporalService    temporalservice.TemporalService
	log                *logger.Logger
}

func NewAddonHandler(
	service service.AddonService,
	entitlementService service.EntitlementService,
	temporalService temporalservice.TemporalService,
	log *logger.Logger,
) *AddonHandler {
	return &AddonHandler{
		service:            service,
		entitlementService: entitlementService,
		temporalService:    temporalService,
		log:                log,
	}
}

// @Summary Create addon
// @ID createAddon
// @Description Use when defining an optional purchasable item (e.g. extra storage or support tier). Ideal for add-ons that customers can attach to a subscription.
// @Tags Addons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param addon body dto.CreateAddonRequest true "Addon Request"
// @Success 201 {object} dto.CreateAddonResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /addons [post]
func (h *AddonHandler) CreateAddon(c *gin.Context) {
	var req dto.CreateAddonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateAddon(c.Request.Context(), req)
	if err != nil {
		h.log.Error("Failed to create addon", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get addon
// @ID getAddon
// @Description Use when you need to load a single addon (e.g. for display or to attach to a subscription).
// @Tags Addons
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Success 200 {object} dto.AddonResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /addons/{id} [get]
func (h *AddonHandler) GetAddon(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("addon ID is required").
			WithHint("Please provide a valid addon ID").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetAddon(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to get addon", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get addon by lookup key
// @ID getAddonByLookupKey
// @Description Use when resolving an addon by external id (e.g. from your product catalog). Ideal for integrations.
// @Tags Addons
// @Produce json
// @Security ApiKeyAuth
// @Param lookup_key path string true "Addon Lookup Key"
// @Success 200 {object} dto.AddonResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /addons/lookup/{lookup_key} [get]
func (h *AddonHandler) GetAddonByLookupKey(c *gin.Context) {
	lookupKey := c.Param("lookup_key")
	if lookupKey == "" {
		c.Error(ierr.NewError("lookup key is required").
			WithHint("Please provide a valid lookup key").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetAddonByLookupKey(c.Request.Context(), lookupKey)
	if err != nil {
		h.log.Error("Failed to get addon by lookup key", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *AddonHandler) ListAddons(c *gin.Context) {
	var filter types.AddonFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		h.log.Error("Failed to bind query", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetAddons(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list addons", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update addon
// @ID updateAddon
// @Description Use when changing addon details (e.g. name, pricing, or metadata).
// @Tags Addons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Param addon body dto.UpdateAddonRequest true "Update Addon Request"
// @Success 200 {object} dto.AddonResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /addons/{id} [put]
func (h *AddonHandler) UpdateAddon(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("addon ID is required").
			WithHint("Please provide a valid addon ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateAddonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateAddon(c.Request.Context(), id, req)
	if err != nil {
		h.log.Error("Failed to update addon", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete addon
// @ID deleteAddon
// @Description Use when retiring an addon (e.g. end-of-life). Returns 200 with success message.
// @Tags Addons
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /addons/{id} [delete]
func (h *AddonHandler) DeleteAddon(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("addon ID is required").
			WithHint("Please provide a valid addon ID").
			Mark(ierr.ErrValidation))
		return
	}

	if err := h.service.DeleteAddon(c.Request.Context(), id); err != nil {
		h.log.Error("Failed to delete addon", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "addon deleted successfully"})
}

// @Summary Query addons
// @ID queryAddon
// @Description Use when listing or searching addons (e.g. catalog or subscription builder). Returns a paginated list; supports filtering and sorting.
// @Tags Addons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.AddonFilter true "Filter"
// @Success 200 {object} dto.ListAddonsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /addons/search [post]
func (h *AddonHandler) QueryAddons(c *gin.Context) {
	var filter types.AddonFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if err := filter.Validate(); err != nil {
		h.log.Error("Invalid filter parameters", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Please provide valid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetAddons(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list addons", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get addon entitlements
// @ID getAddonEntitlements
// @Description Use when checking what features or limits an addon grants (e.g. for display or entitlement logic).
// @Tags Entitlements
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Success 200 {object} dto.ListEntitlementsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /addons/{id}/entitlements [get]
func (h *AddonHandler) GetAddonEntitlements(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("addon ID is required").
			WithHint("Addon ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.entitlementService.GetAddonEntitlements(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to get addon entitlements", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func addonPriceSyncLockKey(addonID string) string {
	return cache.PrefixAddonPriceSyncLock + addonID
}

// @Summary Sync addon prices to subscriptions (async)
// @ID syncAddonPrices
// @Description Starts a Temporal workflow to sync addon prices to subscription line items for all active subscriptions with this addon.
// @Tags Addons
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 409 {object} ierr.ErrorResponse "Sync already in progress"
// @Failure 500 {object} ierr.ErrorResponse
// @Router /addons/{id}/sync/subscriptions [post]
func (h *AddonHandler) SyncAddonPrices(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("addon ID is required").
			WithHint("Addon ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	// Verify addon exists
	if _, err := h.service.GetAddon(c.Request.Context(), id); err != nil {
		c.Error(err)
		return
	}

	// Acquire addon-level lock
	redisCache := cache.GetRedisCache()
	if redisCache == nil {
		c.Error(ierr.NewError("price sync lock unavailable").
			WithHint("Redis cache is not available. Try again later.").
			Mark(ierr.ErrServiceUnavailable))
		return
	}
	lockKey := addonPriceSyncLockKey(id)
	acquired, err := redisCache.TrySetNX(c.Request.Context(), lockKey, "1", cache.ExpiryPriceSyncLock)
	if err != nil {
		h.log.Errorw("addon_price_sync_lock_acquire_failed", "addon_id", id, "lock_key", lockKey, "error", err)
		c.Error(ierr.NewError("failed to acquire addon price sync lock").
			WithHint("Try again later.").
			Mark(ierr.ErrInternal))
		return
	}
	if !acquired {
		h.log.Infow("addon_price_sync_lock_rejected", "addon_id", id, "lock_key", lockKey, "reason", "already_held")
		c.Error(ierr.NewError("price sync already in progress for this addon").
			WithHint("Try again later or wait up to 2 hours for the current sync to complete.").
			Mark(ierr.ErrAlreadyExists))
		return
	}
	h.log.Infow("addon_price_sync_lock_acquired", "addon_id", id, "lock_key", lockKey)
	// Release lock if workflow dispatch fails (activity releases it on success path).
	defer func() {
		if c.IsAborted() || len(c.Errors) > 0 {
			redisCache.Delete(c.Request.Context(), lockKey)
		}
	}()

	workflowInput := models.PriceSyncWorkflowInput{
		EntityType: types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:   id,
	}
	workflowRun, err := h.temporalService.ExecuteWorkflow(c.Request.Context(), types.TemporalPriceSyncWorkflow, workflowInput)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "addon price sync workflow started successfully",
		"workflow_id": workflowRun.GetID(),
		"run_id":      workflowRun.GetRunID(),
	})
}

// @Summary Sync addon prices to subscriptions (synchronous)
// @ID syncAddonPricesV2
// @Description Synchronously syncs addon prices to subscription line items. Blocks until complete.
// @Tags Addons
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Success 200 {object} dto.SyncAddonPricesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 409 {object} ierr.ErrorResponse "Sync already in progress"
// @Failure 500 {object} ierr.ErrorResponse
// @Router /addons/{id}/sync/subscriptions/v2 [post]
func (h *AddonHandler) SyncAddonPricesV2(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("addon ID is required").
			WithHint("Addon ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	redisCache := cache.GetRedisCache()
	if redisCache == nil {
		c.Error(ierr.NewError("price sync lock unavailable").
			WithHint("Redis cache is not available. Try again later.").
			Mark(ierr.ErrServiceUnavailable))
		return
	}
	lockKey := addonPriceSyncLockKey(id)
	acquired, err := redisCache.TrySetNX(c.Request.Context(), lockKey, "1", cache.ExpiryPriceSyncLock)
	if err != nil {
		h.log.Errorw("addon_price_sync_lock_acquire_failed", "addon_id", id, "lock_key", lockKey, "error", err)
		c.Error(ierr.NewError("failed to acquire addon price sync lock").
			WithHint("Try again later.").
			Mark(ierr.ErrInternal))
		return
	}
	if !acquired {
		h.log.Infow("addon_price_sync_lock_rejected", "addon_id", id, "lock_key", lockKey, "reason", "already_held")
		c.Error(ierr.NewError("price sync already in progress for this addon").
			WithHint("Try again later or wait up to 2 hours for the current sync to complete.").
			Mark(ierr.ErrAlreadyExists))
		return
	}
	h.log.Infow("addon_price_sync_lock_acquired", "addon_id", id, "lock_key", lockKey)
	defer redisCache.Delete(c.Request.Context(), lockKey)

	resp, err := h.service.SyncAddonPrices(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
