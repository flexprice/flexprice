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

type IntegrationHandler struct {
	syncService       service.IntegrationSyncService
	mappingService    service.EntityIntegrationMappingService
	connectionService service.ConnectionService
	logger            *logger.Logger
}

func NewIntegrationHandler(
	syncService service.IntegrationSyncService,
	mappingService service.EntityIntegrationMappingService,
	connectionService service.ConnectionService,
	logger *logger.Logger,
) *IntegrationHandler {
	return &IntegrationHandler{
		syncService:       syncService,
		mappingService:    mappingService,
		connectionService: connectionService,
		logger:            logger,
	}
}

func (h *IntegrationHandler) Sync(c *gin.Context) {
	var req dto.IntegrationSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request body").Mark(ierr.ErrValidation))
		return
	}
	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	if err := h.syncService.SyncEntity(c.Request.Context(), req); err != nil {
		h.logger.Error(c.Request.Context(), "failed to trigger integration sync",
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
	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}
	resp, err := h.mappingService.LinkIntegrationMapping(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary Delink integration mapping
// @ID delinkIntegrationMapping
// @Description Soft-delete (archive) the mapping between a FlexPrice entity and a provider entity.
// @Tags Integrations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.DelinkIntegrationMappingRequest true "Delink mapping request"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /integrations/link [delete]
func (h *IntegrationHandler) Delink(c *gin.Context) {
	var req dto.DelinkIntegrationMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request body").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.mappingService.DelinkIntegrationMapping(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary Get entity integration mappings
// @ID getEntityIntegrationMappings
// @Description Get integration mappings for a specific entity by entity type and entity ID.
// @Tags Integrations
// @Produce json
// @Security ApiKeyAuth
// @Param entity_type query string true "Entity type (customer, plan, invoice, subscription, payment, credit_note, addon, item, item_price, price)"
// @Param entity_id query string true "Entity ID"
// @Success 200 {object} dto.ListEntityIntegrationMappingsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /integrations/mappings [get]
func (h *IntegrationHandler) GetMappings(c *gin.Context) {
	entityType := c.Query("entity_type")
	entityID := c.Query("entity_id")

	if entityType == "" || entityID == "" {
		c.Error(ierr.NewError("entity_type and entity_id are required").
			WithHint("Both entity_type and entity_id query parameters are required").
			Mark(ierr.ErrValidation))
		return
	}
	eType := types.IntegrationEntityType(entityType)
	if err := eType.Validate(); err != nil {
		c.Error(err)
		return
	}
	ctx := c.Request.Context()
	filter := &types.EntityIntegrationMappingFilter{
		QueryFilter: types.NewNoLimitPublishedQueryFilter(),
		EntityType:  eType,
		EntityID:    entityID,
	}
	resp, err := h.mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary Get integration configurations
// @ID getIntegrationConfig
// @Description Returns the base capabilities and current sync configuration for all connected providers.
// @Tags Integrations
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.IntegrationConfigResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /integrations/config [get]
func (h *IntegrationHandler) GetConfig(c *gin.Context) {
	filter := &types.ConnectionFilter{
		QueryFilter: types.NewNoLimitPublishedQueryFilter(),
	}
	connResp, err := h.connectionService.GetConnections(c.Request.Context(), filter)
	if err != nil {
		h.logger.Error(c.Request.Context(), "failed to get connections for integration config", "error", err)
		c.Error(err)
		return
	}

	entries := make([]dto.IntegrationConfigEntry, 0)
	for _, conn := range connResp.Connections {
		baseConfig := types.ProviderBaseSyncConfig(conn.ProviderType)
		if baseConfig == nil {
			continue
		}
		entries = append(entries, dto.IntegrationConfigEntry{
			Provider:      conn.ProviderType,
			BaseConfig:    baseConfig,
			CurrentConfig: dto.EntityOnlySyncConfig(conn.SyncConfig),
		})
	}

	c.JSON(http.StatusOK, dto.IntegrationConfigResponse{
		Integrations: entries,
	})
}
