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
	service            service.AddonService
	entitlementService service.EntitlementService
	log                *logger.Logger
}

func NewAddonHandler(service service.AddonService, entitlementService service.EntitlementService, log *logger.Logger) *AddonHandler {
	return &AddonHandler{
		service:            service,
		entitlementService: entitlementService,
		log:                log,
	}
}

// @Summary Create addon
// @Description Create a new addon
// @Tags Addons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param addon body dto.CreateAddonRequest true "Addon Request"
// @Success 201 {object} dto.CreateAddonResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
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
// @Description Get an addon by ID
// @Tags Addons
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Success 200 {object} dto.AddonResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
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
// @Description Get an addon by lookup key
// @Tags Addons
// @Produce json
// @Security ApiKeyAuth
// @Param lookup_key path string true "Addon Lookup Key"
// @Success 200 {object} dto.AddonResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
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

// @Summary List addons
// @Description Get addons with optional filtering
// @Tags Addons
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.AddonFilter false "Filter"
// @Success 200 {object} dto.ListAddonsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /addons [get]
func (h *AddonHandler) GetAddons(c *gin.Context) {
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
// @Description Update an existing addon
// @Tags Addons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Param addon body dto.UpdateAddonRequest true "Update Addon Request"
// @Success 200 {object} dto.AddonResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
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
// @Description Delete an addon
// @Tags Addons
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
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

// @Summary List addons by filter
// @Description List addons by filter
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
// @Description Get all entitlements for an addon
// @Tags Entitlements
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Success 200 {object} dto.ListEntitlementsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
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
