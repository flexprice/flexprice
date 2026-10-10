package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto/admin"
	adminsvc "github.com/flexprice/flexprice/internal/ee/service/admin"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/gin-gonic/gin"
)

type SettingsHandler struct {
	service adminsvc.SettingsService
}

func NewSettingsHandler(service adminsvc.SettingsService) *SettingsHandler {
	return &SettingsHandler{service: service}
}

func (h *SettingsHandler) UpdateTenantConfig(c *gin.Context) {
	var req admin.UpdateTenantConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateTenantConfig(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
