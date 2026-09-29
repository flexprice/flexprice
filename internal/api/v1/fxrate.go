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

type FXRateHandler struct {
	service service.FXRateService
	logger  *logger.Logger
}

func NewFXRateHandler(service service.FXRateService, logger *logger.Logger) *FXRateHandler {
	return &FXRateHandler{service: service, logger: logger}
}

// @Summary Create an FX rate
// @ID createFXRate
// @Description Configure a fixed exchange rate at tenant, customer or subscription scope. Overrides need a tenant rate for the same pair.
// @Tags FX Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param fx_rate body dto.CreateFXRateRequest true "FX rate to create"
// @Success 201 {object} dto.FXRateResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /fx-rates [post]
func (h *FXRateHandler) CreateFXRate(c *gin.Context) {
	var req dto.CreateFXRateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request format").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.service.CreateFXRate(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, resp)
}

// @Summary Get an FX rate
// @ID getFXRate
// @Description Load a single FX rate by ID.
// @Tags FX Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "FX rate ID"
// @Success 200 {object} dto.FXRateResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /fx-rates/{id} [get]
func (h *FXRateHandler) GetFXRate(c *gin.Context) {
	resp, err := h.service.GetFXRate(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary List FX rates
// @ID listFXRates
// @Description List FX rates with optional filters by scope, scope_id, currency pair and status.
// @Tags FX Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.FXRateFilter true "Filter"
// @Success 200 {object} dto.ListFXRatesResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /fx-rates [get]
func (h *FXRateHandler) ListFXRates(c *gin.Context) {
	var filter types.FXRateFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request format").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.service.ListFXRates(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary Query FX rates
// @ID queryFXRates
// @Description Filter FX rates via a request body (POST used for a complex query, but read-only).
// @Tags FX Rates
// @x-scope "read"
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.FXRateFilter true "Filter"
// @Success 200 {object} dto.ListFXRatesResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /fx-rates/search [post]
func (h *FXRateHandler) QueryFXRates(c *gin.Context) {
	var filter types.FXRateFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request format").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.service.ListFXRates(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary Update an FX rate
// @ID updateFXRate
// @Description Update a rate's value, validity window (overrides only) or metadata. Scope, scope_id and the currency pair are immutable.
// @Tags FX Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "FX rate ID"
// @Param fx_rate body dto.UpdateFXRateRequest true "FX rate fields to update"
// @Success 200 {object} dto.FXRateResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /fx-rates/{id} [put]
func (h *FXRateHandler) UpdateFXRate(c *gin.Context) {
	var req dto.UpdateFXRateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request format").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.service.UpdateFXRate(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary Delete an FX rate
// @ID deleteFXRate
// @Description Archive a customer or subscription override. Tenant rates cannot be deleted.
// @Tags FX Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "FX rate ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /fx-rates/{id} [delete]
func (h *FXRateHandler) DeleteFXRate(c *gin.Context) {
	if err := h.service.DeleteFXRate(c.Request.Context(), c.Param("id")); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "FX rate deleted"})
}

// @Summary Resolve an FX rate
// @ID resolveFXRate
// @Description Show which rate a customer or subscription gets now for a currency pair. Uses the same resolution as invoice finalization.
// @Tags FX Rates
// @x-scope "read"
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param from query string true "From (charge) currency"
// @Param to query string true "To (billing) currency"
// @Param customer_id query string false "Customer ID"
// @Param subscription_id query string false "Subscription ID"
// @Success 200 {object} dto.ResolveFXRateResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "No rate configured"
// @Router /fx-rates/resolve [get]
func (h *FXRateHandler) ResolveFXRate(c *gin.Context) {
	from := c.Query("from")
	to := c.Query("to")
	if from == "" || to == "" {
		c.Error(ierr.NewError("from and to are required").
			WithHint("Provide both from and to currency codes.").
			Mark(ierr.ErrValidation))
		return
	}
	res, err := h.service.ResolveRate(c.Request.Context(), service.ResolveFXRateRequest{
		From:           from,
		To:             to,
		CustomerID:     c.Query("customer_id"),
		SubscriptionID: c.Query("subscription_id"),
	})
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, dto.ResolveFXRateResponse{
		Rate:         res.Rate.String(),
		RateID:       res.RateID,
		Scope:        res.Scope,
		FromCurrency: res.From,
		ToCurrency:   res.To,
	})
}
