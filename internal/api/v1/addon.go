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

type AddonHandler struct {
	addonService service.AddonService
	log          *logger.Logger
}

func NewAddonHandler(addonService service.AddonService, log *logger.Logger) *AddonHandler {
	return &AddonHandler{
		addonService: addonService,
		log:          log,
	}
}

// CreateAddon godoc
// @Summary Create a new addon
// @Description Create a new addon with optional prices and entitlements
// @Tags Addons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param addon body dto.CreateAddonRequest true "Addon to create"
// @Success 201 {object} dto.CreateAddonResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /addons [post]
func (h *AddonHandler) CreateAddon(c *gin.Context) {
	var req dto.CreateAddonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(err)
		return
	}

	addon, err := h.addonService.CreateAddon(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, addon)
}

// GetAddon godoc
// @Summary Get an addon by ID
// @Description Get an addon by ID
// @Tags Addons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Success 200 {object} dto.AddonResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /addons/{id} [get]
func (h *AddonHandler) GetAddon(c *gin.Context) {
	id := c.Param("id")
	addon, err := h.addonService.GetAddon(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, addon)
}

// GetAddonByLookupKey godoc
// @Summary Get an addon by lookup key
// @Description Get an addon by lookup key
// @Tags Addons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param lookup_key path string true "Addon Lookup Key"
// @Success 200 {object} dto.AddonResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /addons/lookup/{lookup_key} [get]
func (h *AddonHandler) GetAddonByLookupKey(c *gin.Context) {
	lookupKey := c.Param("lookup_key")

	addon, err := h.addonService.GetAddonByLookupKey(c.Request.Context(), lookupKey)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, addon)
}

// ListAddons godoc
// @Summary List addons
// @Description List addons with optional filtering
// @Tags Addons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.AddonFilter true "Filter"
// @Success 200 {object} dto.ListAddonsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /addons [get]
func (h *AddonHandler) ListAddons(c *gin.Context) {
	var filter types.AddonFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.addonService.GetAddons(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateAddon godoc
// @Summary Update an addon
// @Description Update an addon by ID
// @Tags Addons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Param addon body dto.UpdateAddonRequest true "Addon update data"
// @Success 200 {object} dto.AddonResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /addons/{id} [put]
func (h *AddonHandler) UpdateAddon(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("addon ID is required").
			WithHint("Addon ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateAddonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	addon, err := h.addonService.UpdateAddon(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, addon)
}

// DeleteAddon godoc
// @Summary Delete an addon
// @Description Delete an addon by ID
// @Tags Addons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Success 200 {object} gin.H
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /addons/{id} [delete]
func (h *AddonHandler) DeleteAddon(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("addon ID is required").
			WithHint("Addon ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	if err := h.addonService.DeleteAddon(c.Request.Context(), id); err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "addon deleted successfully"})
}

// ListAddonsByFilter godoc
// @Summary List addons by filter
// @Description List addons by filter using POST method
// @Tags Addons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.AddonFilter true "Filter"
// @Success 200 {object} dto.ListAddonsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /addons/search [post]
func (h *AddonHandler) ListAddonsByFilter(c *gin.Context) {
	var filter types.AddonFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.addonService.GetAddons(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// AddAddonToSubscription godoc
// @Summary Add an addon to a subscription
// @Description Add an addon to a subscription
// @Tags Addons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param subscription_id path string true "Subscription ID"
// @Param request body dto.AddAddonToSubscriptionRequest true "Add addon request"
// @Success 201 {object} dto.SubscriptionAddonResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /addons/subscriptions/{subscription_id} [post]
func (h *AddonHandler) AddAddonToSubscription(c *gin.Context) {
	subscriptionID := c.Param("subscription_id")
	if subscriptionID == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.AddAddonToSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	subscriptionAddon, err := h.addonService.AddAddonToSubscription(c.Request.Context(), subscriptionID, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, subscriptionAddon)
}
