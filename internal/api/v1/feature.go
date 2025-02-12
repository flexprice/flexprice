package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type FeatureHandler struct {
	service service.FeatureService
	log     *logger.Logger
}

func NewFeatureHandler(service service.FeatureService, log *logger.Logger) *FeatureHandler {
	return &FeatureHandler{service: service, log: log}
}

// CreateFeature godoc
// @Summary Create a new feature
// @Description Create a new feature with the specified configuration
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param feature body dto.CreateFeatureRequest true "Feature configuration"
// @Success 201 {object} dto.FeatureResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /features [post]
func (h *FeatureHandler) CreateFeature(c *gin.Context) {
	var req dto.CreateFeatureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.service.CreateFeature(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// GetFeature godoc
// @Summary Get a feature by ID
// @Description Get a feature by ID
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Feature ID"
// @Success 200 {object} dto.FeatureResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /features/{id} [get]
func (h *FeatureHandler) GetFeature(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	resp, err := h.service.GetFeature(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetFeatures godoc
// @Summary Get features
// @Description Get features with the specified filter
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.FeatureFilter true "Filter"
// @Success 200 {object} dto.ListFeaturesResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /features [get]
func (h *FeatureHandler) GetFeatures(c *gin.Context) {
	var filter types.FeatureFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	resp, err := h.service.GetFeatures(c.Request.Context(), &filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateFeature godoc
// @Summary Update a feature
// @Description Update a feature with the specified configuration
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Feature ID"
// @Param feature body dto.UpdateFeatureRequest true "Feature configuration"
// @Success 200 {object} dto.FeatureResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /features/{id} [put]
func (h *FeatureHandler) UpdateFeature(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	var req dto.UpdateFeatureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.service.UpdateFeature(c.Request.Context(), id, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// DeleteFeature godoc
// @Summary Delete a feature
// @Description Delete a feature
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Feature ID"
// @Success 200 {object} gin.H
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /features/{id} [delete]
func (h *FeatureHandler) DeleteFeature(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	if err := h.service.DeleteFeature(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "feature deleted successfully"})
}
