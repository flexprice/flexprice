package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
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

// @Summary Create a new entitlement
// @Description Create a new entitlement with the specified configuration
// @Tags Entitlements
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param entitlement body dto.CreateEntitlementRequest true "Entitlement configuration"
// @Success 201 {object} dto.EntitlementResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /entitlements [post]
func (h *EntitlementHandler) CreateEntitlement(c *gin.Context) {
	var req dto.CreateEntitlementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.service.CreateEntitlement(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get an entitlement by ID
// @Description Get an entitlement by ID
// @Tags Entitlements
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Entitlement ID"
// @Success 200 {object} dto.EntitlementResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /entitlements/{id} [get]
func (h *EntitlementHandler) GetEntitlement(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	resp, err := h.service.GetEntitlement(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get entitlements
// @Description Get entitlements with the specified filter
// @Tags Entitlements
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.EntitlementFilter true "Filter"
// @Success 200 {object} dto.ListEntitlementsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /entitlements [get]
func (h *EntitlementHandler) ListEntitlements(c *gin.Context) {
	var filter types.EntitlementFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Set default filter if not provided
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	resp, err := h.service.ListEntitlements(c.Request.Context(), &filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update an entitlement
// @Description Update an entitlement with the specified configuration
// @Tags Entitlements
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Entitlement ID"
// @Param entitlement body dto.UpdateEntitlementRequest true "Entitlement configuration"
// @Success 200 {object} dto.EntitlementResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /entitlements/{id} [put]
func (h *EntitlementHandler) UpdateEntitlement(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	var req dto.UpdateEntitlementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.service.UpdateEntitlement(c.Request.Context(), id, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete an entitlement
// @Description Delete an entitlement
// @Tags Entitlements
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Entitlement ID"
// @Success 200 {object} gin.H
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /entitlements/{id} [delete]
func (h *EntitlementHandler) DeleteEntitlement(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	if err := h.service.DeleteEntitlement(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "entitlement deleted successfully"})
}
