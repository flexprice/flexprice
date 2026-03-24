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
// @Summary Create feature
// @ID createFeature
// @Description Use when defining a new feature or capability to gate or meter (e.g. feature flags or usage-based limits). Ideal for boolean or usage features.
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param feature body dto.CreateFeatureRequest true "Feature to create"
// @Success 201 {object} dto.FeatureResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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
// @Summary Update feature
// @ID updateFeature
// @Description Use when changing feature definition (e.g. name, type, or meter). Request body contains the fields to update.
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Feature ID"
// @Param feature body dto.UpdateFeatureRequest true "Feature update data"
// @Success 200 {object} dto.FeatureResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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
// @Summary Delete feature
// @ID deleteFeature
// @Description Use when retiring a feature (e.g. deprecated capability). Returns 200 with success message.
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Feature ID"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

// @Summary Query features
// @ID queryFeature
// @Description Use when listing or searching features (e.g. catalog or entitlement setup). Returns a paginated list; supports filtering and sorting.
// @Tags Features
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.FeatureFilter true "Filter"
// @Success 200 {object} dto.ListFeaturesResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /features/search [post]
func (h *FeatureHandler) QueryFeatures(c *gin.Context) {
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
