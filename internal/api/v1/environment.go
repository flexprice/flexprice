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

type EnvironmentHandler struct {
	service service.EnvironmentService
	log     *logger.Logger
}

func NewEnvironmentHandler(service service.EnvironmentService, log *logger.Logger) *EnvironmentHandler {
	return &EnvironmentHandler{service: service, log: log}
}

// @Summary Create an environment
// @ID createEnvironment
// @Description Create an environment
// @Tags Environments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param environment body dto.CreateEnvironmentRequest true "Environment"
// @Success 201 {object} dto.EnvironmentResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /environments [post]
func (h *EnvironmentHandler) CreateEnvironment(c *gin.Context) {
	var req dto.CreateEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateEnvironment(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get an environment
// @ID getEnvironment
// @Description Get an environment
// @Tags Environments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Environment ID"
// @Success 200 {object} dto.EnvironmentResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /environments/{id} [get]
func (h *EnvironmentHandler) GetEnvironment(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.service.GetEnvironment(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get environments
// @ID listEnvironments
// @Description Get environments
// @Tags Environments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.Filter false "Filter"
// @Success 200 {object} dto.ListEnvironmentsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /environments [get]
func (h *EnvironmentHandler) GetEnvironments(c *gin.Context) {
	var filter types.Filter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the query parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetEnvironments(c.Request.Context(), filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update an environment
// @ID updateEnvironment
// @Description Update an environment
// @Tags Environments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Environment ID"
// @Param environment body dto.UpdateEnvironmentRequest true "Environment"
// @Success 200 {object} dto.EnvironmentResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /environments/{id} [put]
func (h *EnvironmentHandler) UpdateEnvironment(c *gin.Context) {
	id := c.Param("id")

	var req dto.UpdateEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateEnvironment(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
