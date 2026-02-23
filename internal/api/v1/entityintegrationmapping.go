package v1

import (
	"net/http"
	"strconv"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// EntityIntegrationMappingHandler handles entity integration mapping API requests
type EntityIntegrationMappingHandler struct {
	EntityIntegrationMappingService service.EntityIntegrationMappingService
	logger                          *logger.Logger
}

// NewEntityIntegrationMappingHandler creates a new entity integration mapping handler
func NewEntityIntegrationMappingHandler(
	entityIntegrationMappingService service.EntityIntegrationMappingService,
	logger *logger.Logger,
) *EntityIntegrationMappingHandler {
	return &EntityIntegrationMappingHandler{
		EntityIntegrationMappingService: entityIntegrationMappingService,
		logger:                          logger,
	}
}

// CreateEntityIntegrationMapping godoc
// @Summary Create entity integration mapping
// @ID createEntityIntegrationMapping
// @Description Use when linking a FlexPrice entity to an external system (e.g. CRM or payment provider) so you can sync or reconcile by external ID.
// @Tags Entity Integration Mappings
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param entity_integration_mapping body dto.CreateEntityIntegrationMappingRequest true "Entity integration mapping data"
// @Success 201 {object} dto.EntityIntegrationMappingResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 409 {object} ierr.ErrorResponse "Conflict"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /entity-integration-mappings [post]
func (h *EntityIntegrationMappingHandler) CreateEntityIntegrationMapping(c *gin.Context) {
	var req dto.CreateEntityIntegrationMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("failed to bind create entity integration mapping request", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request body").
			Mark(ierr.ErrValidation))
		return
	}

	mapping, err := h.EntityIntegrationMappingService.CreateEntityIntegrationMapping(c.Request.Context(), req)
	if err != nil {
		h.logger.Errorw("failed to create entity integration mapping", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, mapping)
}

func (h *EntityIntegrationMappingHandler) GetEntityIntegrationMapping(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("entity integration mapping ID is required").
			WithHint("Entity integration mapping ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	mapping, err := h.EntityIntegrationMappingService.GetEntityIntegrationMapping(c.Request.Context(), id)
	if err != nil {
		h.logger.Errorw("failed to get entity integration mapping", "error", err, "id", id)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, mapping)
}

// DeleteEntityIntegrationMapping godoc
// @Summary Delete entity integration mapping
// @ID deleteEntityIntegrationMapping
// @Description Use when unlinking a FlexPrice entity from an external system or cleaning up stale integration mappings.
// @Tags Entity Integration Mappings
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Entity integration mapping ID"
// @Success 204 "No Content"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /entity-integration-mappings/{id} [delete]
func (h *EntityIntegrationMappingHandler) DeleteEntityIntegrationMapping(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("entity integration mapping ID is required").
			WithHint("Entity integration mapping ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.EntityIntegrationMappingService.DeleteEntityIntegrationMapping(c.Request.Context(), id)
	if err != nil {
		h.logger.Errorw("failed to delete entity integration mapping", "error", err, "id", id)
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *EntityIntegrationMappingHandler) ListEntityIntegrationMappings(c *gin.Context) {
	// Parse query parameters
	filter := &types.EntityIntegrationMappingFilter{
		QueryFilter: &types.QueryFilter{},
	}

	if entityID := c.Query("entity_id"); entityID != "" {
		filter.EntityID = entityID
	}
	if entityType := c.Query("entity_type"); entityType != "" {
		filter.EntityType = types.IntegrationEntityType(entityType)
	}
	if providerType := c.Query("provider_type"); providerType != "" {
		filter.ProviderTypes = []string{providerType}
	}
	if providerEntityID := c.Query("provider_entity_id"); providerEntityID != "" {
		filter.ProviderEntityIDs = []string{providerEntityID}
	}

	// Parse pagination parameters
	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 100 {
		limit = 20
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	filter.Limit = &limit
	filter.Offset = &offset

	mappings, err := h.EntityIntegrationMappingService.GetEntityIntegrationMappings(c.Request.Context(), filter)
	if err != nil {
		h.logger.Errorw("failed to list entity integration mappings", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, mappings)
}
