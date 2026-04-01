package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

type IntegrationMappingLinkHandler struct {
	service service.EntityIntegrationMappingService
	logger  *logger.Logger
}

func NewIntegrationMappingLinkHandler(service service.EntityIntegrationMappingService, logger *logger.Logger) *IntegrationMappingLinkHandler {
	return &IntegrationMappingLinkHandler{service: service, logger: logger}
}

// @Summary Link integration mapping
// @ID linkIntegrationMapping
// @Description Link a FlexPrice entity to provider entity with provider-specific side effects.
// @Tags Integrations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.LinkIntegrationMappingRequest true "Link mapping request"
// @Success 200 {object} dto.LinkIntegrationMappingResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /integrations/mappings/link [post]
func (h *IntegrationMappingLinkHandler) Link(c *gin.Context) {
	var req dto.LinkIntegrationMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request body").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.service.LinkIntegrationMapping(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
