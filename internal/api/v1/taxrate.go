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

type TaxRateHandler struct {
	service service.TaxService
	logger  *logger.Logger
}

func NewTaxRateHandler(service service.TaxService, logger *logger.Logger) *TaxRateHandler {
	return &TaxRateHandler{
		service: service,
		logger:  logger,
	}
}

// @Summary Create a tax rate
// @Description Create a tax rate
// @Tags Tax Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param tax_rate body dto.CreateTaxRateRequest true "Tax rate to create"
// @Success 201 {object} dto.TaxRateResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxrate [post]
func (h *TaxRateHandler) CreateTaxRate(c *gin.Context) {
	var req dto.CreateTaxRateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateTaxRate(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get a tax rate
// @Description Get a tax rate
// @Tags Tax Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax rate ID"
// @Success 200 {object} dto.TaxRateResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxrate/{id} [get]
func (h *TaxRateHandler) GetTaxRate(c *gin.Context) {
	taxRates, err := h.service.GetTaxRate(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, taxRates)
}

// @Summary Get tax rates
// @Description Get tax rates
// @Tags Tax Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.TaxRateFilter true "Filter"
// @Success 200 {object} []dto.TaxRateResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxrate [get]
func (h *TaxRateHandler) GetTaxRates(c *gin.Context) {
	var filter types.TaxRateFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	taxRates, err := h.service.ListTaxRates(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, taxRates)
}

// @Summary Update a tax rate
// @Description Update a tax rate
// @Tags Tax Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax rate ID"
// @Param tax_rate body dto.UpdateTaxRateRequest true "Tax rate to update"
// @Success 200 {object} dto.TaxRateResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxrate/{id} [put]
func (h *TaxRateHandler) UpdateTaxRate(c *gin.Context) {
	var req dto.UpdateTaxRateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	id := c.Param("id")

	resp, err := h.service.UpdateTaxRate(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete a tax rate
// @Description Delete a tax rate
// @Tags Tax Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax rate ID"
// @Success 204 {object} nil
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxrate/{id} [delete]
func (h *TaxRateHandler) DeleteTaxRate(c *gin.Context) {
	id := c.Param("id")

	err := h.service.DeleteTaxRate(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}
