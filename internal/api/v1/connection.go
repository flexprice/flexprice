package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type ConnectionHandler struct {
	connectionService service.ConnectionService
	gatewayService    service.GatewayService
	entitySyncService service.EntitySyncService
	log               *logger.Logger
}

func NewConnectionHandler(
	connectionService service.ConnectionService,
	gatewayService service.GatewayService,
	entitySyncService service.EntitySyncService,
	log *logger.Logger,
) *ConnectionHandler {
	return &ConnectionHandler{
		connectionService: connectionService,
		gatewayService:    gatewayService,
		entitySyncService: entitySyncService,
		log:               log,
	}
}

// CreateConnection godoc
// @Summary Create a new connection
// @Description Create a new connection to an external service
// @Tags Integrations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param integration body dto.CreateConnectionRequest true "Integration to create"
// @Success 201 {object} dto.ConnectionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /integrations [post]
func (h *ConnectionHandler) CreateConnection(c *gin.Context) {
	var req dto.CreateConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	connection, err := h.connectionService.Create(c.Request.Context(), &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, dto.ToConnectionResponse(connection))
}

// GetConnection godoc
// @Summary Get a connection by ID
// @Description Get a connection by ID
// @Tags Integrations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Connection ID"
// @Success 200 {object} dto.ConnectionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /connections/{id} [get]
func (h *ConnectionHandler) GetConnection(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("connection ID is required").
			WithHint("Connection ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	connection, err := h.connectionService.Get(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.ToConnectionResponse(connection))
}

// GetConnectionByCode godoc
// @Summary Get a connection by connection code
// @Description Get a connection by connection code
// @Tags Integrations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param code path string true "Connection Code"
// @Success 200 {object} dto.ConnectionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /integrations/code/{code} [get]
func (h *ConnectionHandler) GetConnectionByCode(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.Error(ierr.NewError("connection code is required").
			WithHint("Connection code is required").
			Mark(ierr.ErrValidation)) 
		return
	}

	connection, err := h.connectionService.GetByConnectionCode(c.Request.Context(), code)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.ToConnectionResponse(connection))
}

// GetConnectionByProviderType godoc
// @Summary Get a connection by provider type
// @Description Get a connection by provider type
// @Tags Integrations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param provider path string true "Provider Type"
// @Success 200 {object} dto.ConnectionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /connections/provider/{provider} [get]
func (h *ConnectionHandler) GetConnectionByProviderType(c *gin.Context) {
	provider := types.SecretProvider(c.Param("provider"))
	if provider == "" {
		c.Error(ierr.NewError("provider type is required").
			WithHint("Provider type is required").
			Mark(ierr.ErrValidation))
		return
	}

	connection, err := h.connectionService.GetByProviderType(c.Request.Context(), provider)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.ToConnectionResponse(connection))
}

// ListConnections godoc
// @Summary List connections
// @Description List connections with optional filtering
// @Tags Integrations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.ConnectionFilter false "Filter"
// @Success 200 {object} dto.ListConnectionsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /integrations [get]
func (h *ConnectionHandler) ListConnections(c *gin.Context) {
	var filter types.ConnectionFilter

	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	response, err := h.connectionService.List(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// UpdateConnection godoc
// @Summary Update a connection
// @Description Update a connection by ID
// @Tags Integrations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Connection ID"
// @Param connection body dto.UpdateConnectionRequest true "Connection update data"
// @Success 200 {object} dto.ConnectionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /integrations/{id} [put]
func (h *ConnectionHandler) UpdateConnection(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("connection ID is required").
			WithHint("Connection ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	connection, err := h.connectionService.Update(c.Request.Context(), id, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.ToConnectionResponse(connection))
}

// DeleteConnection godoc
// @Summary Delete a connection
// @Description Delete a connection by ID
// @Tags Integrations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Connection ID"
// @Success 204 "No Content"
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /integrations/{id} [delete]
func (h *ConnectionHandler) DeleteConnection(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("connection ID is required").
			WithHint("Connection ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	if err := h.connectionService.Delete(c.Request.Context(), id); err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}
