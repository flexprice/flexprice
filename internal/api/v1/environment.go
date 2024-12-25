package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type EnvironmentHandler struct {
	service service.EnvironmentService
	log     *logger.Logger
}

func NewEnvironmentHandler(service service.EnvironmentService, log *logger.Logger) *EnvironmentHandler {
	return &EnvironmentHandler{service: service, log: log}
}

// @Summary Create an environment
// @Description Create an environment
// @Tags environments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param environment body dto.CreateEnvironmentRequest true "Environment"
// @Success 201 {object} dto.EnvironmentResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /environments [post]
func (h *EnvironmentHandler) CreateEnvironment(c *gin.Context) {
	var req dto.CreateEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		NewErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	resp, err := h.service.CreateEnvironment(c.Request.Context(), req)
	if err != nil {
		NewErrorResponse(c, http.StatusInternalServerError, "Failed to create environment", err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get an environment
// @Description Get an environment
// @Tags environments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Environment ID"
// @Success 200 {object} dto.EnvironmentResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /environments/{id} [get]
func (h *EnvironmentHandler) GetEnvironment(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.service.GetEnvironment(c.Request.Context(), id)
	if err != nil {
		NewErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve environment", err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get environments
// @Description Get environments
// @Tags environments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param filter query types.Filter false "Filter"
// @Success 200 {object} dto.ListEnvironmentsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /environments [get]
func (h *EnvironmentHandler) GetEnvironments(c *gin.Context) {
	var filter types.Filter
	if err := c.ShouldBindQuery(&filter); err != nil {
		NewErrorResponse(c, http.StatusBadRequest, "Invalid query parameters", err)
		return
	}

	resp, err := h.service.GetEnvironments(c.Request.Context(), filter)
	if err != nil {
		NewErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve environments", err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update an environment
// @Description Update an environment
// @Tags environments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Environment ID"
// @Param environment body dto.UpdateEnvironmentRequest true "Environment"
// @Success 200 {object} dto.EnvironmentResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /environments/{id} [put]
func (h *EnvironmentHandler) UpdateEnvironment(c *gin.Context) {
	id := c.Param("id")

	var req dto.UpdateEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		NewErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	resp, err := h.service.UpdateEnvironment(c.Request.Context(), id, req)
	if err != nil {
		NewErrorResponse(c, http.StatusInternalServerError, "Failed to update environment", err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
