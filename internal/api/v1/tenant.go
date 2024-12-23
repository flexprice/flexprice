package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
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
// @Tags Tenant
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.CreateTenantRequest true "Create tenant request"
// @Success 201 {object} dto.TenantResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /tenants [post]
func (h *TenantHandler) CreateTenant(c *gin.Context) {
	var req dto.CreateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		NewErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	resp, err := h.service.CreateTenant(c.Request.Context(), req)
	if err != nil {
		NewErrorResponse(c, http.StatusInternalServerError, "Failed to create tenant", err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get tenant by ID
// @Description Get tenant by ID
// @Tags Tenant
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Tenant ID"
// @Success 200 {object} dto.TenantResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /tenants/{id} [get]
func (h *TenantHandler) GetTenantByID(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.service.GetTenantByID(c.Request.Context(), id)
	if err != nil {
		NewErrorResponse(c, http.StatusNotFound, "Tenant not found", err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
