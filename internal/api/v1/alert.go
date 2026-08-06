package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type AlertSettingsHandler struct {
	alertService service.AlertService
	log          *logger.Logger
}

func NewAlertSettingsHandler(alertService service.AlertService, log *logger.Logger) *AlertSettingsHandler {
	return &AlertSettingsHandler{
		alertService: alertService,
		log:          log,
	}
}

// @Summary Create alert settings
// @ID createAlertSettings
// @Description Configure a subscription, line item, or group spend alert.
// @Tags AlertSettings
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param alert_settings body dto.CreateAlertSettingsRequest true "Alert settings"
// @Success 201 {object} dto.AlertSettingsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /alerts/setting [post]
func (h *AlertSettingsHandler) CreateAlertSettings(c *gin.Context) {
	var req dto.CreateAlertSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.alertService.CreateAlertSettings(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Update alert settings
// @ID updateAlertSettings
// @Description Patch an alert setting's config; omitted fields keep their stored value.
// @Tags AlertSettings
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Alert Settings ID"
// @Param alert_settings body dto.UpdateAlertSettingsRequest true "Alert settings"
// @Success 200 {object} dto.AlertSettingsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /alerts/setting/{id} [put]
func (h *AlertSettingsHandler) UpdateAlertSettings(c *gin.Context) {
	id := c.Param("id")

	var req dto.UpdateAlertSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.alertService.UpdateAlertSettings(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete alert settings
// @ID deleteAlertSettings
// @Description Soft delete an alert setting.
// @Tags AlertSettings
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Alert Settings ID"
// @Success 204 "No Content"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /alerts/setting/{id} [delete]
func (h *AlertSettingsHandler) DeleteAlertSettings(c *gin.Context) {
	id := c.Param("id")

	if err := h.alertService.DeleteAlertSettings(c.Request.Context(), id); err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary Get alert settings
// @ID getAlertSettings
// @Description Fetch a single alert setting by id.
// @Tags AlertSettings
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Alert Settings ID"
// @Success 200 {object} dto.AlertSettingsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /alerts/setting/{id} [get]
func (h *AlertSettingsHandler) GetAlertSettings(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.alertService.GetAlertSettings(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Query alert settings
// @ID queryAlertSettings
// @Description List or search alert settings. Returns a paginated list.
// @Tags AlertSettings
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.AlertSettingsFilter true "Filter"
// @Success 200 {object} dto.ListAlertSettingsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @x-scope "read"
// @Router /alerts/setting/search [post]
func (h *AlertSettingsHandler) QueryAlertSettings(c *gin.Context) {
	var filter types.AlertSettingsFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	resp, err := h.alertService.ListAlertSettings(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
