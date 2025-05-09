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

type FeatureHandler struct {
	featureService service.FeatureService
	log            *logger.Logger
}

func NewFeatureHandler(featureService service.FeatureService, log *logger.Logger) *FeatureHandler {
	return &FeatureHandler{
		featureService: featureService,
		log:            log,
	}
}

// CreateFeature godoc
// @Summary Create a new feature
// @Description Create a new feature
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param feature body dto.CreateFeatureRequest true "Feature to create"
// @Success 201 {object} dto.FeatureResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /features [post]
func (h *FeatureHandler) CreateFeature(c *gin.Context) {
	var req dto.CreateFeatureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	feature, err := h.featureService.CreateFeature(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, feature)
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
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /features/{id} [get]
func (h *FeatureHandler) GetFeature(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("feature ID is required").
			WithHint("Feature ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	feature, err := h.featureService.GetFeature(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, feature)
}

// ListFeatures godoc
// @Summary List features
// @Description List features with optional filtering
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.FeatureFilter true "Filter"
// @Success 200 {object} dto.ListFeaturesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /features [get]
func (h *FeatureHandler) ListFeatures(c *gin.Context) {
	var filter types.FeatureFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	resp, err := h.featureService.GetFeatures(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateFeature godoc
// @Summary Update a feature
// @Description Update a feature by ID
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Feature ID"
// @Param feature body dto.UpdateFeatureRequest true "Feature update data"
// @Success 200 {object} dto.FeatureResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /features/{id} [put]
func (h *FeatureHandler) UpdateFeature(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("feature ID is required").
			WithHint("Feature ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateFeatureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	feature, err := h.featureService.UpdateFeature(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, feature)
}

// DeleteFeature godoc
// @Summary Delete a feature
// @Description Delete a feature by ID
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Feature ID"
// @Success 200 {object} gin.H
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /features/{id} [delete]
func (h *FeatureHandler) DeleteFeature(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("feature ID is required").
			WithHint("Feature ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	if err := h.featureService.DeleteFeature(c.Request.Context(), id); err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "feature deleted successfully"})
}

// ListFeaturesByFilter godoc
// @Summary List features by filter
// @Description List features by filter
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.FeatureFilter true "Filter"
// @Success 200 {object} dto.ListFeaturesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /features/search [post]
func (h *FeatureHandler) ListFeaturesByFilter(c *gin.Context) {
	var filter types.FeatureFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	resp, err := h.featureService.GetFeatures(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
