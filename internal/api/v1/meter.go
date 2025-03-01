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

type MeterHandler struct {
	service service.MeterService
	log     *logger.Logger
}

func NewMeterHandler(service service.MeterService, log *logger.Logger) *MeterHandler {
	return &MeterHandler{service: service, log: log}
}

// @Summary Create meter
// @Description Create a new meter with the specified configuration
// @Tags Meters
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param meter body dto.CreateMeterRequest true "Meter configuration"
// @Success 201 {object} dto.MeterResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /meters [post]
func (h *MeterHandler) CreateMeter(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.CreateMeterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	meter, err := h.service.CreateMeter(ctx, &req)
	if err != nil {
		h.log.Error("Failed to create meter", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, dto.ToMeterResponse(meter))
}

// @Summary List meters
// @Description Get all meters
// @Tags Meters
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.MeterFilter false "Filter"
// @Success 200 {array} dto.MeterResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /meters [get]
func (h *MeterHandler) GetAllMeters(c *gin.Context) {
	ctx := c.Request.Context()
	var filter types.MeterFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		h.log.Error("Failed to bind query", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	response, err := h.service.GetMeters(ctx, &filter)
	if err != nil {
		h.log.Error("Failed to get meters", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary Get meter
// @Description Get a specific meter by ID
// @Tags Meters
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Meter ID"
// @Success 200 {object} dto.MeterResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /meters/{id} [get]
func (h *MeterHandler) GetMeter(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()
	meter, err := h.service.GetMeter(ctx, id)
	if err != nil {
		h.log.Error("Failed to get meter", "error", err)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, dto.ToMeterResponse(meter))
}

// @Summary Disable meter [TODO: Deprecate]
// @Description Disable an existing meter
// @Tags Meters
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Meter ID"
// @Success 200 {object} map[string]string "message:Meter disabled successfully"
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /meters/{id}/disable [post]
func (h *MeterHandler) DisableMeter(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()
	if err := h.service.DisableMeter(ctx, id); err != nil {
		h.log.Error("Failed to disable meter", "error", err)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Meter disabled successfully"})
}

// @Summary Delete meter
// @Description Delete an existing meter
// @Tags Meters
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Meter ID"
// @Success 200 {object} map[string]string "message:Meter deleted successfully"
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /meters/{id} [delete]
func (h *MeterHandler) DeleteMeter(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()
	if err := h.service.DisableMeter(ctx, id); err != nil {
		h.log.Error("Failed to delete meter", "error", err)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Meter deleted successfully"})
}

// @Summary Update meter
// @Description Update an existing meter
// @Tags Meters
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Meter ID"
// @Param meter body dto.UpdateMeterRequest true "Meter configuration"
// @Success 200 {object} dto.MeterResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /meters/{id} [put]
func (h *MeterHandler) UpdateMeter(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("meter ID is required").
			WithHint("Meter ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	ctx := c.Request.Context()
	var req dto.UpdateMeterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request payload").
			Mark(ierr.ErrValidation))
		return
	}

	if len(req.Filters) == 0 {
		c.Error(ierr.NewError("filters cannot be empty").
			WithHint("At least one filter must be provided").
			Mark(ierr.ErrValidation))
		return
	}

	meter, err := h.service.UpdateMeter(ctx, id, req.Filters)
	if err != nil {
		h.log.Error("Failed to update meter", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.ToMeterResponse(meter))
}
