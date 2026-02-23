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

type TenantHandler struct {
	service service.TenantService
	log     *logger.Logger
}

func NewTenantHandler(
	service service.TenantService,
	log *logger.Logger,
) *TenantHandler {
	return &TenantHandler{
		service: service,
		log:     log,
	}
}

// @Summary Get tenant by ID
// @ID getTenantById
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
// @ID updateTenant
// @Description Update a tenant's details including name and billing information
// @Tags Tenants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.UpdateTenantRequest true "Update tenant request"
// @Success 200 {object} dto.TenantResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /tenants/update [put]
func (h *TenantHandler) UpdateTenant(c *gin.Context) {
	tenantID := c.Request.Context().Value(types.CtxTenantID).(string)

	var req dto.UpdateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateTenant(c.Request.Context(), tenantID, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get billing usage for the current tenant
// @ID getTenantBillingUsage
// @Description Get the subscription and usage details for the current tenant
// @Tags Tenants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.TenantBillingUsage
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /tenant/billing [get]
func (h *TenantHandler) GetTenantBillingUsage(c *gin.Context) {
	usage, err := h.service.GetBillingUsage(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, usage)
}
