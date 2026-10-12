package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto/admin"
	adminsvc "github.com/flexprice/flexprice/internal/ee/service/admin"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/gin-gonic/gin"
)

type TenantHandler struct {
	service adminsvc.TenantService
}

func NewTenantHandler(service adminsvc.TenantService) *TenantHandler {
	return &TenantHandler{service: service}
}

func (h *TenantHandler) CreateTenant(c *gin.Context) {
	var req admin.CreateTenantRequest
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
