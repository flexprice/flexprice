package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

type IntegrationSyncHandler struct {
	service service.IntegrationSyncService
	logger  *logger.Logger
}

func NewIntegrationSyncHandler(service service.IntegrationSyncService, logger *logger.Logger) *IntegrationSyncHandler {
	return &IntegrationSyncHandler{service: service, logger: logger}
}

func (h *IntegrationSyncHandler) Sync(c *gin.Context) {
	var req dto.IntegrationSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request body").Mark(ierr.ErrValidation))
		return
	}

	if err := h.service.SyncEntity(c.Request.Context(), req); err != nil {
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
