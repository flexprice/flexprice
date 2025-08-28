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

type PriceHandler struct {
	service service.PriceService
	log     *logger.Logger
}

func NewPriceHandler(service service.PriceService, log *logger.Logger) *PriceHandler {
	return &PriceHandler{service: service, log: log}
}

// @Summary Create a new price
// @Description Create a new price with the specified configuration. Supports both regular and price unit configurations.
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param price body dto.CreatePriceRequest true "Price configuration"
// @Success 201 {object} dto.PriceResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /prices [post]
func (h *PriceHandler) CreatePrice(c *gin.Context) {
	var req dto.CreatePriceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreatePrice(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Create multiple prices in bulk
// @Description Create multiple prices with the specified configurations. Supports both regular and price unit configurations.
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param prices body dto.CreateBulkPriceRequest true "Bulk price configuration"
// @Success 201 {object} dto.CreateBulkPriceResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /prices/bulk [post]
func (h *PriceHandler) CreateBulkPrice(c *gin.Context) {
	var req dto.CreateBulkPriceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateBulkPrice(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get a price by ID
// @Description Get a price by ID with expanded meter and price unit information
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Price ID"
// @Success 200 {object} dto.PriceResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /prices/{id} [get]
func (h *PriceHandler) GetPrice(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Price ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetPrice(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get prices
// @Description Get prices with the specified filter
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.PriceFilter false "Filter"
// @Success 200 {object} dto.ListPricesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /prices [get]
func (h *PriceHandler) GetPrices(c *gin.Context) {
	var filter types.PriceFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	resp, err := h.service.GetPrices(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update a price
// @Description Update a price with the specified configuration
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Price ID"
// @Param price body dto.UpdatePriceRequest true "Price configuration"
// @Success 200 {object} dto.PriceResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /prices/{id} [put]
func (h *PriceHandler) UpdatePrice(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Price ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdatePriceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdatePrice(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete a price
// @Description Delete a price
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Price ID"
// @Success 200 {object} gin.H
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /prices/{id} [delete]
func (h *PriceHandler) DeletePrice(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Price ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.DeletePriceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	if err := h.service.DeletePrice(c.Request.Context(), id, &req); err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "price deleted successfully"})
}

// @Summary Get prices by plan ID
// @Description Get all prices associated with a specific plan
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param plan_id path string true "Plan ID"
// @Success 200 {object} dto.ListPricesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /prices/plan/{plan_id} [get]
func (h *PriceHandler) GetPricesByPlanID(c *gin.Context) {
	planID := c.Param("plan_id")
	if planID == "" {
		c.Error(ierr.NewError("plan_id is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetPricesByPlanID(c.Request.Context(), planID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get prices by addon ID
// @Description Get all prices associated with a specific addon
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param addon_id path string true "Addon ID"
// @Success 200 {object} dto.ListPricesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /prices/addon/{addon_id} [get]
func (h *PriceHandler) GetPricesByAddonID(c *gin.Context) {
	addonID := c.Param("addon_id")
	if addonID == "" {
		c.Error(ierr.NewError("addon_id is required").
			WithHint("Addon ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetPricesByAddonID(c.Request.Context(), addonID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
