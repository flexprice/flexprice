package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

type IntegrationHandler struct {
	syncService    service.IntegrationSyncService
	mappingService service.EntityIntegrationMappingService
	logger         *logger.Logger
}

func NewIntegrationHandler(
	syncService service.IntegrationSyncService,
	mappingService service.EntityIntegrationMappingService,
	logger *logger.Logger,
) *IntegrationHandler {
	return &IntegrationHandler{
		syncService:    syncService,
		mappingService: mappingService,
		logger:         logger,
	}
}

func (h *IntegrationHandler) Sync(c *gin.Context) {
	var req dto.IntegrationSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request body").Mark(ierr.ErrValidation))
		return
	}

	if err := h.syncService.SyncEntity(c.Request.Context(), req); err != nil {
		h.logger.Errorw("failed to trigger integration sync",
			"error", err,
			"entity_type", req.EntityType,
			"entity_id", req.EntityID,
		)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "integration sync triggered successfully"})
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
// @Router /integrations/link [post]
func (h *IntegrationHandler) Link(c *gin.Context) {
	var req dto.LinkIntegrationMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request body").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.mappingService.LinkIntegrationMapping(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
