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

type EntitlementHandler struct {
	service service.EntitlementService
	log     *logger.Logger
}

func NewEntitlementHandler(service service.EntitlementService, log *logger.Logger) *EntitlementHandler {
	return &EntitlementHandler{service: service, log: log}
}

// @Summary Create entitlement
// @ID createEntitlement
// @Description Use when attaching a feature (and its limit) to a plan or addon (e.g. "10 seats" or "1000 API calls"). Defines what the plan/addon includes.
// @Tags Entitlements
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param entitlement body dto.CreateEntitlementRequest true "Entitlement configuration"
// @Success 201 {object} dto.EntitlementResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /entitlements [post]
func (h *EntitlementHandler) CreateEntitlement(c *gin.Context) {
	var req dto.CreateEntitlementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateEntitlement(c.Request.Context(), req)
	if err != nil {
		h.log.Error("Failed to create entitlement", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Create entitlements in bulk
// @ID createEntitlementsBulk
// @Description Use when attaching many features to a plan or addon at once (e.g. initial plan setup or import). Bulk version of create entitlement.
// @Tags Entitlements
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param entitlements body dto.CreateBulkEntitlementRequest true "Bulk entitlement configuration"
// @Success 201 {object} dto.CreateBulkEntitlementResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /entitlements/bulk [post]
func (h *EntitlementHandler) CreateBulkEntitlement(c *gin.Context) {
	var req dto.CreateBulkEntitlementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateBulkEntitlement(c.Request.Context(), req)
	if err != nil {
		h.log.Error("Failed to create bulk entitlements", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get entitlement
// @ID getEntitlement
// @Description Use when you need to load a single entitlement (e.g. to display or edit a feature limit).
// @Tags Entitlements
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Entitlement ID"
// @Success 200 {object} dto.EntitlementResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /entitlements/{id} [get]
func (h *EntitlementHandler) GetEntitlement(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Entitlement ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetEntitlement(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to get entitlement", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *EntitlementHandler) ListEntitlements(c *gin.Context) {
	var filter types.EntitlementFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		h.log.Error("Failed to bind query", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	// Set default filter if not provided
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	resp, err := h.service.ListEntitlements(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list entitlements", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update entitlement
// @ID updateEntitlement
// @Description Use when changing an entitlement (e.g. increasing or decreasing a limit). Request body contains the fields to update.
// @Tags Entitlements
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Entitlement ID"
// @Param entitlement body dto.UpdateEntitlementRequest true "Entitlement configuration"
// @Success 200 {object} dto.EntitlementResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /entitlements/{id} [put]
func (h *EntitlementHandler) UpdateEntitlement(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Entitlement ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateEntitlementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateEntitlement(c.Request.Context(), id, req)
	if err != nil {
		h.log.Error("Failed to update entitlement", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete entitlement
// @ID deleteEntitlement
// @Description Use when removing a feature from a plan or addon (e.g. deprecating a capability). Returns 200 with success message.
// @Tags Entitlements
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Entitlement ID"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /entitlements/{id} [delete]
func (h *EntitlementHandler) DeleteEntitlement(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Entitlement ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	if err := h.service.DeleteEntitlement(c.Request.Context(), id); err != nil {
		h.log.Error("Failed to delete entitlement", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "entitlement deleted successfully"})
}

// @Summary Query entitlements
// @ID queryEntitlement
// @Description Use when listing or searching entitlements (e.g. plan editor or audit). Returns a paginated list; supports filtering by plan, addon, feature.
// @Tags Entitlements
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.EntitlementFilter true "Filter"
// @Success 200 {object} dto.ListEntitlementsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /entitlements/search [post]
func (h *EntitlementHandler) QueryEntitlements(c *gin.Context) {
	var filter types.EntitlementFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.ListEntitlements(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list entitlements", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
