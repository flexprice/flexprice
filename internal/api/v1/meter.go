package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
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
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /meters [post]
func (h *MeterHandler) CreateMeter(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.CreateMeterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request payload"})
		return
	}

	meter, err := h.service.CreateMeter(ctx, &req)
	if err != nil {
		h.log.Error("Failed to create meter ", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to create meter"})
		return
	}

	c.JSON(http.StatusCreated, dto.ToMeterResponse(meter))
}

// @Summary List meters
// @Description Get all meters
// @Tags Meters
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {array} dto.MeterResponse
// @Failure 500 {object} ErrorResponse
// @Router /meters [get]
func (h *MeterHandler) GetAllMeters(c *gin.Context) {
	ctx := c.Request.Context()
	meters, err := h.service.GetAllMeters(ctx)
	if err != nil {
		h.log.Error("Failed to get meters", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to get meters"})
		return
	}

	response := make([]*dto.MeterResponse, len(meters))
	for i, m := range meters {
		response[i] = dto.ToMeterResponse(m)
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
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /meters/{id} [get]
func (h *MeterHandler) GetMeter(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()
	meter, err := h.service.GetMeter(ctx, id)
	if err != nil {
		h.log.Error("Failed to get meter", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to get meter"})
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
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /meters/{id}/disable [post]
func (h *MeterHandler) DisableMeter(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()
	if err := h.service.DisableMeter(ctx, id); err != nil {
		h.log.Error("Failed to disable meter", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to disable meter"})
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
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /meters/{id} [delete]
func (h *MeterHandler) DeleteMeter(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()
	if err := h.service.DisableMeter(ctx, id); err != nil {
		h.log.Error("Failed to delete meter", "error", err)
		NewErrorResponse(c, http.StatusInternalServerError, "Failed to delete meter", err)
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
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /meters/{id} [put]
func (h *MeterHandler) UpdateMeter(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Meter ID is required"})
		return
	}

	ctx := c.Request.Context()
	var req dto.UpdateMeterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request payload"})
		return
	}

	if len(req.Filters) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Filters cannot be empty"})
		return
	}

	meter, err := h.service.UpdateMeter(ctx, id, req.Filters)
	if err != nil {
		h.log.Error("Failed to update meter", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to update meter"})
		return
	}

	c.JSON(http.StatusOK, dto.ToMeterResponse(meter))
}
