package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

type TenantHandler struct {
	service service.TenantService
	log     *logger.Logger
}

func NewTenantHandler(service service.TenantService, log *logger.Logger) *TenantHandler {
	return &TenantHandler{service: service, log: log}
}

// @Summary Create a new tenant
// @Description Create a new tenant
// @Tags Tenants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.CreateTenantRequest true "Create tenant request"
// @Success 201 {object} dto.TenantResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /tenants [post]
func (h *TenantHandler) CreateTenant(c *gin.Context) {
	var req dto.CreateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateTenant(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get tenant by ID
// @Description Get tenant by ID
// @Tags Tenants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tenant ID"
// @Success 200 {object} dto.TenantResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /tenants/{id} [get]
func (h *TenantHandler) GetTenantByID(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.service.GetTenantByID(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update a tenant
// @Description Update a tenant's details including name and billing information
// @Tags Tenants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tenant ID"
// @Param request body dto.UpdateTenantRequest true "Update tenant request"
// @Success 200 {object} dto.TenantResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /tenants/{id} [put]
func (h *TenantHandler) UpdateTenant(c *gin.Context) {
	id := c.Param("id")

	var req dto.UpdateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateTenant(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
