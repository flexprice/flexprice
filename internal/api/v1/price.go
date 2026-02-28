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
	return &PriceHandler{
		service: service,
		log:     log,
	}
}

// @Summary Create price
// @ID createPrice
// @Description Use when adding a new price to a plan or catalog (e.g. per-seat, flat, or metered). Ideal for both simple and usage-based pricing.
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param price body dto.CreatePriceRequest true "Price configuration"
// @Success 201 {object} dto.PriceResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

// @Summary Create prices in bulk
// @ID createPricesBulk
// @Description Use when creating many prices at once (e.g. importing a catalog or setting up a plan with multiple tiers).
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param prices body dto.CreateBulkPriceRequest true "Bulk price configuration"
// @Success 201 {object} dto.CreateBulkPriceResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

// @Summary Get price
// @ID getPrice
// @Description Use when you need to load a single price (e.g. for display or editing). Response includes expanded meter and price unit when applicable.
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Price ID"
// @Success 200 {object} dto.PriceResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

func (h *PriceHandler) ListPrices(c *gin.Context) {
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

// @Summary Update price
// @ID updatePrice
// @Description Use when changing price configuration (e.g. amount, billing scheme, or metadata).
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Price ID"
// @Param price body dto.UpdatePriceRequest true "Price configuration"
// @Success 200 {object} dto.PriceResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

// @Summary Delete price
// @ID deletePrice
// @Description Use when retiring a price (e.g. end-of-life or replacement). Optional effective date or cascade for subscriptions.
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Price ID"
// @Param request body dto.DeletePriceRequest true "Delete Price Request"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

	if err := h.service.DeletePrice(c.Request.Context(), id, req); err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "price deleted successfully"})
}

// @Summary Get price by lookup key
// @ID getPriceByLookupKey
// @Description Use when resolving a price by external id (e.g. from your catalog or CMS). Ideal for integrations.
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param lookup_key path string true "Lookup key"
// @Success 200 {object} dto.PriceResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /prices/lookup/{lookup_key} [get]
func (h *PriceHandler) GetByLookupKey(c *gin.Context) {
	lookupKey := c.Param("lookup_key")
	if lookupKey == "" {
		c.Error(ierr.NewError("lookup key is required").
			WithHint("Lookup key is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetByLookupKey(c.Request.Context(), lookupKey)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Query prices
// @ID queryPrice
// @Description Use when listing or searching prices (e.g. plan builder or catalog). Returns a paginated list; supports filtering and sorting.
// @Tags Prices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.PriceFilter true "Filter"
// @Success 200 {object} dto.ListPricesResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /prices/search [post]
func (h *PriceHandler) QueryPrices(c *gin.Context) {
	var filter types.PriceFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
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
